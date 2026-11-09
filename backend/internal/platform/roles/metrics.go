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

const (
	metricsSetupTimeout       = 5 * time.Second
	metricsSetupRetryInterval = 30 * time.Second
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
	collectors, err := buildMetricsCollectors(pool, redisClient, cfg, role, version, withGrowth, network, clock)
	if err != nil {
		return err
	}
	runner, err := metricsapp.NewRunner(store, collectors, clock, role, nodeID(cfg), versionOrDev(version), logger)
	if err != nil {
		return err
	}
	var retention *metricsapp.RetentionJob
	if withGrowth {
		settingsRepo, err := settingspostgres.New(pool)
		if err != nil {
			return err
		}
		settings := settingsapp.New(settingsRepo)
		retention, err = metricsapp.NewRetentionJob(store, metricsapp.RetentionFunc(settings.MetricsRetentionDays), clock, logger)
		if err != nil {
			return err
		}
	}
	var runRetention func(context.Context)
	if retention != nil {
		runRetention = retention.Run
	}
	runMetricsLoops(ctx, runner.Run, runRetention)
	return nil
}

func runMetricsLoops(ctx context.Context, runner, retention func(context.Context)) {
	if retention == nil {
		runner(ctx)
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		retention(ctx)
	}()
	runner(ctx)
	<-done
}

func buildMetricsCollectors(pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config, role, version string, withGrowth bool, network *metricsprocess.ByteCounter, clock metricsapp.SystemClock) ([]metricsapp.ScheduledCollector, error) {
	processCollector, err := metricsprocess.NewCollector(clock, versionOrDev(version), clock.Now(), network)
	if err != nil {
		return nil, err
	}
	riverCollector, err := metricsriver.NewCollector(pool, clock, role, nodeID(cfg))
	if err != nil {
		return nil, err
	}
	collectors := []metricsapp.ScheduledCollector{
		{Collector: processCollector, Interval: time.Minute},
		{Collector: riverCollector, Interval: time.Minute},
	}
	if !withGrowth {
		return collectors, nil
	}
	growthCollector, err := metricsgrowth.NewCollector(pool, clock)
	if err != nil {
		return nil, err
	}
	if redisClient != nil {
		growthCollector.Redis = metricsgrowth.RedisReader{Client: metricsgrowth.GoRedisMemory{Client: redisClient}}
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
	return append(collectors, metricsapp.ScheduledCollector{Collector: growthCollector, Interval: 10 * time.Minute}), nil
}

func versionOrDev(version string) string {
	if version == "" {
		return "dev"
	}
	return version
}

// metricsWrappedRole starts metrics collection around an existing role.
type metricsWrappedRole struct {
	inner    Role
	roleName string
	version  string
}

func (r metricsWrappedRole) Name() string {
	if r.inner != nil {
		return r.inner.Name()
	}
	return r.roleName
}

var pingRoleDatabase = func(ctx context.Context, pool *pgxpool.Pool) error {
	return pool.Ping(ctx)
}

func (r metricsWrappedRole) Run(ctx context.Context) error {
	if ctx == nil || ctx.Err() != nil {
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runMetricsWithRetry(runCtx, slog.Default(), r.Name(), metricsSetupRetryInterval, r.startMetrics)
	}()
	defer func() {
		cancel()
		<-done
	}()
	if r.inner == nil {
		<-ctx.Done()
		return nil
	}
	return r.inner.Run(ctx)
}

func (r metricsWrappedRole) startMetrics(ctx context.Context) (func(), error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	setupCtx, cancel := context.WithTimeout(ctx, metricsSetupTimeout)
	defer cancel()
	pool, err := openRoleDatabase(setupCtx, cfg.DB)
	if err != nil {
		return nil, err
	}
	if err := pingRoleDatabase(setupCtx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	redisClient := newRedisClient(cfg)
	stopMetrics := startMetricsCollection(ctx, pool, redisClient, cfg, r.Name(), r.version, false, slog.Default())
	return func() {
		stopMetrics()
		_ = redisClient.Close()
		pool.Close()
	}, nil
}

func runMetricsWithRetry(ctx context.Context, logger *slog.Logger, role string, retryInterval time.Duration, start func(context.Context) (func(), error)) {
	if logger == nil {
		logger = slog.Default()
	}
	if retryInterval <= 0 {
		retryInterval = metricsSetupRetryInterval
	}
	for ctx.Err() == nil {
		stopMetrics, err := start(ctx)
		if err == nil {
			<-ctx.Done()
			if stopMetrics != nil {
				stopMetrics()
			}
			return
		}
		if ctx.Err() != nil {
			return
		}
		logger.WarnContext(ctx, "metrics: role collection unavailable; retrying", "role", role, "error", err)
		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
