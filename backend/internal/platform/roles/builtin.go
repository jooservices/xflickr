package roles

import (
	"context"
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

	accountsflickr "github.com/jooservices/xflickr/internal/accounts/adapters/flickr"
	accountshttp "github.com/jooservices/xflickr/internal/accounts/adapters/http"
	accountspostgres "github.com/jooservices/xflickr/internal/accounts/adapters/postgres"
	accountsredis "github.com/jooservices/xflickr/internal/accounts/adapters/redis"
	accountsettings "github.com/jooservices/xflickr/internal/accounts/adapters/settings"
	accountsapp "github.com/jooservices/xflickr/internal/accounts/app"
	accountsdomain "github.com/jooservices/xflickr/internal/accounts/domain"
	catalogdownloadspostgres "github.com/jooservices/xflickr/internal/catalog/adapters/downloads/postgres"
	catalogpostgres "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	catalogapp "github.com/jooservices/xflickr/internal/catalog/app"
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
	"github.com/jooservices/xflickr/internal/platform/outbox"
	"github.com/jooservices/xflickr/internal/platform/queue"
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
	settingspostgres "github.com/jooservices/xflickr/internal/settings/adapters/postgres"
	settingsapp "github.com/jooservices/xflickr/internal/settings/app"
	sharinghttp "github.com/jooservices/xflickr/internal/sharing/adapters/http"
	sharingpostgres "github.com/jooservices/xflickr/internal/sharing/adapters/postgres"
	sharingapp "github.com/jooservices/xflickr/internal/sharing/app"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

const apiAddress = ":8080"

var builtinRoleNames = []string{"api", "scheduler", "indexer", "node-agent", "crawl", "media", "sync", "ai-worker"}

// NewBuiltinRegistry creates the role set supported by the xflickr binary.
func NewBuiltinRegistry(version string) *Registry {
	registry := NewRegistry()
	registerBuiltin(registry, newConfiguredAPIRole(apiAddress, version))
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
	pool, err := db.NewPool(ctx, cfg.DB)
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
	pump, err := newSchedulerPump(pool, quota.Governor, store, queueAdapter, settings)
	if err != nil {
		return err
	}
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
	pool, err := db.NewPool(ctx, cfg.DB)
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
	service := crawlapp.Interests{Repository: interestsRepository, Settings: settingsapp.New(settingsRepository)}
	return outbox.NewConsumer(pool).Consume(ctx, accountsdomain.ConnectedTopic, func(ctx context.Context, event outbox.Event) error {
		return service.HandleConnected(ctx, event.Payload)
	})
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
	pool, err := db.NewPool(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer pool.Close()
	redisClient := redisv9.NewClient(&redisv9.Options{Addr: net.JoinHostPort(cfg.Redis.Host, strconv.Itoa(cfg.Redis.Port)), Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	defer redisClient.Close()
	queueAdapter, err := queue.NewRiver(pool, map[string]int{scheduleriver.PhotoQueue: 1})
	if err != nil {
		return err
	}
	catalogRepository, err := catalogpostgres.New(pool)
	if err != nil {
		return err
	}
	quota := quotaRedis.Governor{Client: redisClient, Prefix: cfg.Redis.Prefix, Budgets: quotaPostgres.Budgets{Pool: pool}, Recorder: quotaPostgres.CallLog{Pool: pool}}
	client := flickrclient.NewClient("", nil)
	handler := crawlapp.Sizes{
		Store:  catalogRepository,
		Flickr: photoSizesClient{Pool: pool, Client: client, Quota: quota},
	}
	if err := queueAdapter.RegisterWithOptions(scheduleriver.PhotoJobKind, func(ctx context.Context, job queue.Job) error {
		args, err := crawlapp.ParseSizesJob(job.Args)
		if err != nil {
			return err
		}
		return handler.Handle(ctx, args)
	}, queue.Options{Queue: scheduleriver.PhotoQueue}); err != nil {
		return err
	}
	return queueAdapter.Start(ctx)
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
	pool, err := db.NewPool(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer pool.Close()
	assets, err := catalogpostgres.New(pool)
	if err != nil {
		return err
	}
	var blobs mediaapp.BlobStore
	switch cfg.Blob.Driver {
	case "local":
		blobs, err = medialocal.New(cfg.Blob.LocalDir, cfg.Blob.MaxBytes)
	case "s3":
		blobs, err = medias3.New(ctx, medias3.Config{
			Endpoint: cfg.Blob.S3Endpoint, Region: cfg.Blob.S3Region, Bucket: cfg.Blob.S3Bucket,
			AccessKey: cfg.Blob.S3AccessKey, SecretKey: cfg.Blob.S3SecretKey,
			UsePathStyle: cfg.Blob.S3UsePathStyle, MaxBytes: cfg.Blob.MaxBytes, MaxTTL: cfg.Blob.MaxTTL,
		})
	default:
		return fmt.Errorf("media: unsupported blob driver %q", cfg.Blob.Driver)
	}
	if err != nil {
		return err
	}
	queueAdapter, err := queue.NewRiver(pool, map[string]int{"media": 1})
	if err != nil {
		return err
	}
	packer := mediaapp.Packer{Blobs: blobs, Repo: mediapostgres.Downloads{Pool: pool}}
	if err := queueAdapter.RegisterWithOptions("media.pack", func(ctx context.Context, job queue.Job) error {
		args, err := mediaapp.ParsePackJob(job.Args)
		if err != nil {
			return err
		}
		return packer.Pack(ctx, args.DownloadID)
	}, queue.Options{Queue: "media", MaxAttempts: 3, LeaseTimeout: 2 * time.Hour}); err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 2)
	go func() { results <- queueAdapter.Start(runCtx) }()
	go func() { results <- (mediaapp.Cleaner{Assets: assets, Blobs: blobs}).Run(runCtx) }()
	first := <-results
	cancel()
	second := <-results
	if first != nil && ctx.Err() == nil {
		return first
	}
	if second != nil && ctx.Err() == nil {
		return second
	}
	return nil
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
	permit, _, err := c.Quota.Acquire(ctx, keyID, class)
	if err != nil {
		return nil, err
	}
	if permit == nil {
		return nil, flickrclient.ErrQuotaExceeded
	}
	sizes, callErr := c.Client.PhotosGetSizes(ctx, flickrclient.Credentials{APIKey: apiKey}, photoID)
	if doneErr := permit.Done(quotadomain.Meta{Err: callErr}); doneErr != nil {
		return nil, fmt.Errorf("crawl: release photo sizes quota permit: %w", doneErr)
	}
	return sizes, callErr
}

func newSchedulerPump(pool *pgxpool.Pool, governor quotaRedis.Governor, store scheduleapp.PumpStore, queueAdapter queue.JobQueue, settings scheduleapp.PumpSettings) (scheduleapp.Pump, error) {
	cycleRepository, err := crawlpostgres.NewCycleRepository(pool)
	if err != nil {
		return scheduleapp.Pump{}, err
	}
	effectiveSettingsRepository, err := settingspostgres.New(pool)
	if err != nil {
		return scheduleapp.Pump{}, err
	}
	pump := scheduleapp.Pump{Store: store, QueueState: queuepostgres.New(pool), Credits: governor, Queue: queueAdapter, Settings: settings}
	pump.ChangeChecks = crawlapp.Cycle{Store: cycleRepository, Users: cycleRepository, Settings: settingsapp.New(effectiveSettingsRepository)}
	return pump, nil
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
	pool, err := db.NewPool(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer pool.Close()
	redisClient := redisv9.NewClient(&redisv9.Options{Addr: net.JoinHostPort(cfg.Redis.Host, strconv.Itoa(cfg.Redis.Port)), Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	defer redisClient.Close()
	handler, err := buildConfiguredAPIHandler(pool, redisClient, cfg, r.version)
	if err != nil {
		return err
	}
	return r.runServer(ctx, handler)
}

func buildConfiguredAPIHandler(pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config, version string) (http.Handler, error) {
	if pool == nil {
		return nil, errors.New("api: postgres database is required")
	}
	identity, err := buildIdentityHandler(pool, redisClient, cfg, version)
	if err != nil {
		return nil, err
	}
	keys, err := buildKeyHandler(pool, cfg)
	if err != nil {
		return nil, err
	}
	connect, err := buildConnectHandler(pool, redisClient, cfg)
	if err != nil {
		return nil, err
	}
	plans, err := buildPlanHandler(pool)
	if err != nil {
		return nil, err
	}
	crawl, err := buildCrawlHandler(pool)
	if err != nil {
		return nil, err
	}
	sharing, err := buildSharingHandler(pool)
	if err != nil {
		return nil, err
	}
	downloads, err := buildDownloadHandler(pool)
	if err != nil {
		return nil, err
	}
	return newAPIRouterWithServices(identity, keys, connect, plans, crawl, buildQuotaHandler(pool, redisClient, cfg), sharing, downloads), nil
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
	planHandler
}

func (s apiServer) PutMeSharing(ctx context.Context, request gen.PutMeSharingRequestObject) (gen.PutMeSharingResponseObject, error) {
	return s.Sharing.PutMeSharing(ctx, request)
}

func (s apiServer) CreateDownload(ctx context.Context, request gen.CreateDownloadRequestObject) (gen.CreateDownloadResponseObject, error) {
	return s.Downloads.CreateDownload(ctx, request)
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

func newAPIRouterWithServices(identity identityhttp.Handler, keys accountshttp.KeyHandler, connect accountshttp.ConnectHandler, plans schedulehttp.Handler, crawl crawlhttp.CrawlHandler, quota quotahttp.QuotaHandler, sharing sharinghttp.Handler, downloads mediahttp.DownloadHandler) http.Handler {
	router := chi.NewRouter()
	serveWebAssets(router, os.Getenv("XFLICKR_WEB_DIR"))
	router.Use(identityhttp.RequestBodyLimit)
	router.Use(identityhttp.CSRF)
	router.Use(identityhttp.ProtectRoutes(identity.Sessions))
	return gen.HandlerFromMux(gen.NewStrictHandler(apiServer{Handler: identity, KeyHandler: keys, ConnectHandler: connect, CrawlHandler: crawl, QuotaHandler: quota, Sharing: sharing, Downloads: downloads, planHandler: planHandler{Handler: plans}}, []gen.StrictMiddlewareFunc{
		identityhttp.Middleware,
		identityhttp.MaintainSessionCookies(identity.Sessions),
	}), router)
}

func buildSharingHandler(pool *pgxpool.Pool) (sharinghttp.Handler, error) {
	repository, err := sharingpostgres.New(pool)
	if err != nil {
		return sharinghttp.Handler{}, err
	}
	return sharinghttp.Handler{Change: sharingapp.Change{Repository: repository}, ActorID: userIDFromPrincipal}, nil
}

func buildDownloadHandler(pool *pgxpool.Pool) (mediahttp.DownloadHandler, error) {
	jobs, err := queue.NewRiver(pool, nil)
	if err != nil {
		return mediahttp.DownloadHandler{}, err
	}
	catalogRepository := catalogdownloadspostgres.Repository{Pool: pool}
	repository := mediapostgres.Downloads{Pool: pool, Jobs: jobs}
	return mediahttp.DownloadHandler{Downloads: mediaapp.Downloads{Catalog: catalogapp.DownloadCatalog{Photos: catalogRepository}, Repository: repository}, Principal: userIDFromPrincipal}, nil
}

func buildCrawlHandler(pool *pgxpool.Pool) (crawlhttp.CrawlHandler, error) {
	repository, err := crawlpostgres.NewControls(pool)
	if err != nil {
		return crawlhttp.CrawlHandler{}, err
	}
	settingsRepository, err := settingspostgres.New(pool)
	if err != nil {
		return crawlhttp.CrawlHandler{}, err
	}
	depth := crawlapp.Depth{Store: crawlsettings.Depth{Settings: settingsapp.New(settingsRepository)}}
	return crawlhttp.CrawlHandler{Controls: crawlapp.Controls{Repository: repository, Rescorer: scheduleriver.PersonRescorer{Pool: pool}}, Depth: depth, ActorID: userIDFromPrincipal}, nil
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
			Probe:      accountsflickr.NewProbe("", nil),
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
			Authorizer:  accountsflickr.NewAuthorizer(nil, accountsflickr.Endpoints{}),
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

func (r APIRole) runServer(ctx context.Context, handler http.Handler) error {
	listen := r.listenerFactory
	if listen == nil {
		listen = func(address string) (net.Listener, error) { return net.Listen("tcp", address) }
	}
	listener, err := listen(r.address)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		return nil
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
