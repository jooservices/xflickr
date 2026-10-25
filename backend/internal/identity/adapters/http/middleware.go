package http

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

const (
	// SessionCookieName is the authenticated session cookie.
	SessionCookieName = "xf_session"
	// CSRFCookieName is the readable half of the double-submit CSRF token.
	CSRFCookieName = "xf_csrf"
	// CSRFHeaderName carries the same token as CSRFCookieName on unsafe methods.
	CSRFHeaderName = "X-CSRF-Token"

	loginOperationID  = "Login"
	logoutOperationID = "Logout"

	codeUnauthenticated = "unauthenticated"
	codeForbidden       = "forbidden"
	codeCSRFRejected    = "csrf_rejected"
	codeInternal        = "internal_error"
)

// Principal is the authenticated user attached to a request.
type Principal struct {
	User      domain.User
	Session   domain.Session
	TokenHash []byte
}

type principalKey struct{}

// SessionService issues, checks, and revokes server-side sessions.
type SessionService interface {
	Issue(context.Context, int64, string, string) (string, domain.Session, string, error)
	Authenticate(context.Context, string) (app.Authenticated, error)
	Revoke(context.Context, string) error
	RevokeOthers(context.Context, int64, []byte) error
	List(context.Context, int64) ([]domain.Session, error)
	RevokeByID(context.Context, int64, int64) (bool, error)
}

// WithPrincipal stores the authenticated principal on ctx.
func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}

// PrincipalFrom returns the principal stored by RequireUser or RequireAdmin.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalKey{}).(Principal)
	return principal, ok
}

// SetSessionCookie writes the xf_session cookie for the sliding expiry.
func SetSessionCookie(w http.ResponseWriter, raw string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // #nosec G124 -- Session cookie attributes are set explicitly below.
		Name:     SessionCookieName,
		Value:    raw,
		Path:     "/",
		Expires:  expires,
		MaxAge:   cookieMaxAge(expires),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// SetCSRFCookie writes the readable CSRF cookie used by the double-submit check.
func SetCSRFCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // #nosec G124 -- CSRF token is intentionally readable; Secure and SameSite are set below.
		Name:     CSRFCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   cookieMaxAge(expires),
		HttpOnly: false,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie removes the session cookie.
func ClearSessionCookie(w http.ResponseWriter) {
	clearCookie(w, SessionCookieName, true)
}

// ClearCSRFCookie removes the CSRF cookie.
func ClearCSRFCookie(w http.ResponseWriter) {
	clearCookie(w, CSRFCookieName, false)
}

func clearCookie(w http.ResponseWriter, name string, httpOnly bool) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // #nosec G124 -- Cookie attributes are set explicitly below, with HttpOnly chosen by cookie type.
		Name:     name,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: httpOnly,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func cookieMaxAge(expires time.Time) int {
	seconds := int(time.Until(expires).Seconds())
	if seconds < 0 {
		return 0
	}
	return seconds
}

// CSRF enforces a double-submit token on unsafe methods. Safe methods issue
// the readable cookie when the browser does not already have one.
func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !unsafeMethod(r.Method) {
			if _, ok := csrfCookie(r); !ok {
				token, err := domain.NewOpaqueToken()
				if err != nil {
					writeJSONError(w, http.StatusInternalServerError, codeInternal, "Internal server error")
					return
				}
				SetCSRFCookie(w, token, time.Now().UTC().Add(domain.SessionLifetime))
			}
			next.ServeHTTP(w, r)
			return
		}
		cookie, ok := csrfCookie(r)
		if !ok || !csrfTokensMatch(cookie, r.Header.Get(CSRFHeaderName)) {
			writeJSONError(w, http.StatusForbidden, codeCSRFRejected, "CSRF token is missing or invalid")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func unsafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}

func csrfCookie(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(CSRFCookieName)
	if err != nil || !domain.ValidOpaqueToken(cookie.Value) {
		return "", false
	}
	return cookie.Value, true
}

func csrfTokensMatch(cookie, header string) bool {
	if len(cookie) != len(header) || !domain.ValidOpaqueToken(header) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie), []byte(header)) == 1
}

// RequireUser allows the request only when xf_session resolves to a user.
// A missing or expired session responds with 401.
func RequireUser(sessions SessionService) func(http.Handler) http.Handler {
	return requirePrincipal(sessions, false)
}

// RequireAdmin allows the request only for an administrator.
// A missing or expired session responds with 401. A signed-in user with another role responds with 403.
func RequireAdmin(sessions SessionService) func(http.Handler) http.Handler {
	return requirePrincipal(sessions, true)
}

func requirePrincipal(sessions SessionService, admin bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if sessions == nil {
				writeJSONError(w, http.StatusInternalServerError, codeInternal, "Internal server error")
				return
			}
			cookie, err := r.Cookie(SessionCookieName)
			if err != nil || cookie.Value == "" {
				writeJSONError(w, http.StatusUnauthorized, codeUnauthenticated, "Authentication required")
				return
			}
			result, err := sessions.Authenticate(r.Context(), cookie.Value)
			if errors.Is(err, app.ErrUnauthenticated) {
				writeJSONError(w, http.StatusUnauthorized, codeUnauthenticated, "Authentication required")
				return
			}
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, codeInternal, "Internal server error")
				return
			}
			if admin && result.User.Role != domain.RoleAdmin {
				writeJSONError(w, http.StatusForbidden, codeForbidden, "Administrator role required")
				return
			}
			if result.Slid {
				SetSessionCookie(w, cookie.Value, result.Session.ExpiresAt)
			}
			ctx := WithUserID(r.Context(), result.User.ID)
			ctx = WithPrincipal(ctx, Principal{User: result.User, Session: result.Session, TokenHash: result.TokenHash})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

var authenticatedRoutes = map[string]struct{}{
	http.MethodGet + " /me":               {},
	http.MethodGet + " /me/crawl/queue":   {},
	http.MethodPut + " /me/sharing":       {},
	http.MethodPatch + " /me":             {},
	http.MethodDelete + " /auth/sessions": {},
	http.MethodGet + " /me/sessions":      {},
	http.MethodDelete + " /me/sessions/":  {},
	http.MethodPost + " /me/password":     {},
}

func authenticatedRequest(method, path string) bool {
	if _, ok := authenticatedRoutes[method+" "+path]; ok {
		return true
	}
	if method == http.MethodDelete && strings.HasPrefix(path, "/me/sessions/") {
		return true
	}
	if path != "/keys" && !strings.HasPrefix(path, "/keys/") && !strings.HasPrefix(path, "/flickr-accounts/") {
		return false
	}
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

func adminRequest(path string) bool {
	return path == "/admin" || strings.HasPrefix(path, "/admin/")
}

// oauthCallbackNavigation reports the browser landing on the Flickr OAuth callback.
func oauthCallbackNavigation(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/flickr-accounts/callback" &&
		strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html")
}

// redirectUnauthenticated sends a browser without a valid session back to the
// SPA with a readable error instead of a JSON 401 page.
func redirectUnauthenticated(requireUser func(http.Handler) http.Handler, next http.Handler) http.Handler {
	guarded := requireUser(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		guarded.ServeHTTP(&unauthorizedRedirect{ResponseWriter: w}, r)
	})
}

type unauthorizedRedirect struct {
	http.ResponseWriter
	redirected bool
}

func (w *unauthorizedRedirect) WriteHeader(status int) {
	if status == http.StatusUnauthorized {
		w.redirected = true
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Location", "/keys?connect_error=unauthenticated")
		status = http.StatusFound
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *unauthorizedRedirect) Write(b []byte) (int, error) {
	if w.redirected {
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}

// ProtectRoutes applies RequireUser to routes that need an authenticated
// principal and RequireAdmin to everything under /admin.
func ProtectRoutes(sessions SessionService) func(http.Handler) http.Handler {
	requireUser := RequireUser(sessions)
	requireAdmin := RequireAdmin(sessions)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if adminRequest(r.URL.Path) {
				requireAdmin(next).ServeHTTP(w, r)
				return
			}
			if authenticatedRequest(r.Method, r.URL.Path) {
				if oauthCallbackNavigation(r) {
					redirectUnauthenticated(requireUser, next).ServeHTTP(w, r)
					return
				}
				requireUser(next).ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// MaintainSessionCookies issues a session after login and revokes it on logout.
func MaintainSessionCookies(sessions SessionService) gen.StrictMiddlewareFunc {
	return func(next gen.StrictHandlerFunc, operationID string) gen.StrictHandlerFunc {
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
			if operationID == logoutOperationID {
				if err := revokePresentedSession(ctx, w, r, sessions); err != nil {
					return nil, err
				}
			}
			response, err := next(ctx, w, r, request)
			if err != nil || operationID != loginOperationID {
				return response, err
			}
			user, ok := response.(gen.Login200JSONResponse)
			if !ok {
				return response, nil
			}
			if sessions == nil {
				return nil, errors.New("identity: session service is required")
			}
			raw, session, csrf, err := sessions.Issue(ctx, user.ID, r.UserAgent(), clientIP(ctx))
			if err != nil {
				return nil, err
			}
			SetSessionCookie(w, raw, session.ExpiresAt)
			SetCSRFCookie(w, csrf, session.ExpiresAt)
			return response, nil
		}
	}
}

func revokePresentedSession(ctx context.Context, w http.ResponseWriter, r *http.Request, sessions SessionService) error {
	ClearSessionCookie(w)
	ClearCSRFCookie(w)
	if sessions == nil {
		return errors.New("identity: session service is required")
	}
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}
	return sessions.Revoke(ctx, cookie.Value)
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(gen.Error{Code: code, Message: message})
}
