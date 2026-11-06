package roles

import (
	"context"
	"errors"
	"net/http"

	"go.opentelemetry.io/otel/trace"

	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	identityapp "github.com/jooservices/xflickr/internal/identity/app"
	identitydomain "github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/api/rest"
)

// discoveryAuthentication is restricted to the additive operation. Old routes
// keep their authentication and error representations unchanged.
func discoveryAuthentication(sessions identityhttp.SessionService) gen.StrictMiddlewareFunc {
	return func(next gen.StrictHandlerFunc, operation string) gen.StrictHandlerFunc {
		if operation != "GetAPIEntry" {
			return next
		}
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
			traceID := discoveryTraceID(ctx)
			if len(r.URL.Query()) > 0 {
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
	collections map[string]string
	traceID     string
}

func (r discoveryResponse) VisitGetAPIEntryResponse(w http.ResponseWriter) error {
	w.Header().Set("Cache-Control", "private, no-store")
	return rest.WriteSuccess(w, 200, "Success", rest.Discovery(r.collections), nil, r.traceID)
}

func (apiServer) GetAPIEntry(ctx context.Context, _ gen.GetAPIEntryRequestObject) (gen.GetAPIEntryResponseObject, error) {
	collections := map[string]string{"authentication-options": "/auth/config"}
	if principal, ok := identityhttp.PrincipalFrom(ctx); ok {
		for relation, path := range map[string]string{"me": "/me", "events": "/events", "keys": "/keys", "flickr-accounts": "/flickr-accounts", "people": "/people", "albums": "/albums", "favorites": "/favorites"} {
			collections[relation] = path
		}
		if principal.User.Role == identitydomain.RoleAdmin {
			for relation, path := range map[string]string{"users": "/admin/users", "plans": "/admin/plans", "flags": "/admin/flags"} {
				collections[relation] = path
			}
		}
	}
	return discoveryResponse{collections, discoveryTraceID(ctx)}, nil
}
