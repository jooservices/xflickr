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
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	redisv9 "github.com/redis/go-redis/v9"

	accountsflickr "github.com/jooservices/xflickr/internal/accounts/adapters/flickr"
	accountshttp "github.com/jooservices/xflickr/internal/accounts/adapters/http"
	accountspostgres "github.com/jooservices/xflickr/internal/accounts/adapters/postgres"
	accountsredis "github.com/jooservices/xflickr/internal/accounts/adapters/redis"
	accountsettings "github.com/jooservices/xflickr/internal/accounts/adapters/settings"
	accountsapp "github.com/jooservices/xflickr/internal/accounts/app"
	catalogdownloadspostgres "github.com/jooservices/xflickr/internal/catalog/adapters/downloads/postgres"
	cataloghttp "github.com/jooservices/xflickr/internal/catalog/adapters/http"
	catalogpostgres "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	catalogapp "github.com/jooservices/xflickr/internal/catalog/app"
	clusterhttp "github.com/jooservices/xflickr/internal/cluster/adapters/http"
	clusterpostgres "github.com/jooservices/xflickr/internal/cluster/adapters/postgres"
	clusterapp "github.com/jooservices/xflickr/internal/cluster/app"
	clusterdomain "github.com/jooservices/xflickr/internal/cluster/domain"
	crawlhttp "github.com/jooservices/xflickr/internal/crawl/adapters/http"
	homehttp "github.com/jooservices/xflickr/internal/home/adapters/http"
	homepostgres "github.com/jooservices/xflickr/internal/home/adapters/postgres"
	homeapp "github.com/jooservices/xflickr/internal/home/app"
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
	opsstore "github.com/jooservices/xflickr/internal/ops/adapters/postgres"
	opsapp "github.com/jooservices/xflickr/internal/ops/app"
	opsdomain "github.com/jooservices/xflickr/internal/ops/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	auditpostgres "github.com/jooservices/xflickr/internal/platform/audit/adapters/postgres"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/crypto"
	"github.com/jooservices/xflickr/internal/platform/db"
	"github.com/jooservices/xflickr/internal/platform/httpx"
	metricspostgres "github.com/jooservices/xflickr/internal/platform/metrics/adapters/postgres"
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
	schedulehttp "github.com/jooservices/xflickr/internal/schedule/adapters/http"
	scheduleriver "github.com/jooservices/xflickr/internal/schedule/adapters/river"
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
		registerBuiltin(registry, metricsWrappedRole{inner: idleRole{name: name}, roleName: name})
	}
	return registry
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
	callObserver, stopCallObserver := startFlickrCallLog(ctx, pool)
	defer stopCallObserver()
	redisClient := newRedisClient(cfg)
	defer redisClient.Close()
	stopMetrics := startMetricsCollection(ctx, pool, redisClient, cfg, "api", r.version, false, slog.Default())
	defer stopMetrics()
	hub := platformsse.NewHub(256)
	go listenSSEEvents(ctx, platformsse.Publisher{Client: redisClient, Channel: cfg.Redis.Prefix + "events"}, hub)
	handlers, err := buildConfiguredAPIHandlerSetWithObserver(pool, redisClient, cfg, r.version, hub, readinessFromContext(ctx), callObserver)
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
	var probeReadiness *httpx.Readiness
	if len(readiness) > 0 {
		probeReadiness = readiness[0]
	}
	return buildConfiguredAPIHandlerSetWithObserver(pool, redisClient, cfg, version, hub, probeReadiness, nil)
}

func buildConfiguredAPIHandlerSetWithObserver(pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config, version string, hub *platformsse.Hub, probeReadiness *httpx.Readiness, callObserver flickrclient.CallObserver) (apiHandlerSet, error) {
	if pool == nil {
		return apiHandlerSet{}, errors.New("api: postgres database is required")
	}
	identity, err := buildIdentityHandler(pool, redisClient, cfg, version)
	if err != nil {
		return apiHandlerSet{}, err
	}
	keys, err := buildKeyHandler(pool, cfg, callObserver)
	if err != nil {
		return apiHandlerSet{}, err
	}
	connect, err := buildConnectHandler(pool, redisClient, cfg, callObserver)
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
	if probeReadiness != nil {
		probeReadiness.ConfigureRole("api", postgresDependency(pool), redisDependency(redisClient), blobStoreDependency(blobs))
	}
	downloads, err := buildDownloadHandlerWithEvents(pool, blobs, platformsse.Publisher{Client: redisClient, Channel: cfg.Redis.Prefix + "events"}, cfg.DownloadLinkTTL)
	if err != nil {
		return apiHandlerSet{}, err
	}
	incidentReader, err := opsstore.NewOpenIncidentRepository(pool)
	if err != nil {
		return apiHandlerSet{}, fmt.Errorf("api: create open incident reader: %w", err)
	}
	openIncidents, err := opsapp.NewOpenIncidents(incidentReader)
	if err != nil {
		return apiHandlerSet{}, fmt.Errorf("api: create open incident use case: %w", err)
	}
	opsHandler := opshttp.Handler{
		Service:       ops.Service{Store: opspostgres.Store{Pool: pool, DB: pool}},
		Metrics:       metricspostgres.SeriesStore{Pool: pool},
		OpenIncidents: openIncidents,
		Now:           time.Now,
		ActorID:       userIDFromPrincipal,
	}
	nodeService, err := buildNodeService(pool)
	if err != nil {
		return apiHandlerSet{}, err
	}
	nodesHandler := clusterhttp.Handler{Service: nodeService}
	homeHandler, err := buildHomeHandler(pool)
	if err != nil {
		return apiHandlerSet{}, err
	}
	return newAPIRoutersWithServices(identity, keys, connect, plans, crawl, buildQuotaHandler(pool, redisClient, cfg), sharing, downloads, opsHandler, platformsse.Handler{Hub: hub, Principal: sseUserID}, probeReadiness, flags, catalogReads, nodesHandler, homeHandler), nil
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

type duplicateEgressChecker struct {
	nodes     duplicateEgressFinder
	incidents duplicateIncidentOpener
}

type duplicateEgressFinder interface {
	FindDuplicateEgress(context.Context, clusterdomain.Node, time.Time) (string, error)
}

type duplicateIncidentOpener interface {
	Open(context.Context, opsdomain.Incident) (opsdomain.Incident, error)
}

func (checker duplicateEgressChecker) CheckDuplicate(ctx context.Context, node clusterdomain.Node, at time.Time) error {
	if node.EgressIP == "" || node.EffectiveState(at) != clusterdomain.StateOnline || !slices.Contains(node.Roles, "crawl") {
		return nil
	}
	duplicate, err := checker.nodes.FindDuplicateEgress(ctx, node, at)
	if err != nil {
		return fmt.Errorf("cluster: check duplicate egress IP: %w", err)
	}
	if duplicate == "" {
		return nil
	}
	names := []string{node.Name, duplicate}
	sort.Strings(names)
	_, err = checker.incidents.Open(ctx, opsdomain.Incident{
		Kind:     "duplicate_egress_ip",
		Scope:    fmt.Sprintf("%d:%s|%d:%s", len(names[0]), names[0], len(names[1]), names[1]),
		Severity: opsdomain.SeverityWarning,
		Title:    "Duplicate egress IP",
		Detail:   json.RawMessage("{}"),
	})
	if err != nil {
		return fmt.Errorf("cluster: open duplicate egress incident: %w", err)
	}
	return nil
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
	Home      homehttp.Handler
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

func (s apiServer) GetMeHome(ctx context.Context, request gen.GetMeHomeRequestObject) (gen.GetMeHomeResponseObject, error) {
	return s.Home.GetMeHome(ctx, request)
}

func (s apiServer) ListContacts(ctx context.Context, request gen.ListContactsRequestObject) (gen.ListContactsResponseObject, error) {
	return s.Catalog.ListContacts(ctx, request)
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

func (s apiServer) ListMyAlbums(ctx context.Context, request gen.ListMyAlbumsRequestObject) (gen.ListMyAlbumsResponseObject, error) {
	return s.Catalog.ListMyAlbums(ctx, request)
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

// GetAdminFlag delegates the one-flag read to the settings HTTP adapter.
func (s apiServer) GetAdminFlag(ctx context.Context, request gen.GetAdminFlagRequestObject) (gen.GetAdminFlagResponseObject, error) {
	return s.Flags.GetAdminFlag(ctx, request)
}

func (s apiServer) UpdateAdminFlags(ctx context.Context, request gen.UpdateAdminFlagsRequestObject) (gen.UpdateAdminFlagsResponseObject, error) {
	return s.Flags.UpdateAdminFlags(ctx, request)
}

func (s apiServer) PutMeSharing(ctx context.Context, request gen.PutMeSharingRequestObject) (gen.PutMeSharingResponseObject, error) {
	return s.Sharing.PutMeSharing(ctx, request)
}

func (s apiServer) GetMeSharing(ctx context.Context, request gen.GetMeSharingRequestObject) (gen.GetMeSharingResponseObject, error) {
	return s.Sharing.GetMeSharing(ctx, request)
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

func (s apiServer) ListAdminOpenIncidents(ctx context.Context, request gen.ListAdminOpenIncidentsRequestObject) (gen.ListAdminOpenIncidentsResponseObject, error) {
	return s.Ops.ListAdminOpenIncidents(ctx, request)
}

func (s apiServer) GetAdminMetrics(ctx context.Context, request gen.GetAdminMetricsRequestObject) (gen.GetAdminMetricsResponseObject, error) {
	return s.Ops.GetAdminMetrics(ctx, request)
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
	var homeHandler homehttp.Handler
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
		if home, ok := option.(homehttp.Handler); ok {
			homeHandler = home
		}
	}
	server := apiServer{Handler: identity, KeyHandler: keys, ConnectHandler: connect, CrawlHandler: crawl, QuotaHandler: quota, Sharing: sharing, Home: homeHandler, Downloads: downloads, Ops: opsHandler, Stream: streamHandler, Readiness: readiness, Flags: flagsHandler, Catalog: catalogHandler, Nodes: nodesHandler, planHandler: planHandler{Handler: plans}}
	publicRouter := chi.NewRouter()
	serveWebAssets(publicRouter, os.Getenv("XFLICKR_WEB_DIR"))
	publicRouter.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeAPIError(w, r, "method-not-allowed", "Method not allowed")
	})
	publicRouter.Use(identityhttp.LegacyIdentityRoutes)
	publicRouter.Use(clusterhttp.PublicInternalPathBlocker)
	publicRouter.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(accountshttp.WithRequestAccept(r.Context(), r.Header.Get("Accept"))))
		})
	})
	publicRouter.Use(identityhttp.RequestBodyLimit)
	publicRouter.Use(identityhttp.PrivateIdentityCache)
	publicRouter.Use(identityhttp.CSRF)
	publicRouter.Use(identityhttp.ProtectRoutes(identity.Sessions, accessResolver(publicRouter)))
	publicRouter.Use(metricsQueryMultiplicity)
	publicRouter.Use(identityhttp.Contract)
	strict := gen.NewStrictHandlerWithOptions(server, []gen.StrictMiddlewareFunc{
		identityhttp.Middleware,
		discoveryAuthentication(identity.Sessions),
		flagReadAuthentication(identity.Sessions),
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
