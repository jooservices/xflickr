package roles

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	accountshttp "github.com/jooservices/xflickr/internal/accounts/adapters/http"
	crawlhttp "github.com/jooservices/xflickr/internal/crawl/adapters/http"
	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	identityapp "github.com/jooservices/xflickr/internal/identity/app"
	identitydomain "github.com/jooservices/xflickr/internal/identity/domain"
	mediahttp "github.com/jooservices/xflickr/internal/media/adapters/http"
	quotahttp "github.com/jooservices/xflickr/internal/quota/adapters/http"
	schedulehttp "github.com/jooservices/xflickr/internal/schedule/adapters/http"
	settingshttp "github.com/jooservices/xflickr/internal/settings/adapters/http"
	settingsapp "github.com/jooservices/xflickr/internal/settings/app"
	settingsdomain "github.com/jooservices/xflickr/internal/settings/domain"
	sharinghttp "github.com/jooservices/xflickr/internal/sharing/adapters/http"
)

type flagReadSessions struct {
	stubSessions
	role  identitydomain.Role
	err   error
	slid  bool
	calls int
}

func (sessions *flagReadSessions) Authenticate(_ context.Context, token string) (identityapp.Authenticated, error) {
	sessions.calls++
	if sessions.err != nil {
		return identityapp.Authenticated{}, sessions.err
	}
	if token != "valid" {
		return identityapp.Authenticated{}, identityapp.ErrUnauthenticated
	}
	result := identityapp.Authenticated{User: identitydomain.User{ID: 5, Role: sessions.role}}
	if sessions.slid {
		result.Slid = true
		result.Session.ExpiresAt = time.Now().Add(time.Hour)
	}
	return result, nil
}

func TestFlagReadAuthentication(t *testing.T) {
	for _, test := range []struct {
		name       string
		sessions   identityhttp.SessionService
		cookie     string
		query      string
		operation  string
		wantStatus int
		wantType   string
		wantNext   int
		wantAuth   int
		wantSlide  bool
	}{
		{name: "missing session", sessions: &flagReadSessions{role: identitydomain.RoleAdmin}, wantStatus: 401, wantType: "/problems/unauthenticated"},
		{name: "unknown session", sessions: &flagReadSessions{err: identityapp.ErrUnauthenticated}, cookie: "valid", wantStatus: 401, wantType: "/problems/unauthenticated", wantAuth: 1},
		{name: "authentication failure", sessions: &flagReadSessions{err: errors.New("private auth failure")}, cookie: "valid", wantStatus: 500, wantType: "/problems/internal-error", wantAuth: 1},
		{name: "missing service", cookie: "valid", wantStatus: 500, wantType: "/problems/internal-error"},
		{name: "plain user", sessions: &flagReadSessions{role: identitydomain.RoleUser}, cookie: "valid", wantStatus: 403, wantType: "/problems/forbidden", wantAuth: 1},
		{name: "malformed query", sessions: &flagReadSessions{role: identitydomain.RoleAdmin}, cookie: "valid", query: "bad=%GG", wantStatus: 400, wantType: "/problems/malformed-request", wantAuth: 1},
		{name: "unsupported query", sessions: &flagReadSessions{role: identitydomain.RoleAdmin}, cookie: "valid", query: "unknown=1", wantStatus: 422, wantType: "/problems/validation-error", wantAuth: 1},
		{name: "administrator", sessions: &flagReadSessions{role: identitydomain.RoleAdmin, slid: true}, cookie: "valid", wantStatus: 0, wantNext: 1, wantAuth: 1, wantSlide: true},
		{name: "other operation", sessions: &flagReadSessions{}, operation: "GetAdminFlags", wantStatus: 0, wantNext: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			next := func(ctx context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
				calls++
				if test.name == "administrator" {
					principal, ok := identityhttp.PrincipalFrom(ctx)
					if !ok || principal.User.Role != identitydomain.RoleAdmin {
						t.Fatal("administrator principal was not passed to the handler")
					}
				}
				return "ok", nil
			}
			operation := test.operation
			if operation == "" {
				operation = "GetAdminFlag"
			}
			r := httptest.NewRequest(http.MethodGet, "/api/v1/admin/flags/demo?"+test.query, nil)
			if test.cookie != "" {
				r.AddCookie(&http.Cookie{Name: identityhttp.SessionCookieName, Value: test.cookie})
			}
			w := httptest.NewRecorder()
			response, err := flagReadAuthentication(test.sessions)(next, operation)(r.Context(), w, r, nil)
			if err != nil {
				t.Fatal(err)
			}
			if calls != test.wantNext || (test.wantNext == 1 && response != "ok") {
				t.Fatalf("next calls/response = %d/%v; want %d/ok", calls, response, test.wantNext)
			}
			if test.wantStatus == 0 {
				if w.Code != http.StatusOK || (test.wantSlide && len(w.Result().Cookies()) != 1) {
					t.Fatalf("success status/cookies = %d/%v", w.Code, w.Result().Cookies())
				}
			} else {
				assertFlagReadProblem(t, w, test.wantStatus, test.wantType)
				if test.wantStatus == 401 && w.Header().Get("WWW-Authenticate") == "" {
					t.Fatal("401 response omitted WWW-Authenticate")
				}
			}
			if sessions, ok := test.sessions.(*flagReadSessions); ok && sessions.calls != test.wantAuth {
				t.Fatalf("Authenticate calls = %d, want %d", sessions.calls, test.wantAuth)
			}
		})
	}
}

type routeFlagService struct {
	flag     settingsdomain.FeatureFlag
	getCalls int
}

func (service *routeFlagService) List(context.Context) ([]settingsdomain.FeatureFlag, error) {
	return []settingsdomain.FeatureFlag{service.flag}, nil
}

func (service *routeFlagService) Get(_ context.Context, name string) (settingsdomain.FeatureFlag, error) {
	service.getCalls++
	if name != service.flag.Name {
		return settingsdomain.FeatureFlag{}, settingsapp.ErrFeatureFlagNotFound
	}
	return service.flag, nil
}

func (*routeFlagService) Update(context.Context, []settingsdomain.FeatureFlag) error { return nil }

func TestFlagReadAuthorization(t *testing.T) {
	sessions := &flagReadSessions{role: identitydomain.RoleAdmin}
	service := &routeFlagService{flag: settingsdomain.FeatureFlag{Name: "admin.banner", Enabled: true, Audience: settingsdomain.AudienceAll}}
	router := newAPIRouterWithServices(
		identityhttp.Handler{Version: "test", Sessions: sessions},
		accountshttp.KeyHandler{}, accountshttp.ConnectHandler{}, schedulehttp.Handler{},
		crawlhttp.CrawlHandler{}, quotahttp.QuotaHandler{}, sharinghttp.Handler{}, mediahttp.DownloadHandler{},
		settingshttp.Handler{Flags: service},
	)
	request := func(target, cookie string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, target, nil)
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: identityhttp.SessionCookieName, Value: cookie})
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}

	sessions.calls = 0
	admin := request("/api/v1/admin/flags/admin.banner", "valid")
	if admin.Code != http.StatusOK || admin.Header().Get("Content-Type") != "application/json" || admin.Header().Get("ETag") == "" || sessions.calls != 1 {
		t.Fatalf("admin read = %d %v, calls=%d, body=%s", admin.Code, admin.Header(), sessions.calls, admin.Body.String())
	}
	var body struct {
		Data struct {
			Links map[string]struct {
				Href   string `json:"href"`
				Method string `json:"method"`
			} `json:"links"`
		} `json:"data"`
	}
	if err := json.Unmarshal(admin.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	self := body.Data.Links["self"]
	if self.Href != "/api/v1/admin/flags/admin.banner" || self.Method != http.MethodGet {
		t.Fatalf("self link = %+v", self)
	}
	sessions.calls = 0
	followed := request(self.Href, "valid")
	if followed.Code != http.StatusOK || sessions.calls != 1 || followed.Header().Get("ETag") != admin.Header().Get("ETag") {
		t.Fatalf("following self = %d, calls=%d, ETag=%q", followed.Code, sessions.calls, followed.Header().Get("ETag"))
	}

	sessions.calls = 0
	anonymous := request("/api/v1/admin/flags/admin.banner", "")
	assertFlagReadProblem(t, anonymous, 401, "/problems/unauthenticated")
	if anonymous.Header().Get("WWW-Authenticate") == "" || sessions.calls != 0 {
		t.Fatalf("anonymous challenge/calls = %q/%d", anonymous.Header().Get("WWW-Authenticate"), sessions.calls)
	}

	sessions.role = identitydomain.RoleUser
	sessions.calls = 0
	user := request("/api/v1/admin/flags/admin.banner", "valid")
	assertFlagReadProblem(t, user, 403, "/problems/forbidden")
	if sessions.calls != 1 {
		t.Fatalf("user authentication calls = %d, want one", sessions.calls)
	}

	sessions.role = identitydomain.RoleAdmin
	for _, test := range []struct {
		target  string
		status  int
		typeURI string
	}{
		{target: "/api/v1/admin/flags/admin.banner?bad=%GG", status: 400, typeURI: "/problems/malformed-request"},
		{target: "/api/v1/admin/flags/admin.banner?unknown=1", status: 422, typeURI: "/problems/validation-error"},
		{target: "/api/v1/admin/flags/INVALID", status: 422, typeURI: "/problems/validation-error"},
	} {
		problem := request(test.target, "valid")
		assertFlagReadProblem(t, problem, test.status, test.typeURI)
	}

	oldRoute := request("/admin/flags", "valid")
	if oldRoute.Code != http.StatusOK || !strings.HasPrefix(oldRoute.Header().Get("Content-Type"), "application/json") || !strings.HasPrefix(oldRoute.Body.String(), "[") {
		t.Fatalf("legacy flags route = %d %s %s", oldRoute.Code, oldRoute.Header().Get("Content-Type"), oldRoute.Body.String())
	}
}

func assertFlagReadProblem(t *testing.T, response *httptest.ResponseRecorder, status int, typeURI string) {
	t.Helper()
	var body struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode problem response %s: %v", response.Body.String(), err)
	}
	if response.Code != status || response.Header().Get("Content-Type") != "application/problem+json" || body.Status != status || body.Type != typeURI {
		t.Fatalf("problem response = %d %q %+v; want %d %s", response.Code, response.Header().Get("Content-Type"), body, status, typeURI)
	}
}
