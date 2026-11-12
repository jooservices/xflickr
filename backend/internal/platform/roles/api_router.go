package roles

import (
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"

	accountshttp "github.com/jooservices/xflickr/internal/accounts/adapters/http"
	cataloghttp "github.com/jooservices/xflickr/internal/catalog/adapters/http"
	clusterhttp "github.com/jooservices/xflickr/internal/cluster/adapters/http"
	crawlhttp "github.com/jooservices/xflickr/internal/crawl/adapters/http"
	homehttp "github.com/jooservices/xflickr/internal/home/adapters/http"
	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	mediahttp "github.com/jooservices/xflickr/internal/media/adapters/http"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/httpx"
	opshttp "github.com/jooservices/xflickr/internal/platform/ops/adapters/http"
	platformsse "github.com/jooservices/xflickr/internal/platform/sse"
	quotahttp "github.com/jooservices/xflickr/internal/quota/adapters/http"
	schedulehttp "github.com/jooservices/xflickr/internal/schedule/adapters/http"
	settingshttp "github.com/jooservices/xflickr/internal/settings/adapters/http"
	sharinghttp "github.com/jooservices/xflickr/internal/sharing/adapters/http"
)

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
