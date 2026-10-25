package roles

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accountshttp "github.com/jooservices/xflickr/internal/accounts/adapters/http"
	crawlhttp "github.com/jooservices/xflickr/internal/crawl/adapters/http"
	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	"github.com/jooservices/xflickr/internal/platform/config"
	schedulehttp "github.com/jooservices/xflickr/internal/schedule/adapters/http"
)

func TestDatabaseRolesRejectInvalidConfiguration(t *testing.T) {
	for _, name := range []string{
		"DB_HOST", "DB_NAME", "DB_USER", "REDIS_PREFIX", "OPENSEARCH_INDEX_PREFIX", "NODE_ID",
		"XFLICKR_MASTER_KEYS", "XFLICKR_MASTER_KEY_ACTIVE",
	} {
		t.Setenv(name, "")
	}
	if err := (schedulerRole{}).Run(context.Background()); err == nil {
		t.Fatal("scheduler accepted missing configuration")
	}
	if err := (crawlRole{}).Run(context.Background()); err == nil {
		t.Fatal("crawl role accepted missing configuration")
	}
	if err := newConfiguredAPIRole(":0", "test").Run(context.Background()); err == nil {
		t.Fatal("configured API role accepted missing configuration")
	}
}

func TestDatabaseRolesStopForUnavailableContext(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for name, run := range map[string]func(context.Context) error{
		"scheduler": (schedulerRole{}).Run,
		"crawl":     (crawlRole{}).Run,
	} {
		for _, ctx := range []context.Context{nil, cancelled} {
			if err := run(ctx); err != nil {
				t.Errorf("%s role with unavailable context returned %v", name, err)
			}
		}
	}
}

func TestAPIRouterUsesProvidedCrawlHandler(t *testing.T) {
	router := newAPIRouter(identityhttp.Handler{}, accountshttp.KeyHandler{}, accountshttp.ConnectHandler{}, schedulehttp.Handler{}, crawlhttp.CrawlHandler{})
	if router == nil {
		t.Fatal("API router is nil")
	}
}

func TestNodeIDFallsBackToHostName(t *testing.T) {
	if got := nodeID(config.Config{}); got == "" {
		t.Fatal("nodeID returned an empty value")
	}
	if got := nodeIDWithHostname(config.Config{}, func() (string, error) { return "", errors.New("hostname unavailable") }); got != "unknown" {
		t.Fatalf("nodeIDWithHostname() = %q, want unknown", got)
	}
}

func TestBuildIdentityHandlerRejectsInvalidPasswordSettings(t *testing.T) {
	_, err := buildIdentityHandler(&pgxpool.Pool{}, nil, config.Config{}, "test")
	if err == nil {
		t.Fatal("identity handler accepted invalid password settings")
	}
	if _, err = buildConfiguredAPIHandler(&pgxpool.Pool{}, nil, config.Config{}, "test"); err == nil {
		t.Fatal("API handler accepted invalid identity settings")
	}
}

func TestAPIRoleReturnsServeErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want error
	}{
		{name: "closed server", err: http.ErrServerClosed},
		{name: "serve failure", err: errors.New("serve failed"), want: errors.New("serve failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			role := NewAPIRole(":0", "test")
			role.listenerFactory = func(string) (net.Listener, error) {
				return failingAcceptListener{err: test.err}, nil
			}
			err := role.Run(context.Background())
			if test.want == nil && err != nil {
				t.Fatalf("APIRole.Run() error = %v, want nil", err)
			}
			if test.want != nil && (err == nil || err.Error() != test.want.Error()) {
				t.Fatalf("APIRole.Run() error = %v, want %v", err, test.want)
			}
		})
	}
}

type failingAcceptListener struct{ err error }

func (listener failingAcceptListener) Accept() (net.Conn, error) { return nil, listener.err }
func (failingAcceptListener) Close() error                       { return nil }
func (failingAcceptListener) Addr() net.Addr                     { return &net.TCPAddr{} }

func TestDatabaseRolesStopWhenPostgresIsUnavailable(t *testing.T) {
	for name, value := range map[string]string{
		"DB_HOST": "127.0.0.1", "DB_PORT": "1", "DB_NAME": "xflickr_wtunit", "DB_USER": "unused",
		"REDIS_PREFIX": "roles-test:", "OPENSEARCH_INDEX_PREFIX": "roles-test_", "NODE_ID": "roles-test",
		"XFLICKR_MASTER_KEYS":       "test:" + base64.StdEncoding.EncodeToString(make([]byte, 32)),
		"XFLICKR_MASTER_KEY_ACTIVE": "test",
	} {
		t.Setenv(name, value)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := (schedulerRole{}).Run(ctx); err == nil {
		t.Fatal("scheduler started without PostgreSQL")
	}
	if err := (crawlRole{}).Run(ctx); err == nil {
		t.Fatal("crawl role started without PostgreSQL")
	}
}

func TestServeWebAssets(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html>app</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "app.js"), []byte("asset"), 0o600); err != nil {
		t.Fatal(err)
	}

	router := chi.NewRouter()
	serveWebAssets(router, root)
	router.Get("/healthz", func(http.ResponseWriter, *http.Request) {}) // chi runs middleware only once a route exists
	tests := []struct {
		name, method, path, body string
		status                   int
		navigation               bool
		accept                   string
	}{
		{name: "index", method: http.MethodGet, path: "/", body: "<html>app</html>", status: http.StatusOK, navigation: true},
		{name: "spa route", method: http.MethodGet, path: "/register", body: "<html>app</html>", status: http.StatusOK, navigation: true},
		{name: "index without accept", method: http.MethodGet, path: "/", body: "<html>app</html>", status: http.StatusOK},
		{name: "spa route without accept", method: http.MethodGet, path: "/register", body: "<html>app</html>", status: http.StatusOK},
		{name: "asset", method: http.MethodGet, path: "/assets/app.js", body: "asset", status: http.StatusOK},
		{name: "missing asset", method: http.MethodGet, path: "/assets/missing.js", status: http.StatusNotFound},
		{name: "head", method: http.MethodHead, path: "/", status: http.StatusOK, navigation: true},
		{name: "unsupported method", method: http.MethodPost, path: "/register", status: http.StatusNotFound},
		{name: "unknown api path", method: http.MethodGet, path: "/keys/usage", body: `{"code":"not_found"}`, status: http.StatusNotFound, accept: "application/json"},
		{name: "json client", method: http.MethodGet, path: "/keys/usage", body: `{"code":"not_found"}`, status: http.StatusNotFound, accept: "application/json"},
		{name: "json client on index", method: http.MethodGet, path: "/", body: `{"code":"not_found"}`, status: http.StatusNotFound, accept: "application/json"},
		{name: "backend only path", method: http.MethodGet, path: "/auth/nope", body: `{"code":"not_found"}`, status: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, test.path, nil)
			if test.accept != "" {
				request.Header.Set("Accept", test.accept)
			}
			if test.navigation {
				request.Header.Set("Accept", "text/html")
			}
			router.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if !strings.Contains(response.Body.String(), test.body) {
				t.Fatalf("body = %q, want it to contain %q", response.Body.String(), test.body)
			}
		})
	}
}

func TestServeWebAssetsDisabled(t *testing.T) {
	router := chi.NewRouter()
	serveWebAssets(router, "")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestSharedPageAndAPIURLIsNotCachedAcrossAccept(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html>app</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	serveWebAssets(router, root)
	router.Get("/keys", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("[]")) })

	for name, accept := range map[string]string{"page": "text/html", "api": "application/json"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/keys", nil)
		request.Header.Set("Accept", accept)
		router.ServeHTTP(response, request)
		if got := response.Header().Values("Vary"); len(got) == 0 || !strings.Contains(strings.Join(got, ","), "Accept") {
			t.Errorf("%s: Vary = %v, want it to include Accept", name, got)
		}
		if name == "page" && response.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("page: Cache-Control = %q, want no-cache", response.Header().Get("Cache-Control"))
		}
	}

	fallback := httptest.NewRecorder()
	router.ServeHTTP(fallback, httptest.NewRequest(http.MethodGet, "/register", nil))
	if !strings.Contains(strings.Join(fallback.Header().Values("Vary"), ","), "Accept") || fallback.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("fallback headers = %v", fallback.Header())
	}
}
