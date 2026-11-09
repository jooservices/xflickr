package roles

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/ops"
	opsopenobserve "github.com/jooservices/xflickr/internal/platform/ops/adapters/openobserve"
	opspostgres "github.com/jooservices/xflickr/internal/platform/ops/adapters/postgres"
)

// newHealthSink ships crawl health to OpenObserve. It returns nil, so the
// health line is only logged, until OpenObserve credentials are configured.
func newHealthSink(cfg config.Config) ops.Sink {
	if cfg.Obs.Username == "" || cfg.Obs.URL == "" {
		return nil
	}
	return opsopenobserve.Sink{URL: cfg.Obs.URL, Org: cfg.Obs.Org, Stream: cfg.Obs.Stream, Username: cfg.Obs.Username, Password: cfg.Obs.Password, Client: &http.Client{Timeout: 10 * time.Second}}
}

type healthLockPool struct {
	pool *pgxpool.Pool
}

func (p healthLockPool) Acquire(ctx context.Context) (opspostgres.LockConn, error) {
	if p.pool == nil {
		return nil, fmt.Errorf("ops: health lock pool is required")
	}
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("ops: acquire health lock connection: %w", err)
	}
	return conn, nil
}

// loadHealthSettings is replaced in tests to exercise success and failure paths.
var loadHealthSettings = opspostgres.LoadHealthSettings

// crawlHealthDeps builds the exclusive snapshot store and refresh interval.
func crawlHealthDeps(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) (ops.Store, time.Duration) {
	settings := opspostgres.DefaultHealthSettings()
	loaded, err := loadHealthSettings(ctx, pool)
	if err != nil {
		if logger != nil {
			logger.WarnContext(ctx, "crawl health: load settings failed; using defaults", "error", err)
		}
	} else {
		settings = loaded
	}
	inner := opspostgres.Store{Pool: pool, DB: pool, StatementTimeout: settings.StatementTimeout}
	return opspostgres.ExclusiveStore{Inner: inner, Pool: healthLockPool{pool: pool}}, settings.Interval
}

// startCrawlHealth logs the crawl health line until stop is called.
func startCrawlHealth(ctx context.Context, store ops.Store, cfg config.Config, logger *slog.Logger, interval time.Duration) (stop func()) {
	if interval <= 0 {
		interval = ops.DefaultInterval
	}
	monitor := &ops.Monitor{
		Service:  ops.Service{Store: store},
		Sink:     newHealthSink(cfg),
		Logger:   logger,
		Node:     nodeID(cfg),
		Interval: interval,
	}
	runCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		monitor.Run(runCtx)
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			wg.Wait()
		})
	}
}

// startCrawlHealthMonitor wires settings, the advisory lock and the monitor loop.
func startCrawlHealthMonitor(ctx context.Context, pool *pgxpool.Pool, cfg config.Config, logger *slog.Logger) (stop func()) {
	store, interval := crawlHealthDeps(ctx, pool, logger)
	return startCrawlHealth(ctx, store, cfg, logger, interval)
}
