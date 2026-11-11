package roles

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	identityapp "github.com/jooservices/xflickr/internal/identity/app"
	identitydomain "github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/api/rest"
)

func flagReadAuthentication(sessions identityhttp.SessionService) gen.StrictMiddlewareFunc {
	return func(next gen.StrictHandlerFunc, operation string) gen.StrictHandlerFunc {
		if operation != "GetAdminFlag" {
			return next
		}
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
			traceID := discoveryTraceID(ctx)
			cookie, err := r.Cookie(identityhttp.SessionCookieName)
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
			if result.User.Role != identitydomain.RoleAdmin {
				return nil, rest.WriteProblem(w, r, rest.NewProblem("forbidden", "Administrator role required", nil), traceID)
			}
			query, err := url.ParseQuery(r.URL.RawQuery)
			if err != nil {
				return nil, rest.WriteProblem(w, r, rest.NewProblem("malformed-request", "Malformed query string", nil), traceID)
			}
			if len(query) > 0 {
				return nil, rest.WriteProblem(w, r, rest.NewProblem("validation-error", "No query parameters are supported", nil), traceID)
			}
			principal := identityhttp.Principal{User: result.User, Session: result.Session, TokenHash: result.TokenHash}
			return next(identityhttp.WithPrincipal(ctx, principal), w, r, request)
		}
	}
}
