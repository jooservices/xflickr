package roles

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/trace"

	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/api/rest"
)

// routeAccess is the single place that declares the auth requirement of every
// route mounted from the OpenAPI contract, keyed by "METHOD pattern". A route
// missing here is treated as admin-only and fails TestEveryRouteDeclaresAccess.
var routeAccess = map[string]identityhttp.Access{
	"GET /api/v1":                        identityhttp.AccessPublic,
	"GET /healthz":                       identityhttp.AccessPublic,
	"GET /readyz":                        identityhttp.AccessPublic,
	"GET /api/v1/authentication-options": identityhttp.AccessPublic,
	"POST /api/v1/users":                 identityhttp.AccessPublic,
	"GET /api/v1/users/{id}":             identityhttp.AccessUser,
	"POST /api/v1/sessions":              identityhttp.AccessPublic,
	"DELETE /api/v1/sessions/current":    identityhttp.AccessPublic,
	"GET /api/v1/sessions/current":       identityhttp.AccessUser,
	"GET /api/v1/sessions/{id}":          identityhttp.AccessUser,
	"GET /api/v1/me":                     identityhttp.AccessUser,
	"PATCH /api/v1/me":                   identityhttp.AccessUser,
	"GET /api/v1/me/sessions":            identityhttp.AccessUser,
	"DELETE /api/v1/me/sessions":         identityhttp.AccessUser,
	"GET /api/v1/me/sessions/{id}":       identityhttp.AccessUser,
	"DELETE /api/v1/me/sessions/{id}":    identityhttp.AccessUser,
	"PUT /api/v1/me/password":            identityhttp.AccessUser,
	"GET /api/v1/me/sharing":             identityhttp.AccessUser,
	"PUT /api/v1/me/sharing":             identityhttp.AccessUser,
	"GET /keys":                          identityhttp.AccessUser,
	"POST /keys":                         identityhttp.AccessUser,
	"GET /keys/{id}":                     identityhttp.AccessUser,
	"PUT /keys/{id}":                     identityhttp.AccessUser,
	"DELETE /keys/{id}":                  identityhttp.AccessUser,
	"GET /keys/{id}/quota":               identityhttp.AccessUser,
	"POST /flickr-accounts/connect":      identityhttp.AccessUser,
	"GET /flickr-accounts":               identityhttp.AccessUser,
	"GET /flickr-accounts/callback":      identityhttp.AccessUser,
	"GET /me/crawl":                      identityhttp.AccessUser,
	"GET /me/crawl/queue":                identityhttp.AccessUser,
	"GET /me/crawl/settings":             identityhttp.AccessUser,
	"PUT /me/crawl/settings":             identityhttp.AccessUser,
	"PUT /me/crawl/pause":                identityhttp.AccessUser,
	"PUT /me/crawl/resume":               identityhttp.AccessUser,
	"PUT /me/contacts/{nsid}/star":       identityhttp.AccessUser,
	"PUT /me/contacts/{nsid}/select":     identityhttp.AccessUser,
	"POST /downloads":                    identityhttp.AccessUser,
	"GET /people":                        identityhttp.AccessUser,
	"GET /people/{nsid}":                 identityhttp.AccessUser,
	"GET /people/{nsid}/photos":          identityhttp.AccessUser,
	"GET /favorites":                     identityhttp.AccessUser,
	"GET /me/albums":                     identityhttp.AccessUser,
	"GET /albums":                        identityhttp.AccessUser,
	"GET /albums/{id}/photos":            identityhttp.AccessUser,
	"GET /photos/{id}":                   identityhttp.AccessUser,

	"GET /downloads/{id}/parts":                       identityhttp.AccessUser,
	"POST /downloads/{id}/parts/{partNumber}/fetched": identityhttp.AccessUser,
	"POST /downloads/{id}/retry-failed":               identityhttp.AccessUser,

	"GET /events": identityhttp.AccessUser,

	"GET /admin/users":               identityhttp.AccessAdmin,
	"PATCH /admin/users/{id}":        identityhttp.AccessAdmin,
	"PUT /admin/users/{id}/plan":     identityhttp.AccessAdmin,
	"GET /admin/plans":               identityhttp.AccessAdmin,
	"PUT /admin/plans":               identityhttp.AccessAdmin,
	"GET /admin/flags":               identityhttp.AccessAdmin,
	"PUT /admin/flags":               identityhttp.AccessAdmin,
	"GET /api/v1/admin/flags/{name}": identityhttp.AccessPublic,
	"GET /admin/queue":               identityhttp.AccessAdmin,
	"GET /admin/quota":               identityhttp.AccessAdmin,
	"GET /admin/calls/stats":         identityhttp.AccessAdmin,
	"GET /admin/metrics":             identityhttp.AccessAdmin,
	"GET /admin/ops/status":          identityhttp.AccessAdmin,
	"POST /admin/nodes":              identityhttp.AccessAdmin,

	"POST /internal/nodes/heartbeat": identityhttp.AccessPublic,
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

// writeAPIError answers binding failures with the shared problem format.
func writeAPIError(w http.ResponseWriter, r *http.Request, slug, message string) {
	path, traceID := "", ""
	if r != nil {
		path = r.URL.Path
		span := trace.SpanContextFromContext(r.Context())
		if span.IsValid() {
			traceID = span.TraceID().String()
		}
	}
	_ = rest.WriteProblemAt(w, path, rest.NewProblem(slug, message, nil), traceID)
}

func badRequest(w http.ResponseWriter, r *http.Request, _ error) {
	if r.URL.Path == "/admin/metrics" {
		writeMetricsQueryError(w, http.StatusBadRequest, "invalid_request", "Invalid metrics query")
		return
	}
	writeAPIError(w, r, "malformed-request", "Malformed request")
}

func writeMetricsQueryError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(gen.Error{Code: code, Message: message})
}

func metricsQueryMultiplicity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err == nil {
			for _, name := range []string{"series", "queue", "role", "node", "from", "to", "step"} {
				if len(query[name]) > 1 {
					writeMetricsQueryError(w, http.StatusBadRequest, "invalid_request", "Invalid metrics query")
					return
				}
			}
		} else {
			writeMetricsQueryError(w, http.StatusBadRequest, "invalid_request", "Invalid metrics query")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func internalError(w http.ResponseWriter, r *http.Request, _ error) {
	if r != nil && r.URL.Path == "/admin/metrics" {
		writeMetricsQueryError(w, http.StatusInternalServerError, "internal_error", "Internal server error")
		return
	}
	writeAPIError(w, r, "internal-error", "Internal server error")
}
