package roles

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	redisv9 "github.com/redis/go-redis/v9"

	accountshttp "github.com/jooservices/xflickr/internal/accounts/adapters/http"
	crawlhttp "github.com/jooservices/xflickr/internal/crawl/adapters/http"
	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	"github.com/jooservices/xflickr/internal/platform/config"
	quotahttp "github.com/jooservices/xflickr/internal/quota/adapters/http"
	quotaapp "github.com/jooservices/xflickr/internal/quota/app"
	schedulehttp "github.com/jooservices/xflickr/internal/schedule/adapters/http"
)

func TestNodeIDPrefersConfiguredID(t *testing.T) {
	if got := nodeID(config.Config{Node: config.NodeConfig{ID: " node-7 "}}); got != "node-7" {
		t.Fatalf("nodeID = %q", got)
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		t.Skip("no host name available")
	}
	if got := nodeID(config.Config{}); got != host {
		t.Fatalf("nodeID fallback = %q, want %q", got, host)
	}
}

func TestBuildQuotaHandlerWiresStatesAndNode(t *testing.T) {
	client := redisv9.NewClient(&redisv9.Options{Addr: "127.0.0.1:1"})
	defer client.Close()
	cfg := config.Config{Node: config.NodeConfig{ID: "n1"}, Redis: config.RedisConfig{Prefix: "wt:"}}
	handler := buildQuotaHandler(&pgxpool.Pool{}, client, cfg)
	states, ok := handler.States.(quotaapp.States)
	if !ok || states.Keys == nil || states.Budgets == nil || states.Live == nil || states.History == nil || handler.Node != "n1" || handler.ActorID == nil {
		t.Fatalf("handler wiring = %+v", handler)
	}
}

func TestNewQuotaEmitterNeedsObservabilityCredentials(t *testing.T) {
	states := quotaapp.States{}
	if newQuotaEmitter(states, config.Config{Obs: config.ObsConfig{URL: "http://o"}}) != nil {
		t.Fatal("emitter created without credentials")
	}
	if newQuotaEmitter(states, config.Config{Obs: config.ObsConfig{Username: "u"}}) != nil {
		t.Fatal("emitter created without URL")
	}
	emitter := newQuotaEmitter(states, config.Config{Node: config.NodeConfig{ID: "n1"}, Obs: config.ObsConfig{URL: "http://o", Username: "u", Password: "p", Org: "default", Stream: "xflickr"}})
	if emitter == nil || emitter.Sink == nil || emitter.Node != "n1" {
		t.Fatalf("emitter = %+v", emitter)
	}
}

type emitStates struct{ called chan struct{} }

func (e emitStates) All(context.Context) ([]quotaapp.KeyState, error) {
	select {
	case e.called <- struct{}{}:
	default:
	}
	return nil, nil
}

func TestQuotaRuntimeStartRunsEmitter(t *testing.T) {
	store := &startStore{rebuilt: make(chan struct{}, 1), flushed: make(chan struct{}, 8)}
	called := make(chan struct{}, 1)
	quota := quotaRuntime{
		Rebuilder: &quotaapp.Rebuilder{Store: store, Calls: startCalls{}, Counters: startCounters{}},
		Flusher:   quotaapp.Flusher{Store: store, Counters: startCounters{}, Interval: time.Hour},
		Emitter:   &quotaapp.Emitter{States: emitStates{called: called}, Sink: nopSink{}, Interval: 5 * time.Millisecond},
	}
	stop := quota.start(context.Background(), nil)
	defer stop()
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("emitter did not run in the background")
	}
}

type nopSink struct{}

func (nopSink) Send(context.Context, []quotaapp.Event) error { return nil }

func TestAPIRouterServesQuotaEndpointsBehindAuthentication(t *testing.T) {
	router := newAPIRouterWithQuota(identityhttp.Handler{Version: "t", Sessions: stubSessions{}}, accountshttp.KeyHandler{}, accountshttp.ConnectHandler{}, schedulehttp.Handler{}, crawlhttp.CrawlHandler{}, quotahttp.QuotaHandler{})
	for _, path := range []string{"/keys/1/quota", "/admin/quota"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":"unauthenticated"`) {
			t.Fatalf("GET %s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestAPIRouterServesSPAForHTMLNavigationOnPagePaths(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html>app</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XFLICKR_WEB_DIR", root)
	router := newAPIRouterWithQuota(identityhttp.Handler{Version: "t", Sessions: stubSessions{}}, accountshttp.KeyHandler{}, accountshttp.ConnectHandler{}, schedulehttp.Handler{}, crawlhttp.CrawlHandler{}, quotahttp.QuotaHandler{})

	for _, path := range []string{"/register", "/keys", "/admin/nodes"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("Accept", "text/html")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "<html>app</html>") {
				t.Fatalf("GET %s status=%d body=%q, want SPA index", path, response.Code, response.Body.String())
			}
		})
	}

	request := httptest.NewRequest(http.MethodGet, "/keys", nil)
	request.Header.Set("Accept", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":"unauthenticated"`) {
		t.Fatalf("GET /keys as API request status=%d body=%q, want unauthenticated API response", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/unknown", nil)
	request.Header.Set("Accept", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Header().Get("Content-Type"), "application/json") || !strings.Contains(response.Body.String(), `"code":"not_found"`) {
		t.Fatalf("GET /unknown status=%d content-type=%q body=%q, want JSON 404", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
}
