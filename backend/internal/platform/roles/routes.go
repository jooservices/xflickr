package roles

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

// routeAccess is the single place that declares the auth requirement of every
// route mounted from the OpenAPI contract, keyed by "METHOD pattern". A route
// missing here is treated as admin-only and fails TestEveryRouteDeclaresAccess.
var routeAccess = map[string]identityhttp.Access{
	"GET /healthz":        identityhttp.AccessPublic,
	"GET /readyz":         identityhttp.AccessPublic,
	"GET /auth/config":    identityhttp.AccessPublic,
	"POST /auth/register": identityhttp.AccessPublic,
	"POST /auth/login":    identityhttp.AccessPublic,
	"POST /auth/logout":   identityhttp.AccessPublic,

	"DELETE /auth/sessions":          identityhttp.AccessUser,
	"GET /me":                        identityhttp.AccessUser,
	"PATCH /me":                      identityhttp.AccessUser,
	"GET /me/sessions":               identityhttp.AccessUser,
	"DELETE /me/sessions/{id}":       identityhttp.AccessUser,
	"POST /me/password":              identityhttp.AccessUser,
	"PUT /me/sharing":                identityhttp.AccessUser,
	"GET /keys":                      identityhttp.AccessUser,
	"POST /keys":                     identityhttp.AccessUser,
	"GET /keys/{id}":                 identityhttp.AccessUser,
	"PUT /keys/{id}":                 identityhttp.AccessUser,
	"DELETE /keys/{id}":              identityhttp.AccessUser,
	"GET /keys/{id}/quota":           identityhttp.AccessUser,
	"POST /flickr-accounts/connect":  identityhttp.AccessUser,
	"GET /flickr-accounts":           identityhttp.AccessUser,
	"GET /flickr-accounts/callback":  identityhttp.AccessUser,
	"GET /me/crawl":                  identityhttp.AccessUser,
	"GET /me/crawl/queue":            identityhttp.AccessUser,
	"GET /me/crawl/settings":         identityhttp.AccessUser,
	"PUT /me/crawl/settings":         identityhttp.AccessUser,
	"PUT /me/crawl/pause":            identityhttp.AccessUser,
	"PUT /me/crawl/resume":           identityhttp.AccessUser,
	"PUT /me/contacts/{nsid}/star":   identityhttp.AccessUser,
	"PUT /me/contacts/{nsid}/select": identityhttp.AccessUser,
	"POST /downloads":                identityhttp.AccessUser,
	"GET /people":                    identityhttp.AccessUser,
	"GET /people/{nsid}":             identityhttp.AccessUser,
	"GET /people/{nsid}/photos":      identityhttp.AccessUser,
	"GET /favorites":                 identityhttp.AccessUser,
	"GET /albums":                    identityhttp.AccessUser,
	"GET /albums/{id}/photos":        identityhttp.AccessUser,
	"GET /photos/{id}":               identityhttp.AccessUser,

	"GET /downloads/{id}/parts":                       identityhttp.AccessUser,
	"POST /downloads/{id}/parts/{partNumber}/fetched": identityhttp.AccessUser,
	"POST /downloads/{id}/retry-failed":               identityhttp.AccessUser,

	"GET /events": identityhttp.AccessUser,

	"GET /admin/users":           identityhttp.AccessAdmin,
	"PATCH /admin/users/{id}":    identityhttp.AccessAdmin,
	"PUT /admin/users/{id}/plan": identityhttp.AccessAdmin,
	"GET /admin/plans":           identityhttp.AccessAdmin,
	"PUT /admin/plans":           identityhttp.AccessAdmin,
	"GET /admin/flags":           identityhttp.AccessAdmin,
	"PUT /admin/flags":           identityhttp.AccessAdmin,
	"GET /admin/queue":           identityhttp.AccessAdmin,
	"GET /admin/quota":           identityhttp.AccessAdmin,
	"GET /admin/ops/status":      identityhttp.AccessAdmin,
}

// accessResolver classifies a request by the route pattern the router matches
// for it. Requests that match no route stay public (they end in the 404
// handler); matched routes without a declaration require an admin.
func accessResolver(router *chi.Mux) func(*http.Request) identityhttp.Access {
	return func(r *http.Request) identityhttp.Access {
		path := r.URL.RawPath
		if path == "" {
			path = r.URL.Path
		}
		rctx := chi.NewRouteContext()
		if !router.Match(rctx, r.Method, path) {
			return identityhttp.AccessPublic
		}
		if access, ok := routeAccess[r.Method+" "+rctx.RoutePattern()]; ok {
			return access
		}
		return identityhttp.AccessAdmin
	}
}

// writeAPIError answers binding failures with the shared {code, message} body.
func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(gen.Error{Code: code, Message: message})
}

func badRequest(w http.ResponseWriter, _ *http.Request, err error) {
	writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error())
}

func internalError(w http.ResponseWriter, _ *http.Request, _ error) {
	writeAPIError(w, http.StatusInternalServerError, "internal_error", "Internal server error")
}
