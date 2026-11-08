package roles

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	redisv9 "github.com/redis/go-redis/v9"

	"github.com/jooservices/xflickr/internal/platform/config"
	metricsgrowth "github.com/jooservices/xflickr/internal/platform/metrics/adapters/growth"
	metricspostgres "github.com/jooservices/xflickr/internal/platform/metrics/adapters/postgres"
	metricsprocess "github.com/jooservices/xflickr/internal/platform/metrics/adapters/process"
	metricsriver "github.com/jooservices/xflickr/internal/platform/metrics/adapters/river"
	metricsapp "github.com/jooservices/xflickr/internal/platform/metrics/app"
	settingspostgres "github.com/jooservices/xflickr/internal/settings/adapters/postgres"
	settingsapp "github.com/jooservices/xflickr/internal/settings/app"
)

// startMetricsCollection runs the role-safe metrics runner until stop is called.
// Growth and retention run only when withGrowth is true (scheduler role).
func startMetricsCollection(ctx context.Context, pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config, role, version string, withGrowth bool, logger *slog.Logger) (stop func()) {
	if logger == nil {
		logger = slog.Default()
	}
	runCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := runMetrics(runCtx, pool, redisClient, cfg, role, version, withGrowth, logger); err != nil && runCtx.Err() == nil {
			logger.ErrorContext(runCtx, "metrics: runner stopped", "error", err, "role", role)
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			wg.Wait()
		})
	}
}

func runMetrics(ctx context.Context, pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config, role, version string, withGrowth bool, logger *slog.Logger) error {
	clock := metricsapp.SystemClock{}
	store := metricspostgres.Store{Pool: pool}
	network := &metricsprocess.ByteCounter{}
	if version == "" {
		version = "dev"
	}
	processCollector, err := metricsprocess.NewCollector(clock, version, clock.Now(), network)
	if err != nil {
		return err
	}
	riverCollector, err := metricsriver.NewCollector(pool, clock, role, nodeID(cfg))
	if err != nil {
		return err
	}
	collectors := []metricsapp.ScheduledCollector{
		{Collector: processCollector, Interval: time.Minute},
		{Collector: riverCollector, Interval: time.Minute},
	}
	if withGrowth {
		growthCollector, err := metricsgrowth.NewCollector(pool, clock)
		if err != nil {
			return err
		}
		if redisClient != nil {
			growthCollector.Redis = metricsgrowth.RedisReader{Client: redisClient}
		}
		if cfg.Search.URL != "" && cfg.Search.Username != "" {
			growthCollector.Search = &metricsgrowth.SearchReader{
				URL:         cfg.Search.URL,
				Username:    cfg.Search.Username,
				Password:    cfg.Search.Password,
				IndexPrefix: cfg.Search.IndexPrefix,
			}
		}
		if cfg.Blob.Driver == "local" && cfg.Blob.LocalDir != "" {
			growthCollector.MediaPath = cfg.Blob.LocalDir
		}
		collectors = append(collectors, metricsapp.ScheduledCollector{Collector: growthCollector, Interval: 10 * time.Minute})
	}
	runner, err := metricsapp.NewRunner(store, collectors, clock, role, nodeID(cfg), version, logger)
	if err != nil {
		return err
	}
	if withGrowth {
		settingsRepo, err := settingspostgres.New(pool)
		if err != nil {
			return err
		}
		settings := settingsapp.New(settingsRepo)
		retention, err := metricsapp.NewRetentionJob(store, metricsapp.RetentionFunc(settings.MetricsRetentionDays), clock, logger)
		if err != nil {
			return err
		}
		go retention.Run(ctx)
	}
	runner.Run(ctx)
	return nil
}
