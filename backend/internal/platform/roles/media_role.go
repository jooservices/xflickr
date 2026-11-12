package roles

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	catalogdownloadspostgres "github.com/jooservices/xflickr/internal/catalog/adapters/downloads/postgres"
	catalogpostgres "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	catalogapp "github.com/jooservices/xflickr/internal/catalog/app"
	mediahttp "github.com/jooservices/xflickr/internal/media/adapters/http"
	medialocal "github.com/jooservices/xflickr/internal/media/adapters/local"
	mediapostgres "github.com/jooservices/xflickr/internal/media/adapters/postgres"
	medias3 "github.com/jooservices/xflickr/internal/media/adapters/s3"
	mediaapp "github.com/jooservices/xflickr/internal/media/app"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/outbox"
	"github.com/jooservices/xflickr/internal/platform/queue"
	platformsse "github.com/jooservices/xflickr/internal/platform/sse"
)

type mediaRole struct{}

func (mediaRole) Name() string { return "media" }

//nolint:cyclop // Role startup validates shared dependencies before starting its worker and cleaner.
func (mediaRole) Run(ctx context.Context) error {
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
	redisClient := newRedisClient(cfg)
	defer redisClient.Close()
	stopMetrics := startMetricsCollection(ctx, pool, redisClient, cfg, "media", "", false, slog.Default())
	defer stopMetrics()
	assets, err := catalogpostgres.New(pool)
	if err != nil {
		return err
	}
	blobs, err := newBlobStore(ctx, cfg)
	if err != nil {
		return err
	}
	queueAdapter, err := newRiverWithSettings(ctx, pool, map[string]int{"media": 1})
	if err != nil {
		return err
	}
	events := platformsse.Publisher{Client: redisClient, Channel: cfg.Redis.Prefix + "events"}
	packer := mediaapp.Packer{Blobs: blobs, Repo: mediapostgres.Downloads{Pool: pool, Events: events}}
	if err := queueAdapter.RegisterWithOptions("media.pack", func(ctx context.Context, job queue.Job) error {
		args, err := mediaapp.ParsePackJob(job.Args)
		if err != nil {
			return err
		}
		return packer.Pack(ctx, args.DownloadID)
	}, queue.Options{Queue: "media", MaxAttempts: 3, LeaseTimeout: 2 * time.Hour}); err != nil {
		return err
	}
	configureRoleReadiness(ctx, "media", postgresDependency(pool), redisDependency(redisClient), blobStoreDependency(blobs))
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 4)
	go func() { results <- queue.Worker{Queue: queueAdapter}.Run(runCtx) }()
	go func() { results <- (mediaapp.Cleaner{Assets: assets, Blobs: blobs}).Run(runCtx) }()
	go func() {
		results <- (mediaapp.DownloadExpiryCleaner{Repository: mediapostgres.Downloads{Pool: pool}, Blobs: blobs}).Run(runCtx)
	}()
	go func() {
		handler := mediaapp.SizesResolvedHandler{Repository: mediapostgres.Downloads{Pool: pool, Jobs: queueAdapter, Events: events}}
		results <- outbox.NewConsumer(pool).Consume(runCtx, catalogapp.SizesResolvedTopic, func(ctx context.Context, event outbox.Event) error { return handler.Handle(ctx, event.Payload) })
	}()
	first := <-results
	cancel()
	second := <-results
	third := <-results
	fourth := <-results
	if ctx.Err() == nil {
		for _, result := range []error{first, second, third, fourth} {
			if result != nil {
				return result
			}
		}
	}
	return nil
}

func newBlobStore(ctx context.Context, cfg config.Config) (mediaapp.BlobStore, error) {
	switch cfg.Blob.Driver {
	case "local":
		return medialocal.New(cfg.Blob.LocalDir, cfg.Blob.MaxBytes)
	case "s3":
		return medias3.New(ctx, medias3.Config{
			Endpoint: cfg.Blob.S3Endpoint, Region: cfg.Blob.S3Region, Bucket: cfg.Blob.S3Bucket,
			AccessKey: cfg.Blob.S3AccessKey, SecretKey: cfg.Blob.S3SecretKey,
			UsePathStyle: cfg.Blob.S3UsePathStyle, MaxBytes: cfg.Blob.MaxBytes, MaxTTL: cfg.Blob.MaxTTL,
		})
	default:
		return nil, fmt.Errorf("media: unsupported blob driver %q", cfg.Blob.Driver)
	}
}

func buildDownloadHandler(pool *pgxpool.Pool, stores ...mediaapp.BlobStore) (mediahttp.DownloadHandler, error) {
	var store mediaapp.BlobStore
	if len(stores) > 0 {
		store = stores[0]
	}
	return buildDownloadHandlerWithEvents(pool, store, nil)
}

type downloadEventPublisher interface {
	Publish(context.Context, platformsse.Event) error
}

func buildDownloadHandlerWithEvents(pool *pgxpool.Pool, store mediaapp.BlobStore, events downloadEventPublisher, linkTTLs ...time.Duration) (mediahttp.DownloadHandler, error) {
	jobs, err := queue.NewRiver(pool, nil)
	if err != nil {
		return mediahttp.DownloadHandler{}, err
	}
	catalogRepository := catalogdownloadspostgres.Repository{Pool: pool}
	repository := mediapostgres.Downloads{Pool: pool, Jobs: jobs, Events: events}
	linkTTL := mediaapp.DefaultDownloadLinkTTL
	if len(linkTTLs) > 0 {
		linkTTL = linkTTLs[0]
	}
	var pressure interface{ Check(context.Context) error }
	if store != nil {
		pressure = mediaapp.Pressure{Store: store}
	}
	return mediahttp.DownloadHandler{
		Downloads: mediaapp.Downloads{Catalog: catalogapp.DownloadCatalog{Photos: catalogRepository}, Repository: repository, Pressure: pressure},
		Parts:     mediaapp.DownloadPartLinks{Repository: repository, Blobs: store, TTL: linkTTL},
		Retry:     mediaapp.RetryFailed{Repository: repository},
		Principal: userIDFromPrincipal,
	}, nil
}
