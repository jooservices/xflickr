package queue

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

func TestNewRiverValidation(t *testing.T) {
	if _, err := NewRiver(nil, nil); err == nil || !strings.Contains(err.Error(), "pool") {
		t.Fatalf("NewRiver(nil) error = %v", err)
	}
	pool := disconnectedPool(t)
	if _, err := NewRiver(pool, map[string]int{"media": 0}); err == nil {
		t.Fatal("NewRiver() accepted zero concurrency")
	}
	adapter, err := NewRiver(pool, map[string]int{"crawl@node-1": 2, "media": 4})
	if err != nil {
		t.Fatalf("NewRiver() error = %v", err)
	}
	if len(adapter.queues) != 2 || adapter.queues["media"].MaxWorkers != 4 || adapter.queues[riverQueueName("crawl@node-1")].MaxWorkers != 2 {
		t.Fatalf("configured queues = %#v", adapter.queues)
	}
	_ = adapter.Stopped()
	if err := adapter.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := (*River)(nil).Stop(context.Background()); err != nil {
		t.Fatalf("nil adapter Stop() error = %v", err)
	}
	if err := adapter.Stop(nil); err == nil {
		t.Fatal("Stop() accepted nil context")
	}
	select {
	case <-(*River)(nil).Stopped():
	case <-time.After(time.Second):
		t.Fatal("nil adapter stopped channel remained open")
	}
	if err := (Worker{}).Run(context.Background()); err == nil {
		t.Fatal("Worker.Run() accepted missing queue")
	}
}

func TestRegisterAndStartValidation(t *testing.T) {
	pool := disconnectedPool(t)
	adapter, err := NewRiver(pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := (*River)(nil).Register("x", func(context.Context, Job) error { return nil }); err == nil {
		t.Fatal("nil adapter Register() succeeded")
	}
	if err := adapter.Register("", func(context.Context, Job) error { return nil }); err == nil {
		t.Fatal("Register() accepted empty kind")
	}
	if err := adapter.Register("x", nil); err == nil {
		t.Fatal("Register() accepted nil handler")
	}
	if err := adapter.RegisterWithOptions("x", func(context.Context, Job) error { return nil }, Options{MaxAttempts: -1}); err == nil {
		t.Fatal("RegisterWithOptions() accepted negative attempts")
	}
	if err := adapter.RegisterWithOptions("x", func(context.Context, Job) error { return nil }, Options{Queue: "media", MaxAttempts: 4}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Register("x", func(context.Context, Job) error { return nil }); err == nil {
		t.Fatal("Register() accepted duplicate kind")
	}
	if err := adapter.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "queue") {
		t.Fatalf("Start() error = %v", err)
	}
	if err := (&River{}).Start(context.Background()); err == nil {
		t.Fatal("Start() accepted uninitialized adapter")
	}
	if err := (*River)(nil).Start(context.Background()); err == nil {
		t.Fatal("nil adapter Start() succeeded")
	}
	if err := (*River)(nil).Enqueue(context.Background(), nil, Job{}, Options{}); err == nil {
		t.Fatal("nil adapter Enqueue() succeeded")
	}
	if err := adapter.Enqueue(context.Background(), nil, Job{}, Options{}); err == nil {
		t.Fatal("Enqueue() accepted nil transaction")
	}
	if err := adapter.Enqueue(nil, nil, Job{}, Options{}); err == nil {
		t.Fatal("Enqueue() accepted nil context")
	}
}

func TestEnqueueValidationAndDefaults(t *testing.T) {
	pool := disconnectedPool(t)
	adapter, err := NewRiver(pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Enqueue(context.Background(), nil, Job{}, Options{}); err == nil {
		t.Fatal("Enqueue() accepted invalid job")
	}
	job := Job{Kind: "crawl", Args: json.RawMessage(`{"id":7}`)}
	if err := adapter.Enqueue(context.Background(), nil, job, Options{}); err == nil || !strings.Contains(err.Error(), "queue name") {
		t.Fatalf("Enqueue() nil transaction error = %v", err)
	}
	if err := adapter.Enqueue(context.Background(), nil, job, Options{Queue: "media"}); err == nil || !strings.Contains(err.Error(), "transaction") {
		t.Fatalf("Enqueue() nil transaction error = %v", err)
	}
	if err := adapter.RegisterWithOptions("crawl", func(context.Context, Job) error { return nil }, Options{
		Queue: "crawl@node-1", MaxAttempts: 5, LeaseTimeout: time.Minute, Backoff: time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	resolved := applyDefaults(Options{}, adapter.handlers["crawl"].defaults)
	if resolved.Queue != "crawl@node-1" || resolved.MaxAttempts != 5 || resolved.LeaseTimeout != time.Minute || resolved.Backoff != time.Second {
		t.Fatalf("resolved per-kind defaults = %#v", resolved)
	}
	if got := applyDefaults(Options{Queue: "media", MaxAttempts: 2}, resolved); got.Queue != "media" || got.MaxAttempts != 2 {
		t.Fatalf("explicit enqueue options were not preserved: %#v", got)
	}
	var captured riverArgs
	var capturedOpts *river.InsertOpts
	adapter.insertTx = func(_ context.Context, tx pgx.Tx, args riverArgs, opts *river.InsertOpts) error {
		if tx == nil {
			t.Fatal("insert received nil transaction")
		}
		captured, capturedOpts = args, opts
		return nil
	}
	if err := adapter.Enqueue(context.Background(), fakeTx{}, job, Options{}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if captured.KindName != "crawl" || string(captured.Args) != `{"id":7}` || captured.Queue != "crawl@node-1" || captured.Timeout != time.Minute || captured.Backoff != time.Second {
		t.Fatalf("encoded River job = %#v", captured)
	}
	if capturedOpts.Queue != riverQueueName("crawl@node-1") || capturedOpts.MaxAttempts != 5 {
		t.Fatalf("River insert options = %#v", capturedOpts)
	}
	insertErr := errors.New("insert failed")
	adapter.insertTx = func(context.Context, pgx.Tx, riverArgs, *river.InsertOpts) error { return insertErr }
	if err := adapter.Enqueue(context.Background(), fakeTx{}, job, Options{}); !errors.Is(err, insertErr) || !strings.Contains(err.Error(), "insert River job") {
		t.Fatalf("Enqueue() insert error = %v", err)
	}
}

func TestDispatchWorker(t *testing.T) {
	adapter := &River{handlers: map[string]registeredHandler{}}
	worker := &dispatchWorker{adapter: adapter}
	jobArgs := riverArgs{KindName: "media", Args: json.RawMessage(`{"id":1}`), Timeout: time.Minute, Backoff: 2 * time.Second}
	row := &rivertype.JobRow{AttemptedAt: timePtr(time.Now())}
	job := &river.Job[riverArgs]{JobRow: row, Args: jobArgs}
	if worker.Timeout(job) != time.Minute {
		t.Fatal("Timeout() did not use job timeout")
	}
	if got := worker.NextRetry(job); !got.Equal(row.AttemptedAt.Add(2 * time.Second)) {
		t.Fatalf("NextRetry() = %v", got)
	}
	job.Args.Backoff = 0
	if !worker.NextRetry(job).IsZero() {
		t.Fatal("NextRetry() should use client retry policy when no backoff is set")
	}
	if err := worker.Work(context.Background(), job); err == nil || !strings.Contains(err.Error(), "no handler") {
		t.Fatalf("Work() missing handler error = %v", err)
	}
	wantErr := errors.New("handled")
	var got Job
	adapter.handlers["media"] = registeredHandler{handler: func(_ context.Context, received Job) error {
		got = received
		return wantErr
	}}
	if err := worker.Work(context.Background(), job); !errors.Is(err, wantErr) {
		t.Fatalf("Work() error = %v", err)
	}
	if got.Kind != "media" || string(got.Args) != `{"id":1}` {
		t.Fatalf("dispatched job = %#v", got)
	}
	if err := (&dispatchWorker{}).Work(context.Background(), job); err == nil {
		t.Fatal("Work() accepted missing adapter")
	}
}

func TestDeadLetterHookSkipsSuccessfulAndNonFinalJobs(t *testing.T) {
	hook := &deadLetterHook{}
	row := &rivertype.JobRow{Attempt: 1, MaxAttempts: 2}
	if got := hook.WorkEnd(context.Background(), row, nil); got != nil {
		t.Fatalf("successful WorkEnd() = %v", got)
	}
	wantErr := errors.New("retry")
	if got := hook.WorkEnd(context.Background(), row, wantErr); !errors.Is(got, wantErr) {
		t.Fatalf("non-final WorkEnd() = %v", got)
	}
}

func TestDeadLetterHookReportsInvalidFinalPayload(t *testing.T) {
	hook := &deadLetterHook{}
	row := &rivertype.JobRow{Attempt: 1, MaxAttempts: 1, EncodedArgs: []byte("not-json")}
	wantErr := errors.New("failed")
	got := hook.WorkEnd(context.Background(), row, wantErr)
	if !errors.Is(got, wantErr) || !strings.Contains(got.Error(), "decode exhausted") {
		t.Fatalf("WorkEnd() error = %v", got)
	}
}

func TestDeadLetterHookRequiresPool(t *testing.T) {
	hook := &deadLetterHook{}
	encoded, _ := json.Marshal(riverArgs{KindName: "crawl", Args: json.RawMessage(`{"id":1}`)})
	row := &rivertype.JobRow{Attempt: 2, MaxAttempts: 2, EncodedArgs: encoded}
	wantErr := errors.New("failed")
	got := hook.WorkEnd(context.Background(), row, wantErr)
	if !errors.Is(got, wantErr) || !strings.Contains(got.Error(), "outbox publisher") {
		t.Fatalf("WorkEnd() error = %v", got)
	}
	if err := publishDeadLetter(context.Background(), nil, "job.dead", "1", map[string]any{}); err == nil {
		t.Fatal("publishDeadLetter() accepted nil pool")
	}
	if err := publishDeadLetter(context.Background(), disconnectedPool(t), "job.dead", "1", map[string]any{}); err == nil {
		t.Fatal("publishDeadLetter() succeeded without PostgreSQL")
	}
}

func TestDeadLetterHookPublishesFinalFailure(t *testing.T) {
	encoded, _ := json.Marshal(riverArgs{KindName: "crawl", Args: json.RawMessage(`{"id":1}`), Queue: "crawl@node-1"})
	row := &rivertype.JobRow{ID: 42, Attempt: 3, MaxAttempts: 3, EncodedArgs: encoded}
	wantErr := errors.New("failed")
	var topic, key string
	var payload map[string]any
	hook := &deadLetterHook{publish: func(_ context.Context, gotTopic, gotKey string, gotPayload any) error {
		topic, key = gotTopic, gotKey
		payload = gotPayload.(map[string]any)
		return nil
	}}
	if got := hook.WorkEnd(context.Background(), row, wantErr); !errors.Is(got, wantErr) {
		t.Fatalf("WorkEnd() error = %v", got)
	}
	if topic != "job.dead" || key != "42" || payload["kind"] != "crawl" || payload["queue"] != "crawl@node-1" {
		t.Fatalf("published dead letter = %q %q %#v", topic, key, payload)
	}
	publishErr := errors.New("database unavailable")
	hook.publish = func(context.Context, string, string, any) error { return publishErr }
	if got := hook.WorkEnd(context.Background(), row, wantErr); !errors.Is(got, wantErr) || !errors.Is(got, publishErr) {
		t.Fatalf("failed publication error = %v", got)
	}
}

func disconnectedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://user:password@127.0.0.1:1/xflickr_wt82?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type fakeTx struct{}

func (fakeTx) Begin(context.Context) (pgx.Tx, error) { return nil, errors.New("unused") }
func (fakeTx) Commit(context.Context) error          { return nil }
func (fakeTx) Rollback(context.Context) error        { return nil }
func (fakeTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("unused")
}
func (fakeTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (fakeTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }
func (fakeTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("unused")
}

func (fakeTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unused")
}

func (fakeTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unused")
}
func (fakeTx) QueryRow(context.Context, string, ...any) pgx.Row { return nil }
func (fakeTx) Conn() *pgx.Conn                                  { return nil }

func timePtr(value time.Time) *time.Time { return &value }
