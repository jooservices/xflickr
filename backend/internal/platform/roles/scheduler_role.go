package roles

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	redisv9 "github.com/redis/go-redis/v9"

	catalogpostgres "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	catalogapp "github.com/jooservices/xflickr/internal/catalog/app"
	crawlpostgres "github.com/jooservices/xflickr/internal/crawl/adapters/postgres"
	crawlapp "github.com/jooservices/xflickr/internal/crawl/app"
	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	mediaapp "github.com/jooservices/xflickr/internal/media/app"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/outbox"
	"github.com/jooservices/xflickr/internal/platform/queue"
	quotaRedis "github.com/jooservices/xflickr/internal/quota/adapters/redis"
	quotaapp "github.com/jooservices/xflickr/internal/quota/app"
	quotadomain "github.com/jooservices/xflickr/internal/quota/domain"
	schedulehttp "github.com/jooservices/xflickr/internal/schedule/adapters/http"
	schedulepostgres "github.com/jooservices/xflickr/internal/schedule/adapters/postgres"
	queuepostgres "github.com/jooservices/xflickr/internal/schedule/adapters/queuepostgres"
	scheduleriver "github.com/jooservices/xflickr/internal/schedule/adapters/river"
	scheduleapp "github.com/jooservices/xflickr/internal/schedule/app"
	settingspostgres "github.com/jooservices/xflickr/internal/settings/adapters/postgres"
	settingsapp "github.com/jooservices/xflickr/internal/settings/app"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

type schedulerRole struct{}

func (schedulerRole) Name() string { return "scheduler" }

func (schedulerRole) Run(ctx context.Context) error {
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
	queueAdapter, err := queue.NewRiver(pool, nil)
	if err != nil {
		return err
	}
	settings, err := scheduleriver.LoadSettings(ctx, pool)
	if err != nil {
		return err
	}
	store := scheduleriver.Store{Pool: pool}
	quota, err := newQuotaRuntime(pool, redisClient, cfg)
	if err != nil {
		return err
	}
	quota.Emitter = newQuotaEmitter(quotaStates(pool, redisClient, cfg), cfg)
	defer quota.start(ctx, slog.Default())()
	stopHealth := startCrawlHealthMonitor(ctx, pool, cfg, slog.Default())
	defer stopHealth()
	stopMetrics := startMetricsCollection(ctx, pool, redisClient, cfg, "scheduler", "", true, slog.Default())
	defer stopMetrics()
	blobs, err := newBlobStore(ctx, cfg)
	if err != nil {
		return err
	}
	pump, err := newSchedulerPump(pool, quota.Governor, store, queueAdapter, settings, blobs)
	if err != nil {
		return err
	}
	configureRoleReadiness(ctx, "scheduler", postgresDependency(pool), redisDependency(redisClient), blobStoreDependency(blobs))
	return pump.Run(ctx)
}

// registerPhotoSizes registers the photo-size job on the sync queue.
func registerPhotoSizes(queueAdapter *queue.River, pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config, client *flickrclient.Client) error {
	catalogRepository, err := catalogpostgres.New(pool)
	if err != nil {
		return err
	}
	quota, _, err := newQuotaGovernor(pool, redisClient, cfg)
	if err != nil {
		return err
	}
	handler := crawlapp.Sizes{
		States: crawlpostgres.EntityStates{DB: pool},
		Store:  catalogpostgres.ResolvedSizes{Repository: catalogRepository, Pool: pool, Publish: publishSizesResolved},
		Flickr: photoSizesClient{Pool: pool, Client: client, Quota: quota, NodeID: nodeID(cfg)},
	}
	if err := queueAdapter.RegisterWithOptions(scheduleriver.PhotoJobKind, func(ctx context.Context, job queue.Job) error {
		ctx = flickrclient.WithCallContext(ctx, flickrclient.CallContext{Attempt: job.Attempt})
		args, err := crawlapp.ParseSizesJob(job.Args)
		if err != nil {
			return err
		}
		return snoozeOnQuota(handler.Handle(ctx, args))
	}, queue.Options{Queue: scheduleriver.PhotoQueue}); err != nil {
		return err
	}
	facetHandler := crawlapp.PhotoFacets{
		States: crawlpostgres.EntityStates{DB: pool},
		Store:  catalogRepository,
		Flickr: photoFacetClient{
			Pool: pool, Client: client,
			NodeID: nodeID(cfg),
			Quota:  quota,
		},
	}
	return queueAdapter.RegisterWithOptions(scheduleriver.PhotoFacetJobKind, func(ctx context.Context, job queue.Job) error {
		ctx = flickrclient.WithCallContext(ctx, flickrclient.CallContext{Attempt: job.Attempt})
		args, err := crawlapp.ParsePhotoFacetJob(job.Args)
		if err != nil {
			return err
		}
		return snoozeOnQuota(facetHandler.Handle(ctx, args))
	}, queue.Options{Queue: scheduleriver.PhotoExtrasQueue})
}

// snoozeOnQuota turns a quota wait into a River snooze, which never consumes an
// attempt, so exhausted quota delays a sizes job instead of discarding it.
func snoozeOnQuota(err error) error {
	var waiting *crawlapp.QuotaWaitError
	if errors.As(err, &waiting) {
		return queue.Snooze(waiting.Wait)
	}
	return err
}

type photoSizesClient struct {
	Pool interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	}
	Client *flickrclient.Client
	Quota  quotadomain.Governor
	NodeID string
}

func (c photoSizesClient) GetSizes(ctx context.Context, keyID int64, photoID string) ([]flickrclient.PhotoSize, error) {
	return c.GetSizesClass(ctx, keyID, photoID, quotadomain.Photo)
}

func (c photoSizesClient) GetSizesClass(ctx context.Context, keyID int64, photoID string, class quotadomain.Class) ([]flickrclient.PhotoSize, error) {
	if c.Pool == nil || c.Client == nil || c.Quota == nil || keyID <= 0 {
		return nil, errors.New("crawl: photo sizes client dependencies are required")
	}
	var apiKey, status string
	var userID int64
	if err := c.Pool.QueryRow(ctx, `SELECT key_public, status, user_id FROM api_keys WHERE id=$1`, keyID).Scan(&apiKey, &status, &userID); err != nil {
		return nil, fmt.Errorf("crawl: load photo sizes key: %w", err)
	}
	if status != "valid" {
		return nil, errors.New("crawl: photo sizes key is not valid")
	}
	if !class.Valid() {
		return nil, quotadomain.ErrInvalidClass
	}
	method := "flickr.photos.getSizes"
	metadata, _ := flickrclient.CallContextFrom(ctx)
	metadata.KeyID, metadata.UserID, metadata.Lane, metadata.Facet, metadata.NodeID = keyID, &userID, string(class), "photo_sizes", c.NodeID
	ctx = flickrclient.WithCallContext(ctx, metadata)
	permit, wait, err := c.Quota.Acquire(ctx, keyID, class)
	if err != nil {
		return nil, err
	}
	if permit == nil {
		c.Client.RecordQuotaWait(ctx, method)
		return nil, crawlapp.NewQuotaWaitError(flickrclient.ErrQuotaExceeded, wait)
	}
	sizes, callErr := c.Client.PhotosGetSizes(ctx, flickrclient.Credentials{APIKey: apiKey}, photoID)
	if doneErr := permit.Done(quotadomain.Meta{Err: callErr}); doneErr != nil {
		return nil, fmt.Errorf("crawl: release photo sizes quota permit: %w", doneErr)
	}
	return sizes, callErr
}

func newSchedulerPump(pool *pgxpool.Pool, governor quotaRedis.Governor, store scheduleapp.PumpStore, queueAdapter queue.JobQueue, settings scheduleapp.PumpSettings, stores ...mediaapp.BlobStore) (scheduleapp.Pump, error) {
	cycleRepository, err := crawlpostgres.NewCycleRepository(pool)
	if err != nil {
		return scheduleapp.Pump{}, err
	}
	effectiveSettingsRepository, err := settingspostgres.New(pool)
	if err != nil {
		return scheduleapp.Pump{}, err
	}
	pump := scheduleapp.Pump{Store: store, QueueState: queuepostgres.New(pool), Credits: governor, Queue: queueAdapter, Settings: settings}
	if source, ok := store.(scheduleapp.PumpSettingsSource); ok {
		pump.SettingsSource = source
	}
	if len(stores) > 0 && stores[0] != nil {
		pump.MediaPressure = mediaapp.Pressure{Store: stores[0]}
	}
	settingsService := settingsapp.New(effectiveSettingsRepository)
	pump.ChangeChecks = crawlapp.Cycle{Store: cycleRepository, Users: cycleRepository, Settings: settingsService}
	keyPool := quotaapp.KeyPool{
		Interests: personInterests{pool: pool},
		Keys:      usableKeys{pool: pool},
		Quota:     governor,
	}
	pump.PhotoLane = scheduleapp.PhotoLane{
		Eligibility: crawlpostgres.EntityStates{DB: pool},
		Selector:    catalogpostgres.PhotoLane{Pool: pool},
		Keys:        keyPool,
		Settings:    photoFacetSettings{settings: settingsService},
		Queue: scheduleriver.PhotoEnqueuer{
			Pool: pool, Jobs: queueAdapter, ReadyCap: settings.SyncReadyCap,
			Options: queue.Options{Queue: scheduleriver.PhotoQueue, MaxAttempts: max(1, settings.RetryMax), LeaseTimeout: settings.LeaseTimeout, Backoff: settings.RetryBackoff},
		},
	}
	return pump, nil
}

type planHandler struct{ schedulehttp.Handler }

func buildPlanHandler(pool *pgxpool.Pool) (schedulehttp.Handler, error) {
	if pool == nil {
		return schedulehttp.Handler{}, errors.New("schedule: postgres database is required")
	}
	store := schedulepostgres.New(pool)
	return schedulehttp.Handler{
		Plans: &scheduleapp.Plans{Store: store},
		Queue: scheduleapp.Queue{Store: queuepostgres.New(pool)},
		ActorID: func(ctx context.Context) (int64, bool) {
			principal, ok := identityhttp.PrincipalFrom(ctx)
			if !ok || principal.User.ID <= 0 {
				return 0, false
			}
			return principal.User.ID, true
		},
	}, nil
}

func publishSizesResolved(ctx context.Context, tx pgx.Tx, event catalogapp.SizesResolved) error {
	return outbox.PublishContext(ctx, tx, catalogapp.SizesResolvedTopic, strconv.FormatInt(event.PhotoID, 10), event)
}
