package roles

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	redisv9 "github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"

	"github.com/jooservices/xflickr/internal/platform/config"
	quotaOpenObserve "github.com/jooservices/xflickr/internal/quota/adapters/openobserve"
	quotaPostgres "github.com/jooservices/xflickr/internal/quota/adapters/postgres"
	quotaRedis "github.com/jooservices/xflickr/internal/quota/adapters/redis"
	quotaapp "github.com/jooservices/xflickr/internal/quota/app"
)

// quotaRuntime holds the quota pieces that must share one Rebuilder: the
// governor that gates every call, and the flusher that persists its usage.
type quotaRuntime struct {
	Governor     quotaRedis.Governor
	Rebuilder    *quotaapp.Rebuilder
	Flusher      quotaapp.Flusher
	CallRecorder *quotaapp.CallRecorder
	Partitions   quotaapp.Partitions
	// Emitter, when set, sends a quota.state event per active key every minute.
	Emitter *quotaapp.Emitter
}

// newQuotaEmitter sends quota.state events to OpenObserve. It returns nil, so
// nothing is emitted, until OpenObserve credentials are configured.
func newQuotaEmitter(states quotaapp.StateReader, cfg config.Config) *quotaapp.Emitter {
	if cfg.Obs.Username == "" || cfg.Obs.URL == "" {
		return nil
	}
	sink := quotaOpenObserve.Sink{URL: cfg.Obs.URL, Org: cfg.Obs.Org, Stream: cfg.Obs.Stream, Username: cfg.Obs.Username, Password: cfg.Obs.Password, Client: &http.Client{Timeout: 10 * time.Second}}
	return &quotaapp.Emitter{States: states, Sink: sink, Node: nodeID(cfg)}
}

func newQuotaRuntime(pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config) quotaRuntime {
	counters := quotaRedis.Counters{Client: redisClient, Prefix: cfg.Redis.Prefix}
	usage := quotaPostgres.Usage{Pool: pool}
	calls := quotaPostgres.CallLog{Pool: pool}
	rebuilder := &quotaapp.Rebuilder{Store: usage, Calls: calls, Counters: counters}
	var onDrop func()
	if dropped, err := otel.Meter("github.com/jooservices/xflickr/internal/quota").Int64Counter("xflickr.api_call_log.dropped"); err != nil {
		slog.Default().Error("quota: create call log dropped metric", "error", err)
	} else {
		onDrop = func() { dropped.Add(context.Background(), 1) }
	}
	return quotaRuntime{
		Governor:     quotaRedis.Governor{Client: redisClient, Prefix: cfg.Redis.Prefix, Budgets: quotaPostgres.Budgets{Pool: pool}, Rebuilder: rebuilder, Recorder: calls},
		Rebuilder:    rebuilder,
		Flusher:      quotaapp.Flusher{Store: usage, Counters: counters, Pruner: calls},
		CallRecorder: quotaapp.NewCallRecorder(calls, 10_000, 500, time.Second, onDrop),
		Partitions:   quotaapp.Partitions{Store: quotaPostgres.Partitions{Pool: pool}, RetentionDays: cfg.CallLog.RetentionDays},
	}
}

// start rebuilds every key's counters, then flushes usage every minute until
// the returned stop function is called. Stop waits for the final flush. A
// failed start rebuild is logged only: Acquire stays closed for any key that
// is not ready and rebuilds it on demand.
func (q quotaRuntime) start(ctx context.Context, logger *slog.Logger) (stop func()) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := q.Rebuilder.RebuildAll(ctx); err != nil {
		logger.ErrorContext(ctx, "quota: rebuild at start failed", "error", err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		q.Flusher.Run(runCtx, func(err error) { logger.ErrorContext(runCtx, "quota: flush failed", "error", err) })
	}()
	if q.CallRecorder != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q.CallRecorder.Run(runCtx, func(err error) { logger.ErrorContext(runCtx, "quota: call log write failed", "error", err) })
		}()
	}
	if q.Partitions.Store != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q.Partitions.Run(runCtx, func(err error) { logger.ErrorContext(runCtx, "quota: call log maintenance failed", "error", err) })
		}()
	}
	if q.Emitter != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q.Emitter.Run(runCtx, func(err error) { logger.ErrorContext(runCtx, "quota: emit state failed", "error", err) })
		}()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			wg.Wait()
		})
	}
}
