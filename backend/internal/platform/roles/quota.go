package roles

import (
	"context"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	redisv9 "github.com/redis/go-redis/v9"

	"github.com/jooservices/xflickr/internal/platform/config"
	quotaPostgres "github.com/jooservices/xflickr/internal/quota/adapters/postgres"
	quotaRedis "github.com/jooservices/xflickr/internal/quota/adapters/redis"
	quotaapp "github.com/jooservices/xflickr/internal/quota/app"
)

// quotaRuntime holds the quota pieces that must share one Rebuilder: the
// governor that gates every call, and the flusher that persists its usage.
type quotaRuntime struct {
	Governor  quotaRedis.Governor
	Rebuilder *quotaapp.Rebuilder
	Flusher   quotaapp.Flusher
}

func newQuotaRuntime(pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config) quotaRuntime {
	counters := quotaRedis.Counters{Client: redisClient, Prefix: cfg.Redis.Prefix}
	usage := quotaPostgres.Usage{Pool: pool}
	calls := quotaPostgres.CallLog{Pool: pool}
	rebuilder := &quotaapp.Rebuilder{Store: usage, Calls: calls, Counters: counters}
	return quotaRuntime{
		Governor:  quotaRedis.Governor{Client: redisClient, Prefix: cfg.Redis.Prefix, Budgets: quotaPostgres.Budgets{Pool: pool}, Rebuilder: rebuilder, Recorder: calls},
		Rebuilder: rebuilder,
		Flusher:   quotaapp.Flusher{Store: usage, Counters: counters, Pruner: calls},
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
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			wg.Wait()
		})
	}
}
