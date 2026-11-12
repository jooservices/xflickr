package roles

import (
	"context"
	"log/slog"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/queue"
	scheduleriver "github.com/jooservices/xflickr/internal/schedule/adapters/river"
)

type syncRole struct{}

func (syncRole) Name() string { return "sync" }

func (syncRole) Run(ctx context.Context) error {
	if ctx == nil || ctx.Err() != nil {
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pool, err := openRoleDatabase(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer pool.Close()
	callObserver, stopCallObserver := startFlickrCallLog(ctx, pool)
	defer stopCallObserver()
	redisClient := newRedisClient(cfg)
	defer redisClient.Close()
	stopMetrics := startMetricsCollection(ctx, pool, redisClient, cfg, "sync", "", false, slog.Default())
	defer stopMetrics()
	queueAdapter, err := newRiverWithSettings(ctx, pool, map[string]int{scheduleriver.PhotoQueue: 1, scheduleriver.PhotoExtrasQueue: 1})
	if err != nil {
		return err
	}
	if err := registerPhotoSizes(queueAdapter, pool, redisClient, cfg, newFlickrClient(cfg, callObserver)); err != nil {
		return err
	}
	configureRoleReadiness(ctx, "sync", postgresDependency(pool), redisDependency(redisClient))
	return queue.Worker{Queue: queueAdapter}.Run(ctx)
}
