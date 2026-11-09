package roles

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	clusterhttp "github.com/jooservices/xflickr/internal/cluster/adapters/http"
	clusterapp "github.com/jooservices/xflickr/internal/cluster/app"
	clusterdomain "github.com/jooservices/xflickr/internal/cluster/domain"

	accountshttp "github.com/jooservices/xflickr/internal/accounts/adapters/http"
	crawlhttp "github.com/jooservices/xflickr/internal/crawl/adapters/http"
	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	identityapp "github.com/jooservices/xflickr/internal/identity/app"
	identitydomain "github.com/jooservices/xflickr/internal/identity/domain"
	mediahttp "github.com/jooservices/xflickr/internal/media/adapters/http"
	metricsapp "github.com/jooservices/xflickr/internal/platform/metrics/app"
	opshttp "github.com/jooservices/xflickr/internal/platform/ops/adapters/http"
	quotahttp "github.com/jooservices/xflickr/internal/quota/adapters/http"
	schedulehttp "github.com/jooservices/xflickr/internal/schedule/adapters/http"
	sharinghttp "github.com/jooservices/xflickr/internal/sharing/adapters/http"
)

// roleSessions authenticates the "session" cookie as a user with the given role.
type roleSessions struct {
	stubSessions
	role identitydomain.Role
}

func (s roleSessions) Authenticate(_ context.Context, raw string) (identityapp.Authenticated, error) {
	if raw != "valid" {
		return identityapp.Authenticated{}, identityapp.ErrUnauthenticated
	}
	return identityapp.Authenticated{User: identitydomain.User{ID: 5, Role: s.role}}, nil
}

func routerWith(sessions identityhttp.SessionService) http.Handler {
	return newAPIRouter(identityhttp.Handler{Version: "t", Sessions: sessions}, accountshttp.KeyHandler{}, accountshttp.ConnectHandler{}, schedulehttp.Handler{})
}

// samplePath fills the path parameters of a route pattern.
func samplePath(pattern string) string {
	for _, name := range []string{"{id}", "{nsid}", "{partNumber}"} {
		pattern = strings.ReplaceAll(pattern, name, "1")
	}
	return pattern
}

func splitRoute(key string) (method, pattern string) {
	method, pattern, _ = strings.Cut(key, " ")
	return method, pattern
}

// TestEveryRouteDeclaresAccess fails the build when a mounted route has no
// declared auth requirement, or the table lists a route that is not mounted.
func TestEveryRouteDeclaresAccess(t *testing.T) {
	router := newAPIRouter(identityhttp.Handler{Sessions: stubSessions{}}, accountshttp.KeyHandler{}, accountshttp.ConnectHandler{}, schedulehttp.Handler{}).(chi.Routes)
	mounted := map[string]bool{}
	if err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		mounted[method+" "+route] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var problems []string
	for route := range mounted {
		if _, ok := routeAccess[route]; !ok {
			problems = append(problems, "route without declared access: "+route)
		}
	}
	for route := range routeAccess {
		if !mounted[route] {
			problems = append(problems, "declared route is not mounted: "+route)
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		t.Fatal(strings.Join(problems, "\n"))
	}
}

func TestIdentityRoutesMatchMapping(t *testing.T) {
	want := []string{
		"GET /api/v1/authentication-options",
		"POST /api/v1/users",
		"GET /api/v1/users/{id}",
		"POST /api/v1/sessions",
		"DELETE /api/v1/sessions/current",
		"GET /api/v1/sessions/current",
		"GET /api/v1/sessions/{id}",
		"DELETE /api/v1/me/sessions",
		"GET /api/v1/me",
		"PATCH /api/v1/me",
		"GET /api/v1/me/sessions",
		"GET /api/v1/me/sessions/{id}",
		"DELETE /api/v1/me/sessions/{id}",
		"PUT /api/v1/me/password",
		"GET /api/v1/me/sharing",
		"PUT /api/v1/me/sharing",
	}
	for _, route := range want {
		if _, ok := routeAccess[route]; !ok {
			t.Errorf("identity route %q is missing from the access map", route)
		}
	}
	for route := range routeAccess {
		if strings.HasPrefix(route, "GET /auth/") || strings.HasPrefix(route, "POST /auth/") ||
			strings.HasPrefix(route, "DELETE /auth/") || route == "GET /me" || route == "PATCH /me" ||
			strings.HasPrefix(route, "GET /me/sessions") || strings.HasPrefix(route, "DELETE /me/sessions") ||
			route == "POST /me/password" || route == "PUT /me/sharing" {
			t.Errorf("legacy identity route %q remains mounted", route)
		}
	}
}

func TestUserReadRequiresAuthenticatedRouteAccess(t *testing.T) {
	if got := routeAccess["GET /api/v1/users/{id}"]; got != identityhttp.AccessUser {
		t.Fatalf("user read access = %v, want authenticated user access", got)
	}
}

func TestOldIdentityRoutesAreGone(t *testing.T) {
	router := routerWith(stubSessions{})
	for _, target := range []struct{ method, path string }{
		{http.MethodGet, "/auth/config"},
		{http.MethodPost, "/auth/register"},
		{http.MethodPost, "/auth/login"},
		{http.MethodPost, "/auth/logout"},
		{http.MethodDelete, "/auth/sessions"},
		{http.MethodGet, "/me"},
		{http.MethodPatch, "/me"},
		{http.MethodGet, "/me/sessions"},
		{http.MethodDelete, "/me/sessions/1"},
		{http.MethodPost, "/me/password"},
		{http.MethodPut, "/me/sharing"},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(target.method, target.path, nil))
		if recorder.Code != http.StatusNotFound || recorder.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s %s = %d %q, want problem+json 404", target.method, target.path, recorder.Code, recorder.Header().Get("Content-Type"))
		}
	}
}

func TestRoutesEnforceDeclaredAccess(t *testing.T) {
	anonymous := routerWith(stubSessions{})
	asUser := routerWith(roleSessions{role: identitydomain.RoleUser})
	for key, access := range routeAccess {
		method, pattern := splitRoute(key)
		path := samplePath(pattern)
		request := func(withSession bool) *http.Request {
			req := httptest.NewRequest(method, path, nil)
			if withSession {
				req.AddCookie(&http.Cookie{Name: identityhttp.SessionCookieName, Value: "valid"})
			}
			if method != http.MethodGet {
				// CSRF is not under test here.
				req.AddCookie(&http.Cookie{Name: identityhttp.CSRFCookieName, Value: strings.Repeat("a", 43)})
				req.Header.Set(identityhttp.CSRFHeaderName, strings.Repeat("a", 43))
			}
			return req
		}
		anonymousResponse := httptest.NewRecorder()
		anonymous.ServeHTTP(anonymousResponse, request(false))
		userResponse := httptest.NewRecorder()
		asUser.ServeHTTP(userResponse, request(true))
		switch access {
		case identityhttp.AccessPublic:
			if anonymousResponse.Code == http.StatusUnauthorized || anonymousResponse.Code == http.StatusForbidden && !strings.Contains(anonymousResponse.Body.String(), "csrf") {
				t.Errorf("%s public route answered %d", key, anonymousResponse.Code)
			}
		case identityhttp.AccessUser:
			if anonymousResponse.Code != http.StatusUnauthorized {
				t.Errorf("%s without session = %d, want 401", key, anonymousResponse.Code)
			}
			// The zero-value handlers of this stack answer 401 themselves; the
			// signed-in 200 path runs against the real stack in TestRoutesWithSessionIntegration.
		case identityhttp.AccessAdmin:
			if anonymousResponse.Code != http.StatusUnauthorized {
				t.Errorf("%s without session = %d, want 401", key, anonymousResponse.Code)
			}
			if userResponse.Code != http.StatusForbidden {
				t.Errorf("%s as plain user = %d, want 403", key, userResponse.Code)
			}
		}
	}
}

func TestAdminMetricsAuthorization(t *testing.T) {
	reader := adminMetricsReader{}
	makeRouter := func(reader adminMetricsReader) http.Handler {
		opsHandler := opshttp.Handler{
			ActorID: userIDFromPrincipal,
			Metrics: reader,
			Now: func() time.Time {
				return time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
			},
		}
		return newAPIRouterWithServices(
			identityhttp.Handler{Sessions: roleSessions{stubSessions: stubSessions{}, role: identitydomain.RoleAdmin}}, accountshttp.KeyHandler{}, accountshttp.ConnectHandler{}, schedulehttp.Handler{},
			crawlhttp.CrawlHandler{}, quotahttp.QuotaHandler{}, sharinghttp.Handler{}, mediahttp.DownloadHandler{}, opsHandler,
		)
	}
	path := "/admin/metrics?series=queue.jobs_available&queue=default&role=scheduler&node=node-1&from=2026-11-11T08:00:00Z&to=2026-11-11T09:00:00Z&step=1m"
	send := func(router http.Handler, target string, role identitydomain.Role) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if role != "" {
			req.AddCookie(&http.Cookie{Name: identityhttp.SessionCookieName, Value: "valid"})
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}
	if got := send(makeRouter(reader), path, "").Code; got != http.StatusUnauthorized {
		t.Fatalf("anonymous request status=%d, want 401", got)
	}
	userRouter := newAPIRouterWithServices(
		identityhttp.Handler{Sessions: roleSessions{stubSessions: stubSessions{}, role: identitydomain.RoleUser}},
		accountshttp.KeyHandler{}, accountshttp.ConnectHandler{}, schedulehttp.Handler{}, crawlhttp.CrawlHandler{}, quotahttp.QuotaHandler{},
		sharinghttp.Handler{}, mediahttp.DownloadHandler{}, opshttp.Handler{ActorID: userIDFromPrincipal, Metrics: reader, Now: func() time.Time { return time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC) }},
	)
	if got := send(userRouter, path, identitydomain.RoleUser).Code; got != http.StatusForbidden {
		t.Fatalf("user request status=%d, want 403", got)
	}
	adminRouter := newAPIRouterWithServices(
		identityhttp.Handler{Sessions: roleSessions{stubSessions: stubSessions{}, role: identitydomain.RoleAdmin}},
		accountshttp.KeyHandler{}, accountshttp.ConnectHandler{}, schedulehttp.Handler{}, crawlhttp.CrawlHandler{}, quotahttp.QuotaHandler{},
		sharinghttp.Handler{}, mediahttp.DownloadHandler{}, opshttp.Handler{ActorID: userIDFromPrincipal, Metrics: reader, Now: func() time.Time { return time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC) }},
	)
	if response := send(adminRouter, path, identitydomain.RoleAdmin); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"value":12`) {
		t.Fatalf("admin request status=%d body=%s", response.Code, response.Body.String())
	}

	missing := send(adminRouter, "/admin/metrics?series=queue.jobs_available", identitydomain.RoleAdmin)
	if missing.Code != http.StatusBadRequest || missing.Body.String() != "{\"code\":\"invalid_request\",\"message\":\"Invalid metrics query\"}\n" {
		t.Fatalf("missing query status=%d body=%s", missing.Code, missing.Body.String())
	}
	query := strings.TrimPrefix(path, "/admin/metrics?")
	for _, parameter := range []string{
		"series=queue.jobs_available&", "&queue=default", "&role=scheduler", "&node=node-1",
		"&from=2026-11-11T08:00:00Z", "&to=2026-11-11T09:00:00Z", "&step=1m",
	} {
		without := strings.Replace(query, parameter, "", 1)
		response := send(adminRouter, "/admin/metrics?"+without, identitydomain.RoleAdmin)
		if response.Code != http.StatusBadRequest || response.Body.String() != "{\"code\":\"invalid_request\",\"message\":\"Invalid metrics query\"}\n" {
			t.Errorf("missing %s status=%d body=%s", parameter, response.Code, response.Body.String())
		}
	}
	for _, name := range []string{"series", "queue", "role", "node", "from", "to", "step"} {
		duplicate := send(adminRouter, path+"&"+name+"=duplicate", identitydomain.RoleAdmin)
		if duplicate.Code != http.StatusBadRequest || duplicate.Body.String() != "{\"code\":\"invalid_request\",\"message\":\"Invalid metrics query\"}\n" {
			t.Errorf("duplicate %s status=%d body=%s", name, duplicate.Code, duplicate.Body.String())
		}
	}
	malformed := send(adminRouter, strings.Replace(path, "from=2026-11-11T08:00:00Z", "from=not-a-time", 1), identitydomain.RoleAdmin)
	if malformed.Code != http.StatusBadRequest || malformed.Body.String() != "{\"code\":\"invalid_request\",\"message\":\"Invalid metrics query\"}\n" {
		t.Fatalf("malformed query status=%d body=%s", malformed.Code, malformed.Body.String())
	}

	storeFailure := adminMetricsReader{err: errors.New("private database failure")}
	failed := send(makeRouter(storeFailure), path, identitydomain.RoleAdmin)
	if failed.Code != http.StatusInternalServerError || failed.Body.String() != "{\"code\":\"internal_error\",\"message\":\"Internal server error\"}\n" {
		t.Fatalf("storage failure status=%d body=%s", failed.Code, failed.Body.String())
	}
}

type adminMetricsReader struct{ err error }

func (r adminMetricsReader) ReadSeries(context.Context, metricsapp.SeriesQuery) ([]metricsapp.Point, error) {
	if r.err != nil {
		return nil, r.err
	}
	return []metricsapp.Point{{At: time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC), Value: 12}}, nil
}

func TestUndeclaredMatchedRouteRequiresAdmin(t *testing.T) {
	router := chi.NewRouter()
	router.Get("/surprise", func(http.ResponseWriter, *http.Request) {})
	resolve := accessResolver(router)
	if got := resolve(httptest.NewRequest(http.MethodGet, "/surprise", nil)); got != identityhttp.AccessAdmin {
		t.Fatalf("undeclared route access = %v", got)
	}
	if got := resolve(httptest.NewRequest(http.MethodGet, "/nowhere", nil)); got != identityhttp.AccessPublic {
		t.Fatalf("unmatched path access = %v", got)
	}
	if got := resolve(httptest.NewRequest(http.MethodGet, "/surprise%2Fx", nil)); got != identityhttp.AccessPublic {
		t.Fatalf("escaped unmatched path access = %v", got)
	}
}

func TestPathParameterErrorsAreJSON(t *testing.T) {
	router := routerWith(roleSessions{role: identitydomain.RoleUser})
	for _, target := range []string{"/keys/usage", "/keys/usage/quota", "/admin/users/x"} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.AddCookie(&http.Cookie{Name: identityhttp.SessionCookieName, Value: "valid"})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if strings.HasPrefix(target, "/admin") {
			continue // plain users are refused before binding
		}
		var body struct{ Type, Detail string }
		if rec.Code != http.StatusBadRequest || rec.Header().Get("Content-Type") != "application/problem+json" ||
			json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Type != "/problems/malformed-request" || body.Detail == "" {
			t.Fatalf("%s = %d %q %s", target, rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
		}
	}
}

func TestAPIErrorHandlers(t *testing.T) {
	rec := httptest.NewRecorder()
	internalError(rec, nil, context.Canceled)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "canceled") || !strings.Contains(rec.Body.String(), `"type":"/problems/internal-error"`) {
		t.Fatalf("internal error = %d %s", rec.Code, rec.Body.String())
	}
}

func TestNodeHeartbeatIsIsolatedToConfiguredInternalRouter(t *testing.T) {
	lastHeartbeat := time.Now().UTC()
	service := &routeNodeService{node: clusterdomain.Node{Name: "node-1", State: clusterdomain.StateOnline, LastHeartbeatAt: &lastHeartbeat}}
	nodes := clusterhttp.Handler{Service: service}
	routers := newAPIRoutersWithServices(identityhttp.Handler{Sessions: stubSessions{}}, accountshttp.KeyHandler{}, accountshttp.ConnectHandler{}, schedulehttp.Handler{}, crawlhttp.CrawlHandler{}, quotahttp.QuotaHandler{}, sharinghttp.Handler{}, mediahttp.DownloadHandler{}, nodes)
	request := func(method, path, auth string) *http.Request {
		req := httptest.NewRequest(method, path, strings.NewReader("{\"version\":\"v1\"}"))
		req.Header.Set("Content-Type", "application/json")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		return req
	}
	public := httptest.NewRecorder()
	routers.public.ServeHTTP(public, request(http.MethodPost, "/internal/nodes/heartbeat", "Bearer node-token"))
	if public.Code != http.StatusNotFound {
		t.Fatalf("public API exposed node heartbeat: %d %s", public.Code, public.Body.String())
	}
	unauthenticated := httptest.NewRecorder()
	routers.internal.ServeHTTP(unauthenticated, request(http.MethodPost, "/internal/nodes/heartbeat", ""))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("internal API without token = %d %s", unauthenticated.Code, unauthenticated.Body.String())
	}
	accepted := httptest.NewRecorder()
	routers.internal.ServeHTTP(accepted, request(http.MethodPost, "/internal/nodes/heartbeat", "Bearer node-token"))
	if accepted.Code != http.StatusOK || !strings.Contains(accepted.Body.String(), "node-1") {
		t.Fatalf("internal heartbeat = %d %s", accepted.Code, accepted.Body.String())
	}
	service.mu.Lock()
	calls := service.heartbeats
	service.mu.Unlock()
	if calls != 1 {
		t.Fatalf("accepted heartbeats = %d, want 1", calls)
	}
	other := httptest.NewRecorder()
	routers.internal.ServeHTTP(other, request(http.MethodGet, "/admin/nodes", "Bearer node-token"))
	if other.Code != http.StatusNotFound {
		t.Fatalf("internal listener exposed another route: %d", other.Code)
	}
}

type routeNodeService struct {
	mu         sync.Mutex
	node       clusterdomain.Node
	heartbeats int
}

func (service *routeNodeService) Create(context.Context, string, []string) (clusterdomain.Node, string, error) {
	return service.node, "unused", nil
}

func (service *routeNodeService) AuthenticateToken(_ context.Context, token string) (clusterdomain.Node, error) {
	if token != "node-token" {
		return clusterdomain.Node{}, clusterapp.ErrInvalidToken
	}
	return service.node, nil
}

func (service *routeNodeService) Heartbeat(_ context.Context, name string, _ clusterapp.HeartbeatInput) (clusterdomain.Node, error) {
	service.mu.Lock()
	service.heartbeats++
	service.mu.Unlock()
	service.node.Name = name
	return service.node, nil
}
