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

	accountshttp "github.com/jooservices/xflickr/internal/accounts/adapters/http"
	cataloghttp "github.com/jooservices/xflickr/internal/catalog/adapters/http"
	clusterhttp "github.com/jooservices/xflickr/internal/cluster/adapters/http"
	crawlhttp "github.com/jooservices/xflickr/internal/crawl/adapters/http"
	homehttp "github.com/jooservices/xflickr/internal/home/adapters/http"
	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	mediahttp "github.com/jooservices/xflickr/internal/media/adapters/http"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/httpx"
	opshttp "github.com/jooservices/xflickr/internal/platform/ops/adapters/http"
	platformsse "github.com/jooservices/xflickr/internal/platform/sse"
	quotahttp "github.com/jooservices/xflickr/internal/quota/adapters/http"
	settingshttp "github.com/jooservices/xflickr/internal/settings/adapters/http"
	sharinghttp "github.com/jooservices/xflickr/internal/sharing/adapters/http"
)

const defaultAPIAddress = ":8080"

// apiAddress is the API listener: HTTP_PORT when it is a valid port (the E2E stack picks a free one), else :8080.
func apiAddress() string {
	port, err := strconv.Atoi(strings.TrimSpace(os.Getenv("HTTP_PORT")))
	if err != nil || port < 1 || port > 65535 {
		return defaultAPIAddress
	}
	return ":" + strconv.Itoa(port)
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
	handlers, err := buildAPIHandlerSet(apiBuildOptions{Pool: pool, Redis: redisClient, Config: cfg, Version: r.version, Hub: hub, ProbeReadiness: readinessFromContext(ctx), CallObserver: callObserver})
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

func sseUserID(r *http.Request) (int64, bool) {
	if r == nil {
		return 0, false
	}
	principal, ok := identityhttp.PrincipalFrom(r.Context())
	return principal.User.ID, ok && principal.User.ID > 0
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
