package roles

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"go.opentelemetry.io/otel/trace"

	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	identityapp "github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/api/rest"
)

// discoveryAuthentication authenticates the public API entry point when a
// browser session cookie is present.
func discoveryAuthentication(sessions identityhttp.SessionService) gen.StrictMiddlewareFunc {
	return func(next gen.StrictHandlerFunc, operation string) gen.StrictHandlerFunc {
		if operation != "GetAPIEntry" {
			return next
		}
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
			traceID := discoveryTraceID(ctx)
			query, err := url.ParseQuery(r.URL.RawQuery)
			if err != nil {
				return nil, rest.WriteProblem(w, r, rest.NewProblem("malformed-request", "Malformed query string", nil), traceID)
			}
			if len(query) > 0 {
				return nil, rest.WriteProblem(w, r, rest.NewProblem("validation-error", "No query parameters are supported", nil), traceID)
			}
			cookie, err := r.Cookie(identityhttp.SessionCookieName)
			if errors.Is(err, http.ErrNoCookie) {
				return next(ctx, w, r, request)
			}
			if err != nil || cookie.Value == "" {
				return nil, rest.WriteProblem(w, r, rest.NewProblem("unauthenticated", "Authentication required", nil), traceID)
			}
			if sessions == nil {
				return nil, rest.WriteProblem(w, r, rest.NewProblem("internal-error", "Authentication unavailable", nil), traceID)
			}
			result, err := sessions.Authenticate(ctx, cookie.Value)
			if errors.Is(err, identityapp.ErrUnauthenticated) {
				return nil, rest.WriteProblem(w, r, rest.NewProblem("unauthenticated", "Authentication required", nil), traceID)
			}
			if err != nil {
				return nil, rest.WriteProblem(w, r, rest.NewProblem("internal-error", "Authentication unavailable", nil), traceID)
			}
			if result.Slid {
				identityhttp.SetSessionCookie(w, cookie.Value, result.Session.ExpiresAt)
			}
			return next(identityhttp.WithPrincipal(ctx, identityhttp.Principal{User: result.User, Session: result.Session, TokenHash: result.TokenHash}), w, r, request)
		}
	}
}

func discoveryTraceID(ctx context.Context) string {
	span := trace.SpanContextFromContext(ctx)
	if span.IsValid() {
		return span.TraceID().String()
	}
	return ""
}

type discoveryResponse struct {
	collections map[string]rest.Link
	traceID     string
}

func (r discoveryResponse) VisitGetAPIEntryResponse(w http.ResponseWriter) error {
	w.Header().Set("Cache-Control", "private, no-store")
	return rest.WriteSuccess(w, 200, "Success", rest.Discovery(r.collections), nil, r.traceID)
}

func (apiServer) GetAPIEntry(ctx context.Context, _ gen.GetAPIEntryRequestObject) (gen.GetAPIEntryResponseObject, error) {
	collections := map[string]rest.Link{
		"authentication-options": rest.NewLink("/api/v1/authentication-options", http.MethodGet),
		"users":                  rest.NewLink("/api/v1/users", http.MethodPost),
		"sessions":               rest.NewLink("/api/v1/sessions", http.MethodPost),
	}
	if _, ok := identityhttp.PrincipalFrom(ctx); ok {
		collections["me"] = rest.NewLink("/api/v1/me", http.MethodGet)
		collections["current-session"] = rest.NewLink("/api/v1/sessions/current", http.MethodGet)
		collections["home"] = rest.NewLink("/api/v1/me/home", http.MethodGet)
	}
	return discoveryResponse{collections, discoveryTraceID(ctx)}, nil
}
