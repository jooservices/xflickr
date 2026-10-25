package queue

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/jooservices/xflickr/internal/platform/outbox"
)

const riverJobKind = "xflickr_job"

type riverArgs struct {
	KindName string          `json:"kind"`
	Args     json.RawMessage `json:"args"`
	Queue    string          `json:"queue"`
	Timeout  time.Duration   `json:"timeout,omitempty"`
	Backoff  time.Duration   `json:"backoff,omitempty"`
}

func (riverArgs) Kind() string { return riverJobKind }

// River is the PostgreSQL River adapter for JobQueue. Queue names and their
// concurrency are supplied by each worker process at construction time.
type River struct {
	pool     *pgxpool.Pool
	client   *river.Client[pgx.Tx]
	insertTx func(context.Context, pgx.Tx, riverArgs, *river.InsertOpts) error
	queues   map[string]river.QueueConfig
	mu       sync.RWMutex
	handlers map[string]registeredHandler
}

type registeredHandler struct {
	handler  Handler
	defaults Options
}

// NewRiver creates an adapter that can enqueue jobs and work the configured
// queues. An empty queue map creates an insert-only client.
func NewRiver(pool *pgxpool.Pool, queues map[string]int) (*River, error) {
	if pool == nil {
		return nil, errors.New("queue: PostgreSQL pool is required")
	}
	queueConfigs := make(map[string]river.QueueConfig, len(queues))
	for name, concurrency := range queues {
		if name == "" || concurrency < 1 {
			return nil, fmt.Errorf("queue: queue %q must have positive concurrency", name)
		}
		queueConfigs[riverQueueName(name)] = river.QueueConfig{MaxWorkers: concurrency}
	}
	adapter := &River{pool: pool, queues: queueConfigs, handlers: make(map[string]registeredHandler)}
	worker := &dispatchWorker{adapter: adapter}
	workers := river.NewWorkers()
	river.AddWorker(workers, worker)
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues: queueConfigs, Workers: workers, SkipUnknownJobCheck: true,
		Hooks: []rivertype.Hook{&deadLetterHook{publish: func(ctx context.Context, topic, key string, payload any) error {
			return publishDeadLetter(ctx, pool, topic, key, payload)
		}}},
	})
	if err != nil {
		return nil, fmt.Errorf("queue: create River client: %w", err)
	}
	adapter.client = client
	adapter.insertTx = func(ctx context.Context, tx pgx.Tx, args riverArgs, opts *river.InsertOpts) error {
		_, err := client.InsertTx(ctx, tx, args, opts)
		if err != nil {
			return fmt.Errorf("River insert transaction: %w", err)
		}
		return nil
	}
	return adapter, nil
}

// Enqueue inserts the job in the caller's transaction. A rollback therefore
// removes the River row along with all other changes in the unit of work.
func (r *River) Enqueue(ctx context.Context, tx pgx.Tx, job Job, opts Options) error {
	if r == nil || r.insertTx == nil {
		return errors.New("queue: River adapter is required")
	}
	if ctx == nil {
		return errors.New("queue: context is required")
	}
	if err := job.validate(); err != nil {
		return err
	}
	r.mu.RLock()
	registered, registeredOK := r.handlers[job.Kind]
	r.mu.RUnlock()
	if registeredOK {
		opts = applyDefaults(opts, registered.defaults)
	}
	if err := opts.validate(); err != nil {
		return err
	}
	if tx == nil {
		return errors.New("queue: transaction is required")
	}
	args, err := json.Marshal(riverArgs{KindName: job.Kind, Args: job.Args, Queue: opts.Queue, Timeout: opts.LeaseTimeout, Backoff: opts.Backoff})
	if err != nil {
		return fmt.Errorf("queue: encode River args: %w", err)
	}
	var decoded riverArgs
	if err := json.Unmarshal(args, &decoded); err != nil {
		return fmt.Errorf("queue: decode River args: %w", err)
	}
	err = r.insertTx(ctx, tx, decoded, &river.InsertOpts{
		Queue: riverQueueName(opts.Queue), MaxAttempts: opts.MaxAttempts,
	})
	if err != nil {
		return fmt.Errorf("queue: insert River job: %w", err)
	}
	return nil
}

// River currently restricts configured queue names to ASCII letters, digits,
// underscores, and hyphens. Hex encoding retains a stable mapping for XFlickr
// names such as crawl@node-1 without collisions.
func riverQueueName(name string) string {
	for _, char := range name {
		if !riverQueueCharacter(char) {
			return "q_" + hex.EncodeToString([]byte(name))
		}
	}
	return name
}

func riverQueueCharacter(char rune) bool {
	return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-'
}

// Register associates an application job kind with its handler.
func (r *River) Register(kind string, handler Handler) error {
	return r.RegisterWithOptions(kind, handler, Options{})
}

// RegisterWithOptions registers a handler and per-kind queue, retry, and lease
// defaults. Explicit enqueue options override these defaults.
func (r *River) RegisterWithOptions(kind string, handler Handler, defaults Options) error {
	if r == nil {
		return errors.New("queue: River adapter is required")
	}
	if kind == "" || handler == nil {
		return errors.New("queue: job kind and handler are required")
	}
	if defaults.MaxAttempts < 0 || defaults.LeaseTimeout < 0 || defaults.Backoff < 0 {
		return errors.New("queue: per-kind retry settings cannot be negative")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[kind]; exists {
		return fmt.Errorf("queue: handler already registered for %q", kind)
	}
	r.handlers[kind] = registeredHandler{handler: handler, defaults: defaults}
	return nil
}

func applyDefaults(opts, defaults Options) Options {
	if opts.Queue == "" {
		opts.Queue = defaults.Queue
	}
	if opts.MaxAttempts == 0 {
		opts.MaxAttempts = defaults.MaxAttempts
	}
	if opts.LeaseTimeout == 0 {
		opts.LeaseTimeout = defaults.LeaseTimeout
	}
	if opts.Backoff == 0 {
		opts.Backoff = defaults.Backoff
	}
	return opts
}

// Start runs the configured River worker queues until ctx is canceled.
func (r *River) Start(ctx context.Context) error {
	if r == nil || r.client == nil {
		return errors.New("queue: River adapter is required")
	}
	if len(r.queues) == 0 {
		return errors.New("queue: at least one worker queue is required to start")
	}
	if err := r.client.Start(ctx); err != nil {
		return fmt.Errorf("queue: start River client: %w", err)
	}
	return nil
}

// Stop gracefully stops fetching new jobs and allows active work to finish.
func (r *River) Stop(ctx context.Context) error {
	if ctx == nil {
		return errors.New("queue: stop context is required")
	}
	if r != nil && r.client != nil {
		if err := r.client.Stop(ctx); err != nil {
			return fmt.Errorf("queue: stop River client: %w", err)
		}
	}
	return nil
}

// Stopped returns a channel that closes when the River client has stopped.
func (r *River) Stopped() <-chan struct{} {
	if r == nil || r.client == nil {
		stopped := make(chan struct{})
		close(stopped)
		return stopped
	}
	return r.client.Stopped()
}

type dispatchWorker struct {
	river.WorkerDefaults[riverArgs]
	adapter *River
}

func (w *dispatchWorker) Timeout(job *river.Job[riverArgs]) time.Duration {
	return job.Args.Timeout
}

func (w *dispatchWorker) NextRetry(job *river.Job[riverArgs]) time.Time {
	if job.Args.Backoff <= 0 || job.AttemptedAt == nil {
		return time.Time{}
	}
	return job.AttemptedAt.Add(job.Args.Backoff)
}

func (w *dispatchWorker) Work(ctx context.Context, job *river.Job[riverArgs]) error {
	if w.adapter == nil {
		return errors.New("queue: worker has no adapter")
	}
	w.adapter.mu.RLock()
	registered := w.adapter.handlers[job.Args.KindName]
	w.adapter.mu.RUnlock()
	if registered.handler == nil {
		return fmt.Errorf("queue: no handler registered for %q", job.Args.KindName)
	}
	return registered.handler(ctx, Job{Kind: job.Args.KindName, Args: append(json.RawMessage(nil), job.Args.Args...)})
}

type deadLetterHook struct {
	river.HookDefaults
	publish func(context.Context, string, string, any) error
}

func (h *deadLetterHook) WorkEnd(ctx context.Context, row *rivertype.JobRow, workErr error) error {
	if workErr == nil || row == nil || row.Attempt < row.MaxAttempts {
		return workErr
	}
	var args riverArgs
	if err := json.Unmarshal(row.EncodedArgs, &args); err != nil {
		return errors.Join(workErr, fmt.Errorf("queue: decode exhausted River job: %w", err))
	}
	if h.publish == nil {
		return errors.Join(workErr, errors.New("queue: dead letter outbox publisher is required"))
	}
	payload := map[string]any{
		"job_id": strconv.FormatInt(row.ID, 10), "kind": args.KindName,
		"args": args.Args, "queue": args.Queue, "attempts": row.Attempt,
		"error": workErr.Error(),
	}
	if err := h.publish(ctx, "job.dead", strconv.FormatInt(row.ID, 10), payload); err != nil {
		return errors.Join(workErr, fmt.Errorf("queue: publish dead letter event: %w", err))
	}
	return workErr
}

func publishDeadLetter(ctx context.Context, pool *pgxpool.Pool, topic, key string, payload any) error {
	if pool == nil {
		return errors.New("queue: dead letter outbox database is required")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin dead letter event: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := outbox.PublishContext(ctx, tx, topic, key, payload); err != nil {
		return fmt.Errorf("publish dead letter event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit dead letter event: %w", err)
	}
	return nil
}
