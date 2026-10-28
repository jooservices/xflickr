package roles

import (
	"context"
	"encoding/json"
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
		var body struct{ Code, Message string }
		if rec.Code != http.StatusBadRequest || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") ||
			json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Code != "invalid_request" || body.Message == "" {
			t.Fatalf("%s = %d %q %s", target, rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
		}
	}
}

func TestAPIErrorHandlers(t *testing.T) {
	rec := httptest.NewRecorder()
	internalError(rec, nil, context.Canceled)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "canceled") || !strings.Contains(rec.Body.String(), `"code":"internal_error"`) {
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
