package roles

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	redisv9 "github.com/redis/go-redis/v9"

	accountsflickr "github.com/jooservices/xflickr/internal/accounts/adapters/flickr"
	accountshttp "github.com/jooservices/xflickr/internal/accounts/adapters/http"
	accountspostgres "github.com/jooservices/xflickr/internal/accounts/adapters/postgres"
	accountsredis "github.com/jooservices/xflickr/internal/accounts/adapters/redis"
	accountsettings "github.com/jooservices/xflickr/internal/accounts/adapters/settings"
	accountsapp "github.com/jooservices/xflickr/internal/accounts/app"
	cataloghttp "github.com/jooservices/xflickr/internal/catalog/adapters/http"
	catalogpostgres "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	catalogapp "github.com/jooservices/xflickr/internal/catalog/app"
	clusterhttp "github.com/jooservices/xflickr/internal/cluster/adapters/http"
	clusterpostgres "github.com/jooservices/xflickr/internal/cluster/adapters/postgres"
	clusterapp "github.com/jooservices/xflickr/internal/cluster/app"
	homehttp "github.com/jooservices/xflickr/internal/home/adapters/http"
	homepostgres "github.com/jooservices/xflickr/internal/home/adapters/postgres"
	homeapp "github.com/jooservices/xflickr/internal/home/app"
	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	identitypostgres "github.com/jooservices/xflickr/internal/identity/adapters/postgres"
	identityredis "github.com/jooservices/xflickr/internal/identity/adapters/redis"
	identityapp "github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
	opsstore "github.com/jooservices/xflickr/internal/ops/adapters/postgres"
	opsapp "github.com/jooservices/xflickr/internal/ops/app"
	auditpostgres "github.com/jooservices/xflickr/internal/platform/audit/adapters/postgres"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/crypto"
	"github.com/jooservices/xflickr/internal/platform/httpx"
	metricspostgres "github.com/jooservices/xflickr/internal/platform/metrics/adapters/postgres"
	"github.com/jooservices/xflickr/internal/platform/obs"
	"github.com/jooservices/xflickr/internal/platform/ops"
	opshttp "github.com/jooservices/xflickr/internal/platform/ops/adapters/http"
	opspostgres "github.com/jooservices/xflickr/internal/platform/ops/adapters/postgres"
	platformsse "github.com/jooservices/xflickr/internal/platform/sse"
	quotahttp "github.com/jooservices/xflickr/internal/quota/adapters/http"
	quotaPostgres "github.com/jooservices/xflickr/internal/quota/adapters/postgres"
	quotaRedis "github.com/jooservices/xflickr/internal/quota/adapters/redis"
	quotaapp "github.com/jooservices/xflickr/internal/quota/app"
	settingshttp "github.com/jooservices/xflickr/internal/settings/adapters/http"
	settingspostgres "github.com/jooservices/xflickr/internal/settings/adapters/postgres"
	settingsapp "github.com/jooservices/xflickr/internal/settings/app"
	sharinghttp "github.com/jooservices/xflickr/internal/sharing/adapters/http"
	sharingpostgres "github.com/jooservices/xflickr/internal/sharing/adapters/postgres"
	sharingapp "github.com/jooservices/xflickr/internal/sharing/app"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

// apiBuildOptions are the inputs of the API composition root.
type apiBuildOptions struct {
	Pool           *pgxpool.Pool
	Redis          redisv9.UniversalClient
	Config         config.Config
	Version        string
	Hub            *platformsse.Hub
	ProbeReadiness *httpx.Readiness
	CallObserver   flickrclient.CallObserver
}

// buildAPIHandlerSet wires every API handler in a fixed order and mounts them.
func buildAPIHandlerSet(opts apiBuildOptions) (apiHandlerSet, error) {
	if opts.Pool == nil {
		return apiHandlerSet{}, errors.New("api: postgres database is required")
	}
	var routes apiRouterOptions
	if err := fillAccountHandlers(opts, &routes); err != nil {
		return apiHandlerSet{}, err
	}
	if err := fillContentHandlers(opts, &routes); err != nil {
		return apiHandlerSet{}, err
	}
	if err := fillStorageHandlers(opts, &routes); err != nil {
		return apiHandlerSet{}, err
	}
	if err := fillOperationalHandlers(opts.Pool, &routes); err != nil {
		return apiHandlerSet{}, err
	}
	routes.Quota = buildQuotaHandler(opts.Pool, opts.Redis, opts.Config)
	routes.Stream = platformsse.Handler{Hub: opts.Hub, Principal: sseUserID}
	routes.Readiness = opts.ProbeReadiness
	return newAPIRouterSet(routes), nil
}

// fillAccountHandlers wires identity, API keys, OAuth connect, plans and flags.
func fillAccountHandlers(opts apiBuildOptions, routes *apiRouterOptions) error {
	identity, err := buildIdentityHandler(opts.Pool, opts.Redis, opts.Config, opts.Version)
	if err != nil {
		return err
	}
	routes.Identity = identity
	keys, err := buildKeyHandler(opts.Pool, opts.Config, opts.CallObserver)
	if err != nil {
		return err
	}
	routes.Keys = keys
	connect, err := buildConnectHandler(opts.Pool, opts.Redis, opts.Config, opts.CallObserver)
	if err != nil {
		return err
	}
	routes.Connect = connect
	plans, err := buildPlanHandler(opts.Pool)
	if err != nil {
		return err
	}
	routes.Plans = plans
	flags, err := buildFeatureFlagHandler(opts.Pool)
	if err != nil {
		return err
	}
	routes.Flags = flags
	return nil
}

// fillContentHandlers wires the catalog, crawl and sharing handlers.
func fillContentHandlers(opts apiBuildOptions, routes *apiRouterOptions) error {
	catalogRepository, err := catalogpostgres.New(opts.Pool)
	if err != nil {
		return err
	}
	routes.Catalog = cataloghttp.Handler{Reads: catalogapp.ReadService{Repository: catalogRepository}, ActorID: userIDFromPrincipal}
	crawl, err := buildCrawlHandlerWithEvents(opts.Pool, platformsse.Publisher{Client: opts.Redis, Channel: opts.Config.Redis.Prefix + "events"})
	if err != nil {
		return err
	}
	routes.Crawl = crawl
	sharing, err := buildSharingHandler(opts.Pool)
	if err != nil {
		return err
	}
	routes.Sharing = sharing
	return nil
}

// fillStorageHandlers wires the blob store, the readiness probe and downloads.
func fillStorageHandlers(opts apiBuildOptions, routes *apiRouterOptions) error {
	// The store always exists so download creation always applies the
	// temp-storage back-pressure check; an empty driver fails fast.
	blobs, err := newBlobStore(context.Background(), opts.Config)
	if err != nil {
		return err
	}
	if opts.ProbeReadiness != nil {
		opts.ProbeReadiness.ConfigureRole("api", postgresDependency(opts.Pool), redisDependency(opts.Redis), blobStoreDependency(blobs))
	}
	downloads, err := buildDownloadHandlerWithEvents(opts.Pool, blobs, platformsse.Publisher{Client: opts.Redis, Channel: opts.Config.Redis.Prefix + "events"}, opts.Config.DownloadLinkTTL)
	if err != nil {
		return err
	}
	routes.Downloads = downloads
	return nil
}

// fillOperationalHandlers wires operations, cluster nodes and the home page.
func fillOperationalHandlers(pool *pgxpool.Pool, routes *apiRouterOptions) error {
	incidentReader, err := opsstore.NewOpenIncidentRepository(pool)
	if err != nil {
		return fmt.Errorf("api: create open incident reader: %w", err)
	}
	openIncidents, err := opsapp.NewOpenIncidents(incidentReader)
	if err != nil {
		return fmt.Errorf("api: create open incident use case: %w", err)
	}
	routes.Ops = opshttp.Handler{
		Service:       ops.Service{Store: opspostgres.Store{Pool: pool, DB: pool}},
		Metrics:       metricspostgres.SeriesStore{Pool: pool},
		OpenIncidents: openIncidents,
		Now:           time.Now,
		ActorID:       userIDFromPrincipal,
	}
	nodeService, err := buildNodeService(pool)
	if err != nil {
		return err
	}
	routes.Nodes = clusterhttp.Handler{Service: nodeService}
	home, err := buildHomeHandler(pool)
	if err != nil {
		return err
	}
	routes.Home = home
	return nil
}

func buildNodeService(pool *pgxpool.Pool) (clusterapp.Nodes, error) {
	nodeRepository := clusterpostgres.New(pool)
	incidentRepository, err := opsstore.NewIncidentRepository(pool)
	if err != nil {
		return clusterapp.Nodes{}, fmt.Errorf("api: create duplicate egress incident repository: %w", err)
	}
	return clusterapp.Nodes{
		Repository:       nodeRepository,
		DuplicateChecker: duplicateEgressChecker{nodes: nodeRepository, incidents: opsapp.NewIncidents(incidentRepository)},
		Logger:           slog.Default(),
	}, nil
}

func buildFeatureFlagHandler(pool *pgxpool.Pool) (settingshttp.Handler, error) {
	repository, err := settingspostgres.NewFlagRepository(pool)
	if err != nil {
		return settingshttp.Handler{}, err
	}
	return settingshttp.Handler{Flags: settingsapp.NewFeatureFlags(repository)}, nil
}

func buildQuotaHandler(pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config) quotahttp.QuotaHandler {
	return quotahttp.QuotaHandler{
		States:    quotaStates(pool, redisClient, cfg),
		CallStats: quotaPostgres.CallLog{Pool: pool},
		Node:      nodeID(cfg),
		ActorID:   userIDFromPrincipal,
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

type apiHandlerSet struct {
	public   http.Handler
	internal http.Handler
}

func buildSharingHandler(pool *pgxpool.Pool) (sharinghttp.Handler, error) {
	repository, err := sharingpostgres.New(pool)
	if err != nil {
		return sharinghttp.Handler{}, err
	}
	return sharinghttp.Handler{Change: sharingapp.Change{Repository: repository}, Read: sharingapp.Read{Repository: repository}, ActorID: userIDFromPrincipal}, nil
}

func buildHomeHandler(pool *pgxpool.Pool) (homehttp.Handler, error) {
	repository, err := homepostgres.New(pool)
	if err != nil {
		return homehttp.Handler{}, err
	}
	service, err := homeapp.New(repository, time.Now)
	if err != nil {
		return homehttp.Handler{}, err
	}
	return homehttp.Handler{Snapshots: service, ActorID: userIDFromPrincipal}, nil
}

// flickrRESTURL is the REST endpoint; empty selects Flickr's own.
func flickrRESTURL(cfg config.Config) string {
	if cfg.Flickr.UpstreamURL == "" {
		return ""
	}
	return cfg.Flickr.UpstreamURL + "/services/rest"
}

func newFlickrClient(cfg config.Config, observers ...flickrclient.CallObserver) *flickrclient.Client {
	client := flickrclient.NewClientWithContextObserver(
		flickrRESTURL(cfg),
		nil,
		flickrclient.ContextEventObserverFunc(obs.Event),
	)
	if len(observers) > 0 {
		client.SetCallObserver(observers[0])
	}
	return client
}

// flickrOAuthEndpoints overrides the OAuth URLs only when an upstream is set.
func flickrOAuthEndpoints(cfg config.Config) accountsflickr.Endpoints {
	if cfg.Flickr.UpstreamURL == "" {
		return accountsflickr.Endpoints{}
	}
	base := cfg.Flickr.UpstreamURL + "/services/oauth/"
	return accountsflickr.Endpoints{RequestToken: base + "request_token", Authorize: base + "authorize", AccessToken: base + "access_token"}
}

func buildKeyHandler(pool *pgxpool.Pool, cfg config.Config, observers ...flickrclient.CallObserver) (accountshttp.KeyHandler, error) {
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
			Probe:      accountsflickr.NewProbe(flickrRESTURL(cfg), nil, observers...),
			Sealer:     ring,
			Settings:   accountsettings.Reader{Settings: settingsapp.New(settingsRepository)},
		},
		Principal: userIDFromPrincipal,
	}, nil
}

func buildConnectHandler(pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config, observers ...flickrclient.CallObserver) (accountshttp.ConnectHandler, error) {
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
			Authorizer:  accountsflickr.NewAuthorizer(nil, flickrOAuthEndpoints(cfg), observers...),
			States:      accountsredis.States{Client: redisClient, Prefix: cfg.Redis.Prefix},
			Cipher:      ring,
			CallbackURL: cfg.Flickr.CallbackURL,
		},
		Principal: userIDFromPrincipal,
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
