package roles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	redisv9 "github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"

	accountsflickr "github.com/jooservices/xflickr/internal/accounts/adapters/flickr"
	accountshttp "github.com/jooservices/xflickr/internal/accounts/adapters/http"
	accountspostgres "github.com/jooservices/xflickr/internal/accounts/adapters/postgres"
	accountsredis "github.com/jooservices/xflickr/internal/accounts/adapters/redis"
	accountsettings "github.com/jooservices/xflickr/internal/accounts/adapters/settings"
	accountsapp "github.com/jooservices/xflickr/internal/accounts/app"
	accountsdomain "github.com/jooservices/xflickr/internal/accounts/domain"
	catalogdownloadspostgres "github.com/jooservices/xflickr/internal/catalog/adapters/downloads/postgres"
	cataloghttp "github.com/jooservices/xflickr/internal/catalog/adapters/http"
	catalogpostgres "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	catalogapp "github.com/jooservices/xflickr/internal/catalog/app"
	clusterhttp "github.com/jooservices/xflickr/internal/cluster/adapters/http"
	clusterpostgres "github.com/jooservices/xflickr/internal/cluster/adapters/postgres"
	clusterapp "github.com/jooservices/xflickr/internal/cluster/app"
	crawlhttp "github.com/jooservices/xflickr/internal/crawl/adapters/http"
	crawlpostgres "github.com/jooservices/xflickr/internal/crawl/adapters/postgres"
	crawlsettings "github.com/jooservices/xflickr/internal/crawl/adapters/settings"
	crawlapp "github.com/jooservices/xflickr/internal/crawl/app"
	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	identitypostgres "github.com/jooservices/xflickr/internal/identity/adapters/postgres"
	identityredis "github.com/jooservices/xflickr/internal/identity/adapters/redis"
	identityapp "github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
	mediahttp "github.com/jooservices/xflickr/internal/media/adapters/http"
	medialocal "github.com/jooservices/xflickr/internal/media/adapters/local"
	mediapostgres "github.com/jooservices/xflickr/internal/media/adapters/postgres"
	medias3 "github.com/jooservices/xflickr/internal/media/adapters/s3"
	mediaapp "github.com/jooservices/xflickr/internal/media/app"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	auditpostgres "github.com/jooservices/xflickr/internal/platform/audit/adapters/postgres"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/crypto"
	"github.com/jooservices/xflickr/internal/platform/db"
	"github.com/jooservices/xflickr/internal/platform/httpx"
	"github.com/jooservices/xflickr/internal/platform/obs"
	"github.com/jooservices/xflickr/internal/platform/ops"
	opshttp "github.com/jooservices/xflickr/internal/platform/ops/adapters/http"
	opspostgres "github.com/jooservices/xflickr/internal/platform/ops/adapters/postgres"
	"github.com/jooservices/xflickr/internal/platform/outbox"
	"github.com/jooservices/xflickr/internal/platform/queue"
	platformsse "github.com/jooservices/xflickr/internal/platform/sse"
	quotahttp "github.com/jooservices/xflickr/internal/quota/adapters/http"
	quotaPostgres "github.com/jooservices/xflickr/internal/quota/adapters/postgres"
	quotaRedis "github.com/jooservices/xflickr/internal/quota/adapters/redis"
	quotaapp "github.com/jooservices/xflickr/internal/quota/app"
	quotadomain "github.com/jooservices/xflickr/internal/quota/domain"
	schedulehttp "github.com/jooservices/xflickr/internal/schedule/adapters/http"
	schedulepostgres "github.com/jooservices/xflickr/internal/schedule/adapters/postgres"
	queuepostgres "github.com/jooservices/xflickr/internal/schedule/adapters/queuepostgres"
	scheduleriver "github.com/jooservices/xflickr/internal/schedule/adapters/river"
	scheduleapp "github.com/jooservices/xflickr/internal/schedule/app"
	settingshttp "github.com/jooservices/xflickr/internal/settings/adapters/http"
	settingspostgres "github.com/jooservices/xflickr/internal/settings/adapters/postgres"
	settingsapp "github.com/jooservices/xflickr/internal/settings/app"
	sharinghttp "github.com/jooservices/xflickr/internal/sharing/adapters/http"
	sharingpostgres "github.com/jooservices/xflickr/internal/sharing/adapters/postgres"
	sharingapp "github.com/jooservices/xflickr/internal/sharing/app"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

const defaultAPIAddress = ":8080"

// openRoleDatabase keeps process startup injectable in unit tests without
// connecting to a shared or live database.
var openRoleDatabase = db.NewPool

// apiAddress is the API listener: HTTP_PORT when it is a valid port (the E2E stack picks a free one), else :8080.
func apiAddress() string {
	port, err := strconv.Atoi(strings.TrimSpace(os.Getenv("HTTP_PORT")))
	if err != nil || port < 1 || port > 65535 {
		return defaultAPIAddress
	}
	return ":" + strconv.Itoa(port)
}

var builtinRoleNames = []string{"api", "scheduler", "indexer", "node-agent", "crawl", "media", "sync", "ai-worker"}

// NewBuiltinRegistry creates the role set supported by the xflickr binary.
func NewBuiltinRegistry(version string) *Registry {
	registry := NewRegistry()
	registerBuiltin(registry, newConfiguredAPIRole(apiAddress(), version))
	for _, name := range builtinRoleNames[1:] {
		if name == "scheduler" {
			registerBuiltin(registry, schedulerRole{})
			continue
		}
		if name == "crawl" {
			registerBuiltin(registry, crawlRole{})
			continue
		}
		if name == "sync" {
			registerBuiltin(registry, syncRole{})
			continue
		}
		if name == "media" {
			registerBuiltin(registry, mediaRole{})
			continue
		}
		registerBuiltin(registry, idleRole{name: name})
	}
	return registry
}

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
	redisClient := redisv9.NewClient(&redisv9.Options{Addr: net.JoinHostPort(cfg.Redis.Host, strconv.Itoa(cfg.Redis.Port)), Password: cfg.Redis.Password, DB: cfg.Redis.DB})
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
	quota := newQuotaRuntime(pool, redisClient, cfg)
	quota.Emitter = newQuotaEmitter(quotaStates(pool, redisClient, cfg), cfg)
	defer quota.start(ctx, slog.Default())()
	stopHealth := startCrawlHealth(ctx, opspostgres.Store{Pool: pool}, cfg, slog.Default())
	defer stopHealth()
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

type crawlRole struct{}

func (crawlRole) Name() string { return "crawl" }

func (crawlRole) Run(ctx context.Context) error {
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
	settingsRepository, err := settingspostgres.New(pool)
	if err != nil {
		return err
	}
	interestsRepository, err := crawlpostgres.New(pool)
	if err != nil {
		return err
	}
	redisClient := redisv9.NewClient(&redisv9.Options{Addr: net.JoinHostPort(cfg.Redis.Host, strconv.Itoa(cfg.Redis.Port)), Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	defer redisClient.Close()
	queueAdapter, err := queue.NewRiver(pool, map[string]int{scheduleriver.PhotoQueue: crawlWorkers, scheduleriver.PhotoExtrasQueue: 1, accountsapp.VerifyQueue: 1})
	if err != nil {
		return err
	}
	client := newFlickrClient(cfg)
	engine, err := newCrawlEngine(pool, redisClient, cfg, client)
	if err != nil {
		return err
	}
	if err := registerCrawlJobs(queueAdapter, engine); err != nil {
		return err
	}
	if err := registerPhotoSizes(queueAdapter, pool, redisClient, cfg, client); err != nil {
		return err
	}
	verificationRepository, verifier, err := newAccountVerifier(pool, redisClient, cfg)
	if err != nil {
		return err
	}
	seeder := accountsapp.VerificationSeeder{Repository: verificationRepository, Jobs: queueAdapter}
	if err := startAccountVerification(ctx, queueAdapter, verifier, seeder); err != nil {
		return err
	}
	service := crawlapp.Interests{Repository: interestsRepository, Settings: settingsapp.New(settingsRepository)}
	configureRoleReadiness(ctx, "crawl", postgresDependency(pool), redisDependency(redisClient))
	// The River worker and the interest consumer run side by side; if either
	// fails the role fails, so a dead worker cannot leave the process idling.
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error { return queue.Worker{Queue: queueAdapter}.Run(groupCtx) })
	group.Go(func() error {
		return outbox.NewConsumer(pool).Consume(groupCtx, accountsdomain.ConnectedTopic, func(ctx context.Context, event outbox.Event) error {
			if err := service.HandleConnected(ctx, event.Payload); err != nil {
				return err
			}
			var connected accountsdomain.Connected
			if err := json.Unmarshal(event.Payload, &connected); err != nil {
				return fmt.Errorf("crawl: decode connected account event: %w", err)
			}
			return seeder.Enqueue(ctx, connected.AccountID)
		})
	})
	return group.Wait()
}

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
	redisClient := redisv9.NewClient(&redisv9.Options{Addr: net.JoinHostPort(cfg.Redis.Host, strconv.Itoa(cfg.Redis.Port)), Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	defer redisClient.Close()
	queueAdapter, err := queue.NewRiver(pool, map[string]int{scheduleriver.PhotoQueue: 1, scheduleriver.PhotoExtrasQueue: 1})
	if err != nil {
		return err
	}
	if err := registerPhotoSizes(queueAdapter, pool, redisClient, cfg, newFlickrClient(cfg)); err != nil {
		return err
	}
	configureRoleReadiness(ctx, "sync", postgresDependency(pool), redisDependency(redisClient))
	return queue.Worker{Queue: queueAdapter}.Run(ctx)
}

// registerPhotoSizes registers the photo-size job on the sync queue.
func registerPhotoSizes(queueAdapter *queue.River, pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config, client *flickrclient.Client) error {
	catalogRepository, err := catalogpostgres.New(pool)
	if err != nil {
		return err
	}
	quota := quotaRedis.Governor{Client: redisClient, Prefix: cfg.Redis.Prefix, Budgets: quotaPostgres.Budgets{Pool: pool}, Recorder: quotaPostgres.CallLog{Pool: pool}}
	handler := crawlapp.Sizes{
		Store:  catalogRepository,
		Flickr: photoSizesClient{Pool: pool, Client: client, Quota: quota},
	}
	if err := queueAdapter.RegisterWithOptions(scheduleriver.PhotoJobKind, func(ctx context.Context, job queue.Job) error {
		args, err := crawlapp.ParseSizesJob(job.Args)
		if err != nil {
			return err
		}
		return snoozeOnQuota(handler.Handle(ctx, args))
	}, queue.Options{Queue: scheduleriver.PhotoQueue}); err != nil {
		return err
	}
	facetHandler := crawlapp.PhotoFacets{
		Store: catalogRepository,
		Flickr: photoFacetClient{
			Pool: pool, Client: client,
			Quota: quotaRedis.Governor{Client: redisClient, Prefix: cfg.Redis.Prefix, Budgets: quotaPostgres.Budgets{Pool: pool}, Recorder: quotaPostgres.CallLog{Pool: pool}},
		},
	}
	return queueAdapter.RegisterWithOptions(scheduleriver.PhotoFacetJobKind, func(ctx context.Context, job queue.Job) error {
		args, err := crawlapp.ParsePhotoFacetJob(job.Args)
		if err != nil {
			return err
		}
		return snoozeOnQuota(facetHandler.Handle(ctx, args))
	}, queue.Options{Queue: scheduleriver.PhotoExtrasQueue})
}

type photoFacetClient struct {
	Pool interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	}
	Client *flickrclient.Client
	Quota  quotadomain.Governor
}

func (c photoFacetClient) PhotosGetInfo(ctx context.Context, keyID int64, photoID, secret string) (flickrclient.PhotoInfo, error) {
	var result flickrclient.PhotoInfo
	err := c.call(ctx, keyID, func(credentials flickrclient.Credentials) error {
		var callErr error
		result, callErr = c.Client.PhotosGetInfo(ctx, credentials, photoID, secret)
		return callErr
	})
	return result, err
}

func (c photoFacetClient) PhotosGetExif(ctx context.Context, keyID int64, photoID, secret string) (flickrclient.PhotoExif, error) {
	var result flickrclient.PhotoExif
	err := c.call(ctx, keyID, func(credentials flickrclient.Credentials) error {
		var callErr error
		result, callErr = c.Client.PhotosGetExif(ctx, credentials, photoID, secret)
		return callErr
	})
	return result, err
}

func (c photoFacetClient) PhotosGetAllContexts(ctx context.Context, keyID int64, photoID string) (flickrclient.PhotoContexts, error) {
	var result flickrclient.PhotoContexts
	err := c.call(ctx, keyID, func(credentials flickrclient.Credentials) error {
		var callErr error
		result, callErr = c.Client.PhotosGetAllContexts(ctx, credentials, photoID)
		return callErr
	})
	return result, err
}

func (c photoFacetClient) PhotosCommentsGetList(ctx context.Context, keyID int64, photoID string, page int) (flickrclient.Page[flickrclient.PhotoComment], error) {
	var result flickrclient.Page[flickrclient.PhotoComment]
	err := c.call(ctx, keyID, func(credentials flickrclient.Credentials) error {
		var callErr error
		result, callErr = c.Client.PhotosCommentsGetList(ctx, credentials, photoID, flickrclient.ListOptions{PerPage: 50, Page: page})
		return callErr
	})
	return result, err
}

func (c photoFacetClient) PhotosGetFavorites(ctx context.Context, keyID int64, photoID string, page int) (flickrclient.Page[flickrclient.PhotoFavorite], error) {
	var result flickrclient.Page[flickrclient.PhotoFavorite]
	err := c.call(ctx, keyID, func(credentials flickrclient.Credentials) error {
		var callErr error
		result, callErr = c.Client.PhotosGetFavorites(ctx, credentials, photoID, flickrclient.ListOptions{PerPage: 50, Page: page})
		return callErr
	})
	return result, err
}

func (c photoFacetClient) call(ctx context.Context, keyID int64, request func(flickrclient.Credentials) error) error {
	if c.Pool == nil || c.Client == nil || c.Quota == nil || ctx == nil || keyID <= 0 {
		return errors.New("crawl: photo facet client dependencies are required")
	}
	var apiKey, status string
	if err := c.Pool.QueryRow(ctx, `SELECT key_public, status FROM api_keys WHERE id=$1`, keyID).Scan(&apiKey, &status); err != nil {
		return fmt.Errorf("crawl: load photo facet key: %w", err)
	}
	if status != "valid" {
		return errors.New("crawl: photo facet key is not valid")
	}
	permit, wait, err := c.Quota.Acquire(ctx, keyID, quotadomain.Photo)
	if err != nil {
		return err
	}
	if permit == nil {
		return crawlapp.NewQuotaWaitError(flickrclient.ErrQuotaExceeded, wait)
	}
	callErr := request(flickrclient.Credentials{APIKey: apiKey})
	if doneErr := permit.Done(quotadomain.Meta{Err: callErr}); doneErr != nil {
		if callErr != nil {
			return errors.Join(callErr, fmt.Errorf("crawl: release photo facet quota permit: %w", doneErr))
		}
		return fmt.Errorf("crawl: release photo facet quota permit: %w", doneErr)
	}
	return callErr
}

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
	redisClient := redisv9.NewClient(&redisv9.Options{Addr: net.JoinHostPort(cfg.Redis.Host, strconv.Itoa(cfg.Redis.Port)), Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	defer redisClient.Close()
	assets, err := catalogpostgres.New(pool)
	if err != nil {
		return err
	}
	blobs, err := newBlobStore(ctx, cfg)
	if err != nil {
		return err
	}
	queueAdapter, err := queue.NewRiver(pool, map[string]int{"media": 1})
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
	results := make(chan error, 3)
	go func() { results <- queue.Worker{Queue: queueAdapter}.Run(runCtx) }()
	go func() { results <- (mediaapp.Cleaner{Assets: assets, Blobs: blobs}).Run(runCtx) }()
	go func() {
		results <- (mediaapp.DownloadExpiryCleaner{Repository: mediapostgres.Downloads{Pool: pool}, Blobs: blobs}).Run(runCtx)
	}()
	first := <-results
	cancel()
	second := <-results
	third := <-results
	if ctx.Err() == nil {
		for _, result := range []error{first, second, third} {
			if result != nil {
				return result
			}
		}
	}
	return nil
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
}

func (c photoSizesClient) GetSizes(ctx context.Context, keyID int64, photoID string) ([]flickrclient.PhotoSize, error) {
	return c.GetSizesClass(ctx, keyID, photoID, quotadomain.Photo)
}

func (c photoSizesClient) GetSizesClass(ctx context.Context, keyID int64, photoID string, class quotadomain.Class) ([]flickrclient.PhotoSize, error) {
	if c.Pool == nil || c.Client == nil || c.Quota == nil || keyID <= 0 {
		return nil, errors.New("crawl: photo sizes client dependencies are required")
	}
	var apiKey, status string
	if err := c.Pool.QueryRow(ctx, `SELECT key_public, status FROM api_keys WHERE id=$1`, keyID).Scan(&apiKey, &status); err != nil {
		return nil, fmt.Errorf("crawl: load photo sizes key: %w", err)
	}
	if status != "valid" {
		return nil, errors.New("crawl: photo sizes key is not valid")
	}
	if !class.Valid() {
		return nil, quotadomain.ErrInvalidClass
	}
	permit, wait, err := c.Quota.Acquire(ctx, keyID, class)
	if err != nil {
		return nil, err
	}
	if permit == nil {
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
		Selector: catalogpostgres.PhotoLane{Pool: pool},
		Keys:     keyPool,
		Settings: photoFacetSettings{settings: settingsService},
		Queue: scheduleriver.PhotoEnqueuer{
			Pool: pool, Jobs: queueAdapter, ReadyCap: settings.SyncReadyCap,
			Options: queue.Options{Queue: scheduleriver.PhotoQueue, MaxAttempts: max(1, settings.RetryMax), LeaseTimeout: settings.LeaseTimeout, Backoff: settings.RetryBackoff},
		},
	}
	return pump, nil
}

type photoFacetSettings struct{ settings *settingsapp.Service }

func (s photoFacetSettings) PhotoFacets(ctx context.Context) (scheduleapp.PhotoFacetOptions, error) {
	if s.settings == nil {
		return scheduleapp.PhotoFacetOptions{}, errors.New("scheduler: photo facet settings service is required")
	}
	configured, err := s.settings.Effective(ctx, 0)
	if err != nil {
		return scheduleapp.PhotoFacetOptions{}, err
	}
	return scheduleapp.PhotoFacetOptions{
		Info: configured.PhotoInfoEnabled, Exif: configured.PhotoExifEnabled,
		Contexts: configured.PhotoContextsEnabled, Comments: configured.PhotoCommentsEnabled,
		Favorites: configured.PhotoFavoritesEnabled,
	}, nil
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

func registerBuiltin(registry *Registry, role Role) {
	if err := registry.Register(role); err != nil {
		panic(err)
	}
}

type idleRole struct {
	name string
}

func (r idleRole) Name() string { return r.name }

func (r idleRole) Run(ctx context.Context) error {
	configureRoleReadiness(ctx, r.name)
	<-ctx.Done()
	return nil
}

// APIRole serves the health endpoint for the API process role.
type APIRole struct {
	address         string
	version         string
	handler         http.Handler
	listenerFactory func(string) (net.Listener, error)
}

// NewAPIRole creates an API role bound to address.
func NewAPIRole(address, version string) APIRole {
	return APIRole{address: address, version: version, handler: httpx.NewHealthHandler(version)}
}

func newConfiguredAPIRole(address, version string) APIRole {
	role := NewAPIRole(address, version)
	role.handler = nil
	return role
}

func (APIRole) Name() string { return "api" }

func (r APIRole) Run(ctx context.Context) error {
	if r.handler != nil {
		return r.runServer(ctx, r.handler)
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
	redisClient := redisv9.NewClient(&redisv9.Options{Addr: net.JoinHostPort(cfg.Redis.Host, strconv.Itoa(cfg.Redis.Port)), Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	defer redisClient.Close()
	hub := platformsse.NewHub(256)
	go listenSSEEvents(ctx, platformsse.Publisher{Client: redisClient, Channel: cfg.Redis.Prefix + "events"}, hub)
	handlers, err := buildConfiguredAPIHandlerSetWithHub(pool, redisClient, cfg, r.version, hub, readinessFromContext(ctx))
	if err != nil {
		return err
	}
	servers := []apiServerSpec{{address: r.address, handler: handlers.public}}
	if cfg.InternalHTTPAddr != "" {
		servers = append(servers, apiServerSpec{address: cfg.InternalHTTPAddr, handler: handlers.internal})
	}
	return r.runServers(ctx, servers)
}

func listenSSEEvents(ctx context.Context, publisher platformsse.Publisher, hub *platformsse.Hub) {
	logger := slog.Default()
	for ctx.Err() == nil {
		if err := publisher.Listen(ctx, hub); err != nil && ctx.Err() == nil {
			logger.ErrorContext(ctx, "SSE Redis subscription failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func buildConfiguredAPIHandler(pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config, version string) (http.Handler, error) {
	return buildConfiguredAPIHandlerWithHub(pool, redisClient, cfg, version, platformsse.NewHub(256))
}

func buildConfiguredAPIHandlerWithHub(pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config, version string, hub *platformsse.Hub, readiness ...*httpx.Readiness) (http.Handler, error) {
	handlers, err := buildConfiguredAPIHandlerSetWithHub(pool, redisClient, cfg, version, hub, readiness...)
	return handlers.public, err
}

func buildConfiguredAPIHandlerSetWithHub(pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config, version string, hub *platformsse.Hub, readiness ...*httpx.Readiness) (apiHandlerSet, error) {
	if pool == nil {
		return apiHandlerSet{}, errors.New("api: postgres database is required")
	}
	identity, err := buildIdentityHandler(pool, redisClient, cfg, version)
	if err != nil {
		return apiHandlerSet{}, err
	}
	keys, err := buildKeyHandler(pool, cfg)
	if err != nil {
		return apiHandlerSet{}, err
	}
	connect, err := buildConnectHandler(pool, redisClient, cfg)
	if err != nil {
		return apiHandlerSet{}, err
	}
	plans, err := buildPlanHandler(pool)
	if err != nil {
		return apiHandlerSet{}, err
	}
	flags, err := buildFeatureFlagHandler(pool)
	if err != nil {
		return apiHandlerSet{}, err
	}
	catalogRepository, err := catalogpostgres.New(pool)
	if err != nil {
		return apiHandlerSet{}, err
	}
	catalogReads := cataloghttp.Handler{Reads: catalogapp.ReadService{Repository: catalogRepository}, ActorID: userIDFromPrincipal}
	crawl, err := buildCrawlHandlerWithEvents(pool, platformsse.Publisher{Client: redisClient, Channel: cfg.Redis.Prefix + "events"})
	if err != nil {
		return apiHandlerSet{}, err
	}
	sharing, err := buildSharingHandler(pool)
	if err != nil {
		return apiHandlerSet{}, err
	}
	// The store always exists so download creation always applies the
	// temp-storage back-pressure check; an empty driver fails fast.
	blobs, err := newBlobStore(context.Background(), cfg)
	if err != nil {
		return apiHandlerSet{}, err
	}
	var probeReadiness *httpx.Readiness
	if len(readiness) > 0 {
		probeReadiness = readiness[0]
	}
	if probeReadiness != nil {
		probeReadiness.ConfigureRole("api", postgresDependency(pool), redisDependency(redisClient), blobStoreDependency(blobs))
	}
	downloads, err := buildDownloadHandlerWithEvents(pool, blobs, platformsse.Publisher{Client: redisClient, Channel: cfg.Redis.Prefix + "events"}, cfg.DownloadLinkTTL)
	if err != nil {
		return apiHandlerSet{}, err
	}
	opsHandler := opshttp.Handler{Service: ops.Service{Store: opspostgres.Store{Pool: pool}}, ActorID: userIDFromPrincipal}
	nodeRepository := clusterpostgres.New(pool)
	nodeService := clusterapp.Nodes{Repository: nodeRepository}
	nodesHandler := clusterhttp.Handler{Service: nodeService}
	return newAPIRoutersWithServices(identity, keys, connect, plans, crawl, buildQuotaHandler(pool, redisClient, cfg), sharing, downloads, opsHandler, platformsse.Handler{Hub: hub, Principal: sseUserID}, probeReadiness, flags, catalogReads, nodesHandler), nil
}

func buildFeatureFlagHandler(pool *pgxpool.Pool) (settingshttp.Handler, error) {
	repository, err := settingspostgres.NewFlagRepository(pool)
	if err != nil {
		return settingshttp.Handler{}, err
	}
	return settingshttp.Handler{Flags: settingsapp.NewFeatureFlags(repository)}, nil
}

func sseUserID(r *http.Request) (int64, bool) {
	if r == nil {
		return 0, false
	}
	principal, ok := identityhttp.PrincipalFrom(r.Context())
	return principal.User.ID, ok && principal.User.ID > 0
}

func buildQuotaHandler(pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config) quotahttp.QuotaHandler {
	return quotahttp.QuotaHandler{
		States:  quotaStates(pool, redisClient, cfg),
		Node:    nodeID(cfg),
		ActorID: userIDFromPrincipal,
	}
}

func quotaStates(pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config) quotaapp.States {
	keys := quotaPostgres.Keys{Pool: pool}
	return quotaapp.States{
		Keys:    keys,
		Budgets: quotaPostgres.Budgets{Pool: pool},
		Live:    quotaRedis.Counters{Client: redisClient, Prefix: cfg.Redis.Prefix},
		History: keys,
	}
}

// nodeID names this node in quota reports: NODE_ID, or the host name.
func nodeID(cfg config.Config) string {
	return nodeIDWithHostname(cfg, os.Hostname)
}

func nodeIDWithHostname(cfg config.Config, hostname func() (string, error)) string {
	if id := strings.TrimSpace(cfg.Node.ID); id != "" {
		return id
	}
	if host, err := hostname(); err == nil && host != "" {
		return host
	}
	return "unknown"
}

// apiServer composes identity and API key handlers. The two types expose
// different methods, so both promote onto the generated server interface.
type apiServer struct {
	identityhttp.Handler
	accountshttp.KeyHandler
	accountshttp.ConnectHandler
	crawlhttp.CrawlHandler
	quotahttp.QuotaHandler
	Sharing   sharinghttp.Handler
	Downloads mediahttp.DownloadHandler
	Ops       opshttp.Handler
	Stream    platformsse.Handler
	Readiness *httpx.Readiness
	Flags     settingshttp.Handler
	Catalog   cataloghttp.Handler
	Nodes     clusterhttp.Handler
	planHandler
}

func (s apiServer) CreateAdminNode(ctx context.Context, request gen.CreateAdminNodeRequestObject) (gen.CreateAdminNodeResponseObject, error) {
	return s.Nodes.CreateAdminNode(ctx, request)
}

func (s apiServer) HeartbeatNode(ctx context.Context, request gen.HeartbeatNodeRequestObject) (gen.HeartbeatNodeResponseObject, error) {
	return s.Nodes.HeartbeatNode(ctx, request)
}

func (s apiServer) ListPeople(ctx context.Context, request gen.ListPeopleRequestObject) (gen.ListPeopleResponseObject, error) {
	return s.Catalog.ListPeople(ctx, request)
}

func (s apiServer) GetPerson(ctx context.Context, request gen.GetPersonRequestObject) (gen.GetPersonResponseObject, error) {
	return s.Catalog.GetPerson(ctx, request)
}

func (s apiServer) ListPersonPhotos(ctx context.Context, request gen.ListPersonPhotosRequestObject) (gen.ListPersonPhotosResponseObject, error) {
	return s.Catalog.ListPersonPhotos(ctx, request)
}

func (s apiServer) ListFavorites(ctx context.Context, request gen.ListFavoritesRequestObject) (gen.ListFavoritesResponseObject, error) {
	return s.Catalog.ListFavorites(ctx, request)
}

func (s apiServer) ListAlbums(ctx context.Context, request gen.ListAlbumsRequestObject) (gen.ListAlbumsResponseObject, error) {
	return s.Catalog.ListAlbums(ctx, request)
}

func (s apiServer) ListAlbumPhotos(ctx context.Context, request gen.ListAlbumPhotosRequestObject) (gen.ListAlbumPhotosResponseObject, error) {
	return s.Catalog.ListAlbumPhotos(ctx, request)
}

func (s apiServer) GetPhoto(ctx context.Context, request gen.GetPhotoRequestObject) (gen.GetPhotoResponseObject, error) {
	return s.Catalog.GetPhoto(ctx, request)
}

func (s apiServer) GetAdminFlags(ctx context.Context, request gen.GetAdminFlagsRequestObject) (gen.GetAdminFlagsResponseObject, error) {
	return s.Flags.GetAdminFlags(ctx, request)
}

func (s apiServer) UpdateAdminFlags(ctx context.Context, request gen.UpdateAdminFlagsRequestObject) (gen.UpdateAdminFlagsResponseObject, error) {
	return s.Flags.UpdateAdminFlags(ctx, request)
}

func (s apiServer) PutMeSharing(ctx context.Context, request gen.PutMeSharingRequestObject) (gen.PutMeSharingResponseObject, error) {
	return s.Sharing.PutMeSharing(ctx, request)
}

func (s apiServer) GetReadiness(ctx context.Context, _ gen.GetReadinessRequestObject) (gen.GetReadinessResponseObject, error) {
	status, ready := s.Readiness.Evaluate(ctx)
	response := gen.Readiness{
		Status:       gen.ReadinessStatus(status.Status),
		Dependencies: make([]gen.DependencyStatus, 0, len(status.Dependencies)),
	}
	for _, dependency := range status.Dependencies {
		response.Dependencies = append(response.Dependencies, gen.DependencyStatus{
			Name:   dependency.Name,
			Status: gen.DependencyStatusStatus(dependency.Status),
		})
	}
	if ready {
		return gen.GetReadiness200JSONResponse(response), nil
	}
	return gen.GetReadiness503JSONResponse(response), nil
}

func (s apiServer) GetAdminOpsStatus(ctx context.Context, request gen.GetAdminOpsStatusRequestObject) (gen.GetAdminOpsStatusResponseObject, error) {
	return s.Ops.GetAdminOpsStatus(ctx, request)
}

func (s apiServer) CreateDownload(ctx context.Context, request gen.CreateDownloadRequestObject) (gen.CreateDownloadResponseObject, error) {
	return s.Downloads.CreateDownload(ctx, request)
}

func (s apiServer) GetDownloadParts(ctx context.Context, request gen.GetDownloadPartsRequestObject) (gen.GetDownloadPartsResponseObject, error) {
	return s.Downloads.GetDownloadParts(ctx, request)
}

func (s apiServer) MarkDownloadPartFetched(ctx context.Context, request gen.MarkDownloadPartFetchedRequestObject) (gen.MarkDownloadPartFetchedResponseObject, error) {
	return s.Downloads.MarkDownloadPartFetched(ctx, request)
}

func (s apiServer) RetryFailedDownload(ctx context.Context, request gen.RetryFailedDownloadRequestObject) (gen.RetryFailedDownloadResponseObject, error) {
	return s.Downloads.RetryFailedDownload(ctx, request)
}

func (s apiServer) StreamEvents(ctx context.Context, request gen.StreamEventsRequestObject) (gen.StreamEventsResponseObject, error) {
	userID, ok := userIDFromPrincipal(ctx)
	if !ok {
		return gen.StreamEvents401Response{}, nil
	}
	lastID := ""
	if request.Params.LastEventID != nil {
		lastID = *request.Params.LastEventID
	}
	body, err := s.Stream.Stream(ctx, userID, lastID)
	if err != nil {
		return nil, err
	}
	return streamResponse{Body: body}, nil
}

type planHandler struct{ schedulehttp.Handler }

var _ gen.StrictServerInterface = apiServer{}

func newAPIRouter(identity identityhttp.Handler, keys accountshttp.KeyHandler, connect accountshttp.ConnectHandler, plans schedulehttp.Handler, crawlers ...crawlhttp.CrawlHandler) http.Handler {
	var crawl crawlhttp.CrawlHandler
	if len(crawlers) > 0 {
		crawl = crawlers[0]
	}
	return newAPIRouterWithQuota(identity, keys, connect, plans, crawl, quotahttp.QuotaHandler{})
}

func newAPIRouterWithQuota(identity identityhttp.Handler, keys accountshttp.KeyHandler, connect accountshttp.ConnectHandler, plans schedulehttp.Handler, crawl crawlhttp.CrawlHandler, quota quotahttp.QuotaHandler, sharings ...sharinghttp.Handler) http.Handler {
	var sharing sharinghttp.Handler
	if len(sharings) > 0 {
		sharing = sharings[0]
	}
	return newAPIRouterWithServices(identity, keys, connect, plans, crawl, quota, sharing, mediahttp.DownloadHandler{})
}

type apiHandlerSet struct {
	public   http.Handler
	internal http.Handler
}

func newAPIRouterWithServices(identity identityhttp.Handler, keys accountshttp.KeyHandler, connect accountshttp.ConnectHandler, plans schedulehttp.Handler, crawl crawlhttp.CrawlHandler, quota quotahttp.QuotaHandler, sharing sharinghttp.Handler, downloads mediahttp.DownloadHandler, options ...any) http.Handler {
	return newAPIRoutersWithServices(identity, keys, connect, plans, crawl, quota, sharing, downloads, options...).public
}

func newAPIRoutersWithServices(identity identityhttp.Handler, keys accountshttp.KeyHandler, connect accountshttp.ConnectHandler, plans schedulehttp.Handler, crawl crawlhttp.CrawlHandler, quota quotahttp.QuotaHandler, sharing sharinghttp.Handler, downloads mediahttp.DownloadHandler, options ...any) apiHandlerSet {
	var opsHandler opshttp.Handler
	var flagsHandler settingshttp.Handler
	var catalogHandler cataloghttp.Handler
	var nodesHandler clusterhttp.Handler
	var streamHandler platformsse.Handler
	var readiness *httpx.Readiness
	for _, option := range options {
		if ops, ok := option.(opshttp.Handler); ok {
			opsHandler = ops
		}
		if events, ok := option.(platformsse.Handler); ok {
			streamHandler = events
		}
		if probe, ok := option.(*httpx.Readiness); ok {
			readiness = probe
		}
		if flags, ok := option.(settingshttp.Handler); ok {
			flagsHandler = flags
		}
		if catalog, ok := option.(cataloghttp.Handler); ok {
			catalogHandler = catalog
		}
		if nodes, ok := option.(clusterhttp.Handler); ok {
			nodesHandler = nodes
		}
	}
	server := apiServer{Handler: identity, KeyHandler: keys, ConnectHandler: connect, CrawlHandler: crawl, QuotaHandler: quota, Sharing: sharing, Downloads: downloads, Ops: opsHandler, Stream: streamHandler, Readiness: readiness, Flags: flagsHandler, Catalog: catalogHandler, Nodes: nodesHandler, planHandler: planHandler{Handler: plans}}
	publicRouter := chi.NewRouter()
	serveWebAssets(publicRouter, os.Getenv("XFLICKR_WEB_DIR"))
	publicRouter.Use(clusterhttp.PublicInternalPathBlocker)
	publicRouter.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(accountshttp.WithRequestAccept(r.Context(), r.Header.Get("Accept"))))
		})
	})
	publicRouter.Use(identityhttp.RequestBodyLimit)
	publicRouter.Use(identityhttp.CSRF)
	publicRouter.Use(identityhttp.ProtectRoutes(identity.Sessions, accessResolver(publicRouter)))
	strict := gen.NewStrictHandlerWithOptions(server, []gen.StrictMiddlewareFunc{
		identityhttp.Middleware,
		identityhttp.MaintainSessionCookies(identity.Sessions),
	}, gen.StrictHTTPServerOptions{RequestErrorHandlerFunc: badRequest, ResponseErrorHandlerFunc: internalError})
	public := gen.HandlerWithOptions(strict, gen.ChiServerOptions{BaseRouter: publicRouter, ErrorHandlerFunc: badRequest})

	internalRouter := chi.NewRouter()
	internalRouter.Use(clusterhttp.InternalHeartbeatOnly)
	internalRouter.Use(identityhttp.RequestBodyLimit)
	internalRouter.Use(clusterhttp.NodeAuthMiddleware(nodesHandler.Service))
	internalStrict := gen.NewStrictHandlerWithOptions(server, nil, gen.StrictHTTPServerOptions{RequestErrorHandlerFunc: badRequest, ResponseErrorHandlerFunc: internalError})
	internal := gen.HandlerWithOptions(internalStrict, gen.ChiServerOptions{BaseRouter: internalRouter, ErrorHandlerFunc: badRequest})
	return apiHandlerSet{public: public, internal: internal}
}

func buildSharingHandler(pool *pgxpool.Pool) (sharinghttp.Handler, error) {
	repository, err := sharingpostgres.New(pool)
	if err != nil {
		return sharinghttp.Handler{}, err
	}
	return sharinghttp.Handler{Change: sharingapp.Change{Repository: repository}, ActorID: userIDFromPrincipal}, nil
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

func buildCrawlHandler(pool *pgxpool.Pool) (crawlhttp.CrawlHandler, error) {
	return buildCrawlHandlerWithEvents(pool, nil)
}

func buildCrawlHandlerWithEvents(pool *pgxpool.Pool, events downloadEventPublisher) (crawlhttp.CrawlHandler, error) {
	repository, err := crawlpostgres.NewControls(pool)
	if err != nil {
		return crawlhttp.CrawlHandler{}, err
	}
	settingsRepository, err := settingspostgres.New(pool)
	if err != nil {
		return crawlhttp.CrawlHandler{}, err
	}
	depth := crawlapp.Depth{Store: crawlsettings.Depth{Settings: settingsapp.New(settingsRepository)}}
	var controls crawlhttp.ControlService = crawlapp.Controls{Repository: repository, Rescorer: scheduleriver.PersonRescorer{Pool: pool}}
	if events != nil {
		controls = publishingControls{ControlService: controls, Notifier: crawlProgressNotifier{Status: controls, Events: events}}
	}
	return crawlhttp.CrawlHandler{Controls: controls, Depth: depth, ActorID: userIDFromPrincipal}, nil
}

// flickrRESTURL is the REST endpoint; empty selects Flickr's own.
func flickrRESTURL(cfg config.Config) string {
	if cfg.Flickr.UpstreamURL == "" {
		return ""
	}
	return cfg.Flickr.UpstreamURL + "/services/rest"
}

func newFlickrClient(cfg config.Config) *flickrclient.Client {
	return flickrclient.NewClientWithContextObserver(
		flickrRESTURL(cfg),
		nil,
		flickrclient.ContextEventObserverFunc(obs.Event),
	)
}

// flickrOAuthEndpoints overrides the OAuth URLs only when an upstream is set.
func flickrOAuthEndpoints(cfg config.Config) accountsflickr.Endpoints {
	if cfg.Flickr.UpstreamURL == "" {
		return accountsflickr.Endpoints{}
	}
	base := cfg.Flickr.UpstreamURL + "/services/oauth/"
	return accountsflickr.Endpoints{RequestToken: base + "request_token", Authorize: base + "authorize", AccessToken: base + "access_token"}
}

func buildKeyHandler(pool *pgxpool.Pool, cfg config.Config) (accountshttp.KeyHandler, error) {
	if pool == nil {
		return accountshttp.KeyHandler{}, errors.New("accounts: postgres database is required")
	}
	ring, err := keyringFromConfig(cfg)
	if err != nil {
		return accountshttp.KeyHandler{}, err
	}
	repository, err := accountspostgres.New(pool)
	if err != nil {
		return accountshttp.KeyHandler{}, err
	}
	settingsRepository, err := settingspostgres.New(pool)
	if err != nil {
		return accountshttp.KeyHandler{}, err
	}
	return accountshttp.KeyHandler{
		Keys: accountsapp.Keys{
			Repository: repository,
			Probe:      accountsflickr.NewProbe(flickrRESTURL(cfg), nil),
			Sealer:     ring,
			Settings:   accountsettings.Reader{Settings: settingsapp.New(settingsRepository)},
		},
		Principal: userIDFromPrincipal,
	}, nil
}

func buildConnectHandler(pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config) (accountshttp.ConnectHandler, error) {
	if pool == nil {
		return accountshttp.ConnectHandler{}, errors.New("accounts: postgres database is required")
	}
	ring, err := keyringFromConfig(cfg)
	if err != nil {
		return accountshttp.ConnectHandler{}, err
	}
	repository, err := accountspostgres.New(pool)
	if err != nil {
		return accountshttp.ConnectHandler{}, err
	}
	return accountshttp.ConnectHandler{
		Accounts: accountsapp.Connect{
			Repository:  repository,
			Authorizer:  accountsflickr.NewAuthorizer(nil, flickrOAuthEndpoints(cfg)),
			States:      accountsredis.States{Client: redisClient, Prefix: cfg.Redis.Prefix},
			Cipher:      ring,
			CallbackURL: cfg.Flickr.CallbackURL,
		},
		Principal: userIDFromPrincipal,
	}, nil
}

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

func keyringFromConfig(cfg config.Config) (*crypto.Keyring, error) {
	if strings.TrimSpace(cfg.Crypto.MasterKeys) == "" || strings.TrimSpace(cfg.Crypto.ActiveMasterKeyID) == "" {
		return nil, errors.New("accounts: master key configuration is required")
	}
	keys, err := crypto.ParseMasterKeys(cfg.Crypto.MasterKeys)
	if err != nil {
		return nil, err
	}
	return crypto.NewKeyring(keys, strings.TrimSpace(cfg.Crypto.ActiveMasterKeyID))
}

func userIDFromPrincipal(ctx context.Context) (int64, bool) {
	principal, ok := identityhttp.PrincipalFrom(ctx)
	if !ok || principal.User.ID == 0 {
		return 0, false
	}
	return principal.User.ID, true
}

func buildIdentityHandler(pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config, version string) (identityhttp.Handler, error) {
	users := identitypostgres.New(pool)
	params := domain.Argon2Params{Memory: cfg.Identity.Argon2Memory, Iterations: cfg.Identity.Argon2Iterations, Parallelism: cfg.Identity.Argon2Parallelism, SaltLength: cfg.Identity.Argon2SaltLength, KeyLength: cfg.Identity.Argon2KeyLength}
	dummyHash, err := domain.HashPassword("xflickr-invalid-login-dummy", params)
	if err != nil {
		return identityhttp.Handler{}, err
	}
	settingsRepository, err := settingspostgres.New(pool)
	if err != nil {
		return identityhttp.Handler{}, err
	}
	settings := settingsapp.New(settingsRepository)
	return identityhttp.Handler{
		RegisterUseCase: identityapp.Register{Repository: users, Policy: settings, Params: params},
		LoginUseCase:    identityapp.Login{Repository: users, Limiter: identityredis.Limiter{Client: redisClient, Prefix: cfg.Redis.Prefix}, DummyPasswordHash: dummyHash},
		Users:           users,
		Profile:         identityapp.UpdateProfile{Repository: users},
		Password:        identityapp.ChangePassword{Repository: users, Sessions: users, Limiter: identityredis.Limiter{Client: redisClient, Prefix: cfg.Redis.Prefix}, Params: params},
		Sessions:        identityapp.Sessions{Repository: users, Users: users},
		AdminUsers:      identityapp.AdminUsers{Store: users, Audit: auditpostgres.New(pool)},
		Version:         version,
	}, nil
}

type apiServerSpec struct {
	address string
	handler http.Handler
}

func (r APIRole) runServer(ctx context.Context, handler http.Handler) error {
	return r.runServers(ctx, []apiServerSpec{{address: r.address, handler: handler}})
}

func (r APIRole) runServers(ctx context.Context, specs []apiServerSpec) error {
	listen := r.listenerFactory
	if listen == nil {
		listen = func(address string) (net.Listener, error) { return net.Listen("tcp", address) }
	}
	listeners, servers, err := openAPIServers(specs, listen)
	if err != nil {
		return err
	}
	return serveAPIServers(ctx, listeners, servers)
}

func openAPIServers(specs []apiServerSpec, listen func(string) (net.Listener, error)) ([]net.Listener, []*http.Server, error) {
	if len(specs) == 0 {
		return nil, nil, errors.New("api: at least one HTTP listener is required")
	}
	listeners := make([]net.Listener, 0, len(specs))
	servers := make([]*http.Server, 0, len(specs))
	for _, spec := range specs {
		if spec.handler == nil {
			closeAPIListeners(listeners)
			return nil, nil, errors.New("api: HTTP listener handler is required")
		}
		listener, err := listen(spec.address)
		if err != nil {
			closeAPIListeners(listeners)
			return nil, nil, fmt.Errorf("api: listen on %s: %w", spec.address, err)
		}
		listeners = append(listeners, listener)
		servers = append(servers, &http.Server{Handler: spec.handler, ReadHeaderTimeout: 10 * time.Second})
	}
	return listeners, servers, nil
}

func closeAPIListeners(listeners []net.Listener) {
	for _, listener := range listeners {
		_ = listener.Close()
	}
}

func serveAPIServers(ctx context.Context, listeners []net.Listener, servers []*http.Server) error {
	serveErr := make(chan error, len(servers))
	for index, server := range servers {
		listener := listeners[index]
		go func() { serveErr <- server.Serve(listener) }()
	}
	select {
	case <-ctx.Done():
		return shutdownAPIServers(servers)
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		shutdownErr := shutdownAPIServers(servers)
		if err != nil {
			return err
		}
		return shutdownErr
	}
}

func shutdownAPIServers(servers []*http.Server) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var first error
	for _, server := range servers {
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			if first == nil {
				first = err
			}
		}
	}
	return first
}
