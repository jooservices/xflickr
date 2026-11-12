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

// apiRouterOptions holds every handler the API mounts. A zero field is valid
// and mounts that handler's routes with empty behaviour, as before.
type apiRouterOptions struct {
	Identity  identityhttp.Handler
	Keys      accountshttp.KeyHandler
	Connect   accountshttp.ConnectHandler
	Plans     schedulehttp.Handler
	Crawl     crawlhttp.CrawlHandler
	Quota     quotahttp.QuotaHandler
	Sharing   sharinghttp.Handler
	Downloads mediahttp.DownloadHandler
	Ops       opshttp.Handler
	Stream    platformsse.Handler
	Readiness *httpx.Readiness
	Flags     settingshttp.Handler
	Catalog   cataloghttp.Handler
	Nodes     clusterhttp.Handler
	Home      homehttp.Handler
}

// newAPIRouterSet builds the public and internal routers from one server.
func newAPIRouterSet(opts apiRouterOptions) apiHandlerSet {
	identity := opts.Identity
	server := apiServer{Handler: identity, KeyHandler: opts.Keys, ConnectHandler: opts.Connect, CrawlHandler: opts.Crawl, QuotaHandler: opts.Quota, Sharing: opts.Sharing, Home: opts.Home, Downloads: opts.Downloads, Ops: opts.Ops, Stream: opts.Stream, Readiness: opts.Readiness, Flags: opts.Flags, Catalog: opts.Catalog, Nodes: opts.Nodes, planHandler: planHandler{Handler: opts.Plans}}
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
	internalRouter.Use(clusterhttp.NodeAuthMiddleware(opts.Nodes.Service))
	internalStrict := gen.NewStrictHandlerWithOptions(server, nil, gen.StrictHTTPServerOptions{RequestErrorHandlerFunc: badRequest, ResponseErrorHandlerFunc: internalError})
	internal := gen.HandlerWithOptions(internalStrict, gen.ChiServerOptions{BaseRouter: internalRouter, ErrorHandlerFunc: badRequest})
	return apiHandlerSet{public: public, internal: internal}
}
