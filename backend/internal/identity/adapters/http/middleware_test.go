package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

type fakeSessions struct {
	auth            app.Authenticated
	authErr         error
	authenticated   string
	raw             string
	csrf            string
	session         domain.Session
	issueErr        error
	issuedUser      int64
	issuedUserAgent string
	issuedIP        string
	revoked         []string
	revokeErr       error
	revokedUser     int64
	revokedHash     []byte
	revokeOthersErr error
	listed          []domain.Session
	listErr         error
	revokeByID      int64
	revokeByIDUser  int64
	revokeByIDOK    bool
	revokeByIDErr   error
}

func (f *fakeSessions) Issue(_ context.Context, userID int64, userAgent, ip string) (string, domain.Session, string, error) {
	f.issuedUser = userID
	f.issuedUserAgent = userAgent
	f.issuedIP = ip
	if f.issueErr != nil {
		return "", domain.Session{}, "", f.issueErr
	}
	return f.raw, f.session, f.csrf, nil
}

func (f *fakeSessions) Authenticate(_ context.Context, rawToken string) (app.Authenticated, error) {
	f.authenticated = rawToken
	if f.authErr != nil {
		return app.Authenticated{}, f.authErr
	}
	return f.auth, nil
}

func (f *fakeSessions) Revoke(_ context.Context, rawToken string) error {
	f.revoked = append(f.revoked, rawToken)
	return f.revokeErr
}

func (f *fakeSessions) RevokeOthers(_ context.Context, userID int64, tokenHash []byte) error {
	f.revokedUser = userID
	f.revokedHash = append([]byte(nil), tokenHash...)
	return f.revokeOthersErr
}

func (f *fakeSessions) List(context.Context, int64) ([]domain.Session, error) {
	return f.listed, f.listErr
}

func (f *fakeSessions) RevokeByID(_ context.Context, userID, sessionID int64) (bool, error) {
	f.revokeByIDUser, f.revokeByID = userID, sessionID
	return f.revokeByIDOK, f.revokeByIDErr
}

func TestRequireUserAndRequireAdminMatrix(t *testing.T) {
	user := app.Authenticated{User: domain.User{ID: 5, Role: domain.RoleUser}, Session: domain.Session{ID: 9, ExpiresAt: time.Now().Add(time.Hour)}, TokenHash: []byte{1, 2, 3}}
	admin := user
	admin.User.Role = domain.RoleAdmin
	slid := user
	slid.Slid = true
	slid.Session.ExpiresAt = time.Now().Add(domain.SessionLifetime).UTC()
	for _, tc := range []struct {
		name    string
		admin   bool
		cookie  string
		auth    app.Authenticated
		authErr error
		nilSvc  bool
		want    int
		code    string
		next    bool
	}{
		{name: "user no cookie", want: http.StatusUnauthorized, code: codeUnauthenticated},
		{name: "user empty cookie", cookie: "", want: http.StatusUnauthorized, code: codeUnauthenticated},
		{name: "user expired", cookie: "expired-token", authErr: app.ErrUnauthenticated, want: http.StatusUnauthorized, code: codeUnauthenticated},
		{name: "user database", cookie: "session-token", authErr: io.EOF, want: http.StatusInternalServerError, code: codeInternal},
		{name: "user valid", cookie: "session-token", auth: user, want: http.StatusOK, next: true},
		{name: "user slid", cookie: "session-token", auth: slid, want: http.StatusOK, next: true},
		{name: "admin no cookie", admin: true, want: http.StatusUnauthorized, code: codeUnauthenticated},
		{name: "admin expired", admin: true, cookie: "expired-token", authErr: app.ErrUnauthenticated, want: http.StatusUnauthorized, code: codeUnauthenticated},
		{name: "admin wrong role", admin: true, cookie: "session-token", auth: user, want: http.StatusForbidden, code: codeForbidden},
		{name: "admin allowed", admin: true, cookie: "session-token", auth: admin, want: http.StatusOK, next: true},
		{name: "missing service", nilSvc: true, cookie: "session-token", want: http.StatusInternalServerError, code: codeInternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeSessions{auth: tc.auth, authErr: tc.authErr}
			var sessions SessionService = fake
			if tc.nilSvc {
				sessions = nil
			}
			var guard func(http.Handler) http.Handler
			if tc.admin {
				guard = RequireAdmin(sessions)
			} else {
				guard = RequireUser(sessions)
			}
			called := false
			handler := guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				principal, ok := PrincipalFrom(r.Context())
				if !ok || principal.User.ID != tc.auth.User.ID {
					t.Fatalf("principal = %+v ok=%v", principal, ok)
				}
				id, _ := r.Context().Value(contextKey{}).(int64)
				if id != tc.auth.User.ID {
					t.Fatalf("user id = %d", id)
				}
				w.WriteHeader(http.StatusOK)
			}))
			request := httptest.NewRequest(http.MethodGet, "/me", nil)
			if tc.name != "user no cookie" && tc.name != "admin no cookie" {
				request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: tc.cookie})
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if called != tc.next {
				t.Fatalf("next called = %v", called)
			}
			if tc.code != "" {
				var payload gen.Error
				if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
					t.Fatal(err)
				}
				if payload.Code != tc.code {
					t.Fatalf("code = %s", payload.Code)
				}
			}
			if tc.name == "user slid" {
				cookie := cookieByName(t, response, SessionCookieName)
				if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Value != "session-token" {
					t.Fatalf("slid cookie = %+v", cookie)
				}
			}
		})
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	response := httptest.NewRecorder()
	expires := time.Now().UTC().Add(domain.SessionLifetime)
	SetSessionCookie(response, "opaque-session", expires)
	cookie := cookieByName(t, response, SessionCookieName)
	if cookie.Name != "xf_session" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatalf("session cookie = %+v", cookie)
	}
	if cookie.MaxAge < int((13 * 24 * time.Hour).Seconds()) {
		t.Fatalf("MaxAge = %d", cookie.MaxAge)
	}
	expired := httptest.NewRecorder()
	SetSessionCookie(expired, "opaque-session", time.Now().Add(-time.Hour))
	if cookieByName(t, expired, SessionCookieName).MaxAge != 0 {
		t.Fatal("expired session cookie kept a positive MaxAge")
	}
	cleared := httptest.NewRecorder()
	ClearSessionCookie(cleared)
	ClearCSRFCookie(cleared)
	session := cookieByName(t, cleared, SessionCookieName)
	csrf := cookieByName(t, cleared, CSRFCookieName)
	if session.MaxAge >= 0 || !session.HttpOnly || csrf.MaxAge >= 0 || csrf.HttpOnly {
		t.Fatalf("cleared session=%+v csrf=%+v", session, csrf)
	}
}

func TestCSRFRejectsAndAcceptsUnsafeMethods(t *testing.T) {
	token, err := domain.NewOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	other, err := domain.NewOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	called := false
	handler := CSRF(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, tc := range []struct {
		name   string
		method string
		cookie string
		header string
		want   int
		next   bool
		issue  bool
	}{
		{name: "get issues cookie", method: http.MethodGet, want: http.StatusNoContent, next: true, issue: true},
		{name: "get keeps cookie", method: http.MethodGet, cookie: token, want: http.StatusNoContent, next: true},
		{name: "head is safe", method: http.MethodHead, cookie: token, want: http.StatusNoContent, next: true},
		{name: "trace is safe", method: http.MethodTrace, cookie: token, want: http.StatusNoContent, next: true},
		{name: "post missing", method: http.MethodPost, want: http.StatusForbidden},
		{name: "post mismatch", method: http.MethodPost, cookie: token, header: other, want: http.StatusForbidden},
		{name: "post header only", method: http.MethodPut, header: token, want: http.StatusForbidden},
		{name: "patch match", method: http.MethodPatch, cookie: token, header: token, want: http.StatusNoContent, next: true},
		{name: "delete match", method: http.MethodDelete, cookie: token, header: token, want: http.StatusNoContent, next: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called = false
			request := httptest.NewRequest(tc.method, "/auth/sessions", nil)
			if tc.cookie != "" {
				request.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: tc.cookie})
			}
			if tc.header != "" {
				request.Header.Set(CSRFHeaderName, tc.header)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.want || called != tc.next {
				t.Fatalf("status=%d next=%v body=%s", response.Code, called, response.Body.String())
			}
			if tc.want == http.StatusForbidden {
				var payload gen.Error
				if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
					t.Fatal(err)
				}
				if payload.Code != codeCSRFRejected {
					t.Fatalf("code = %s", payload.Code)
				}
			}
			if tc.issue {
				cookie := cookieByName(t, response, CSRFCookieName)
				if !domain.ValidOpaqueToken(cookie.Value) || cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
					t.Fatalf("csrf cookie = %+v", cookie)
				}
			}
			if tc.name == "get keeps cookie" && len(response.Result().Cookies()) != 0 {
				t.Fatal("safe request replaced an existing CSRF cookie")
			}
		})
	}
}

func TestProtectRoutesLeavesPublicRequestsOpen(t *testing.T) {
	fake := &fakeSessions{authErr: app.ErrUnauthenticated}
	router := ProtectRoutes(fake)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		_, _ = io.WriteString(w, r.URL.Path)
	}))
	open := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	openResponse := httptest.NewRecorder()
	router.ServeHTTP(openResponse, open)
	if openResponse.Code != http.StatusNoContent {
		t.Fatalf("health status = %d", openResponse.Code)
	}
	me := httptest.NewRequest(http.MethodGet, "/me", nil)
	meResponse := httptest.NewRecorder()
	router.ServeHTTP(meResponse, me)
	if meResponse.Code != http.StatusUnauthorized {
		t.Fatalf("me status = %d", meResponse.Code)
	}
	revoke := httptest.NewRequest(http.MethodDelete, "/auth/sessions", nil)
	revoke.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "present"})
	revokeResponse := httptest.NewRecorder()
	router.ServeHTTP(revokeResponse, revoke)
	if revokeResponse.Code != http.StatusUnauthorized {
		t.Fatalf("delete status = %d", revokeResponse.Code)
	}
	for _, target := range []string{"/keys", "/keys/15"} {
		keys := httptest.NewRequest(http.MethodGet, target, nil)
		keysResponse := httptest.NewRecorder()
		router.ServeHTTP(keysResponse, keys)
		if keysResponse.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d", target, keysResponse.Code)
		}
	}
	for _, target := range []string{"/flickr-accounts/connect", "/flickr-accounts/callback"} {
		account := httptest.NewRequest(http.MethodGet, target, nil)
		accountResponse := httptest.NewRecorder()
		router.ServeHTTP(accountResponse, account)
		if accountResponse.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d", target, accountResponse.Code)
		}
	}
	if !authenticatedRequest(http.MethodPost, "/keys") || !authenticatedRequest(http.MethodDelete, "/keys/9") {
		t.Fatal("key mutations were left public")
	}
	if authenticatedRequest(http.MethodHead, "/keys") || authenticatedRequest(http.MethodGet, "/keysomething") {
		t.Fatal("unrelated paths were treated as API keys")
	}
}

func TestLoginAndLogoutMaintainSessionCookies(t *testing.T) {
	params := domain.Argon2Params{Memory: 32 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	hash, err := domain.HashPassword("secret12", params)
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().UTC().Add(domain.SessionLifetime)
	csrf, err := domain.NewOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeSessions{raw: "opaque-session", csrf: csrf, session: domain.Session{ExpiresAt: expires}}
	handler := Handler{
		LoginUseCase: app.Login{Repository: repo{user: domain.User{ID: 4, Email: "u@example.com", PasswordHash: hash, Role: domain.RoleUser, CreatedAt: time.Now().UTC()}}, Limiter: rateLimit{allowed: true}, DummyPasswordHash: hash},
		Sessions:     fake,
	}
	server := gen.NewStrictHandler(StrictHandler{Handler: handler}, []gen.StrictMiddlewareFunc{MaintainSessionCookies(handler.Sessions)})
	login := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"email":"u@example.com","password":"secret12"}`))
	login.Header.Set("Content-Type", "application/json")
	login.Header.Set("User-Agent", "Mozilla/5.0 Firefox/122 Linux")
	login = login.WithContext(WithRemoteAddress(login.Context(), "127.0.0.1:9"))
	loginResponse := httptest.NewRecorder()
	server.Login(loginResponse, login)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d body = %s", loginResponse.Code, loginResponse.Body.String())
	}
	if fake.issuedUser != 4 {
		t.Fatalf("issued user = %d", fake.issuedUser)
	}
	if fake.issuedUserAgent != "Mozilla/5.0 Firefox/122 Linux" || fake.issuedIP != "127.0.0.1" {
		t.Fatalf("issued client metadata = %q %q", fake.issuedUserAgent, fake.issuedIP)
	}
	session := cookieByName(t, loginResponse, SessionCookieName)
	if session.Value != "opaque-session" || !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteLaxMode {
		t.Fatalf("login session cookie = %+v", session)
	}
	issuedCSRF := cookieByName(t, loginResponse, CSRFCookieName)
	if issuedCSRF.Value != csrf || issuedCSRF.HttpOnly || !issuedCSRF.Secure {
		t.Fatalf("login csrf cookie = %+v", issuedCSRF)
	}

	denied := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"email":"u@example.com","password":"wrongpass"}`))
	denied.Header.Set("Content-Type", "application/json")
	denied = denied.WithContext(WithRemoteAddress(denied.Context(), "127.0.0.1:9"))
	deniedResponse := httptest.NewRecorder()
	server.Login(deniedResponse, denied)
	if deniedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("denied status = %d", deniedResponse.Code)
	}
	if len(deniedResponse.Result().Cookies()) != 0 {
		t.Fatal("failed login set a session cookie")
	}

	logout := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	logout.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "opaque-session"})
	logoutResponse := httptest.NewRecorder()
	server.Logout(logoutResponse, logout)
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d", logoutResponse.Code)
	}
	if len(fake.revoked) != 1 || fake.revoked[0] != "opaque-session" {
		t.Fatalf("revoked = %#v", fake.revoked)
	}
	if cookieByName(t, logoutResponse, SessionCookieName).MaxAge >= 0 {
		t.Fatal("logout left the session cookie")
	}
}

func TestMaintainSessionCookiesReportsFailures(t *testing.T) {
	params := domain.Argon2Params{Memory: 32 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	hash, err := domain.HashPassword("secret12", params)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeSessions{issueErr: io.EOF}
	handler := Handler{
		LoginUseCase: app.Login{Repository: repo{user: domain.User{ID: 4, PasswordHash: hash, CreatedAt: time.Now().UTC()}}, Limiter: rateLimit{allowed: true}, DummyPasswordHash: hash},
		Sessions:     fake,
	}
	server := gen.NewStrictHandler(StrictHandler{Handler: handler}, []gen.StrictMiddlewareFunc{MaintainSessionCookies(handler.Sessions)})
	loginRequest := func() *http.Request {
		request := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"email":"u@example.com","password":"secret12"}`))
		request.Header.Set("Content-Type", "application/json")
		return request.WithContext(WithRemoteAddress(request.Context(), "127.0.0.1:9"))
	}
	response := httptest.NewRecorder()
	server.Login(response, loginRequest())
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}

	logoutHandler := Handler{Sessions: &fakeSessions{revokeErr: io.EOF}}
	logoutServer := gen.NewStrictHandler(StrictHandler{Handler: logoutHandler}, []gen.StrictMiddlewareFunc{MaintainSessionCookies(logoutHandler.Sessions)})
	logout := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	logout.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "opaque-session"})
	logoutResponse := httptest.NewRecorder()
	logoutServer.Logout(logoutResponse, logout)
	if logoutResponse.Code != http.StatusInternalServerError {
		t.Fatalf("logout status = %d", logoutResponse.Code)
	}

	bare := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	bareResponse := httptest.NewRecorder()
	logoutServer = gen.NewStrictHandler(StrictHandler{}, []gen.StrictMiddlewareFunc{MaintainSessionCookies(&fakeSessions{})})
	logoutServer.Logout(bareResponse, bare)
	if bareResponse.Code != http.StatusNoContent {
		t.Fatalf("bare logout status = %d", bareResponse.Code)
	}

	missingService := gen.NewStrictHandler(StrictHandler{Handler: handler}, []gen.StrictMiddlewareFunc{MaintainSessionCookies(nil)})
	missingResponse := httptest.NewRecorder()
	missingService.Login(missingResponse, loginRequest())
	if missingResponse.Code != http.StatusInternalServerError {
		t.Fatalf("missing service status = %d", missingResponse.Code)
	}
	missingLogout := httptest.NewRecorder()
	missingService.Logout(missingLogout, bare)
	if missingLogout.Code != http.StatusInternalServerError {
		t.Fatalf("missing service logout status = %d", missingLogout.Code)
	}
}

func TestRevokeOtherSessionsResponses(t *testing.T) {
	handler := Handler{}
	got, err := handler.RevokeOtherSessions(context.Background(), gen.RevokeOtherSessionsRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.(gen.RevokeOtherSessions401Response); !ok {
		t.Fatalf("response = %T", got)
	}
	fake := &fakeSessions{}
	handler.Sessions = fake
	ctx := WithPrincipal(context.Background(), Principal{User: domain.User{ID: 3}, TokenHash: []byte{7, 7}})
	got, err = handler.RevokeOtherSessions(ctx, gen.RevokeOtherSessionsRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.(gen.RevokeOtherSessions204Response); !ok {
		t.Fatalf("response = %T", got)
	}
	if fake.revokedUser != 3 || len(fake.revokedHash) != 2 {
		t.Fatalf("revoked user=%d hash=%v", fake.revokedUser, fake.revokedHash)
	}
	fake.revokeOthersErr = app.ErrUnauthenticated
	got, err = handler.RevokeOtherSessions(ctx, gen.RevokeOtherSessionsRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.(gen.RevokeOtherSessions401Response); !ok {
		t.Fatalf("response = %T", got)
	}
	fake.revokeOthersErr = io.EOF
	if _, err = handler.RevokeOtherSessions(ctx, gen.RevokeOtherSessionsRequestObject{}); err == nil {
		t.Fatal("expected revoke error")
	}
	handler.Sessions = nil
	if _, err = handler.RevokeOtherSessions(ctx, gen.RevokeOtherSessionsRequestObject{}); err == nil {
		t.Fatal("expected missing service error")
	}
}

func TestCSRFOptionsIsSafe(t *testing.T) {
	handler := CSRF(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodOptions, "/auth/login", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d", response.Code)
	}
	if !domain.ValidOpaqueToken(cookieByName(t, response, CSRFCookieName).Value) {
		t.Fatal("OPTIONS did not issue a CSRF cookie")
	}
}

func cookieByName(t *testing.T, response *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("cookie %s missing in %q", name, response.Header().Get("Set-Cookie"))
	return nil
}

func TestLoginResponseWithoutUserDoesNotIssueASession(t *testing.T) {
	called := false
	middleware := MaintainSessionCookies(&fakeSessions{raw: "raw"})
	wrapped := middleware(func(_ context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
		called = true
		return gen.Login401Response{}, nil
	}, loginOperationID)
	response := httptest.NewRecorder()
	got, err := wrapped(context.Background(), response, httptest.NewRequest(http.MethodPost, "/auth/login", nil), nil)
	if err != nil || !called {
		t.Fatalf("response=%v err=%v called=%v", got, err, called)
	}
	if response.Header().Get("Set-Cookie") != "" {
		t.Fatal("rejected login set a cookie")
	}
}
