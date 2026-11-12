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

// routeAccess is the single place that resolves the auth requirement of every
// route mounted from the OpenAPI contract, keyed by "METHOD pattern". Each area
// declares its own routes in its own file (routes_*.go) so tickets that add an
// endpoint do not edit the same lines. A route missing everywhere is treated as
// admin-only and fails TestEveryRouteDeclaresAccess.
var routeAccess = mergeRouteAccess(identityRouteAccess, crawlRouteAccess, catalogRouteAccess, mediaRouteAccess, adminRouteAccess)

// mergeRouteAccess joins the per-area route tables; a route declared twice is a programming error.
func mergeRouteAccess(areas ...map[string]identityhttp.Access) map[string]identityhttp.Access {
	merged := make(map[string]identityhttp.Access)
	for _, area := range areas {
		for route, access := range area {
			if _, exists := merged[route]; exists {
				panic("roles: route declared in two route tables: " + route)
			}
			merged[route] = access
		}
	}
	return merged
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
