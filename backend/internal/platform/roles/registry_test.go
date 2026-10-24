package roles

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	identityredis "github.com/jooservices/xflickr/internal/identity/adapters/redis"
	identityapp "github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/config"
	redisv9 "github.com/redis/go-redis/v9"
)

func TestBuildIdentityHandlerWiresSettingsAndDummyHash(t *testing.T) {
	client := redisv9.NewClient(&redisv9.Options{Addr: "127.0.0.1:1"})
	defer client.Close()
	cfg := config.Config{Identity: config.IdentityConfig{Argon2Memory: 32 * 1024, Argon2Iterations: 1, Argon2Parallelism: 1, Argon2SaltLength: 16, Argon2KeyLength: 32}, Redis: config.RedisConfig{Prefix: "test:"}}
	handler, err := buildIdentityHandler(&pgxpool.Pool{}, client, cfg, "test-version")
	if err != nil {
		t.Fatal(err)
	}
	if handler.RegisterUseCase.Policy == nil || handler.Users == nil || handler.Sessions == nil || handler.Version != "test-version" {
		t.Fatalf("handler wiring is incomplete: %#v", handler)
	}
	if handler.LoginUseCase.DummyPasswordHash == "" || !domain.VerifyPassword(handler.LoginUseCase.DummyPasswordHash, "xflickr-invalid-login-dummy") {
		t.Fatal("login dummy hash is missing or invalid")
	}
	limiter, ok := handler.LoginUseCase.Limiter.(identityredis.Limiter)
	if !ok || limiter.Prefix != "test:" {
		t.Fatal("login limiter is not configured with the Redis prefix")
	}
}

func TestBuildIdentityHandlerRejectsInvalidArgonConfiguration(t *testing.T) {
	cfg := config.Config{Identity: config.IdentityConfig{Argon2Parallelism: 1}}
	if _, err := buildIdentityHandler(&pgxpool.Pool{}, redisv9.NewClient(&redisv9.Options{Addr: "127.0.0.1:1"}), cfg, "test"); err == nil {
		t.Fatal("buildIdentityHandler() succeeded with invalid Argon2 configuration")
	}
}

func TestConfiguredAPIRoleReturnsDatabaseConnectionError(t *testing.T) {
	for key, value := range map[string]string{
		"DB_HOST":                   "127.0.0.1",
		"DB_PORT":                   "1",
		"DB_NAME":                   "xflickr_test",
		"DB_USER":                   "postgres",
		"DB_PASSWORD":               "",
		"REDIS_PREFIX":              "roles_test:",
		"OPENSEARCH_INDEX_PREFIX":   "roles_test_",
		"NODE_ID":                   "roles-test",
		"XFLICKR_MASTER_KEYS":       "active:YmFzZTY0",
		"XFLICKR_MASTER_KEY_ACTIVE": "active",
	} {
		t.Setenv(key, value)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := newConfiguredAPIRole(":0", "test").Run(ctx); err == nil {
		t.Fatal("configured API role started without a database")
	}
}

func TestRegisterBuiltinPanicsOnInvalidRole(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("registerBuiltin() did not panic for an invalid role")
		}
	}()
	registerBuiltin(NewRegistry(), testRole{})
}

func TestConfiguredAPIRoleReturnsConfigurationError(t *testing.T) {
	t.Setenv("DB_HOST", "")
	if err := newConfiguredAPIRole(":0", "test").Run(context.Background()); err == nil {
		t.Fatal("configured API role started with missing database configuration")
	}
}

type testRole struct {
	name string
	run  func(context.Context) error
}

func (r testRole) Name() string { return r.name }
func (r testRole) Run(ctx context.Context) error {
	if r.run != nil {
		return r.run(ctx)
	}
	<-ctx.Done()
	return nil
}

func TestParseRoleListSingleAndCommaSeparated(t *testing.T) {
	registry := testRegistry(t, "api", "scheduler")
	for _, test := range []struct {
		name string
		raw  string
		want []string
	}{
		{name: "single", raw: "api", want: []string{"api"}},
		{name: "comma separated with whitespace", raw: " api, scheduler ", want: []string{"api", "scheduler"}},
		{name: "duplicate role", raw: "api,api", want: []string{"api"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseRoleList(test.raw, registry)
			if err != nil {
				t.Fatalf("ParseRoleList() error = %v", err)
			}
			if names := roleNames(got); strings.Join(names, ",") != strings.Join(test.want, ",") {
				t.Fatalf("role names = %v, want %v", names, test.want)
			}
		})
	}
}

func TestParseRoleListUnknownShowsValidNames(t *testing.T) {
	registry := testRegistry(t, "scheduler", "api")
	_, err := ParseRoleList("missing", registry)
	if err == nil || !strings.Contains(err.Error(), "unknown role \"missing\"") || !strings.Contains(err.Error(), "valid roles: api, scheduler") {
		t.Fatalf("error = %v, want unknown role and sorted valid roles", err)
	}
}

func TestParseRoleListRequiresAtLeastOneRole(t *testing.T) {
	registry := testRegistry(t, "api")
	for _, raw := range []string{"", "  ", ", ,"} {
		if _, err := ParseRoleList(raw, registry); err == nil || !strings.Contains(err.Error(), "a role is required") {
			t.Errorf("ParseRoleList(%q) error = %v, want role required", raw, err)
		}
	}
}

func TestRegistryRegisterAndLookup(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(nil); err == nil {
		t.Fatal("Register(nil) error = nil")
	}
	role := testRole{name: "node-agent"}
	if err := registry.Register(role); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.Register(role); err == nil {
		t.Fatal("Register() duplicate error = nil")
	}
	if _, ok := registry.Get("node-agent"); !ok {
		t.Fatal("Get() did not find registered role")
	}
	if _, ok := registry.Get("missing"); ok {
		t.Fatal("Get() found an unregistered role")
	}
	if got := strings.Join(registry.Names(), ","); got != "node-agent" {
		t.Fatalf("Names() = %q, want node-agent", got)
	}
	if got := registry.ValidRoles(); got != "node-agent" {
		t.Fatalf("ValidRoles() = %q, want node-agent", got)
	}
}

func TestRegistryRejectsInvalidNames(t *testing.T) {
	registry := NewRegistry()
	for _, name := range []string{"", "  ", "bad,name", "two words", "tab\tname"} {
		if err := registry.Register(testRole{name: name}); err == nil {
			t.Errorf("Register(%q) error = nil, want invalid name", name)
		}
	}
}

func TestRunCancelsEverySelectedRole(t *testing.T) {
	started := make(chan struct{}, 2)
	finished := make(chan struct{}, 2)
	block := func(ctx context.Context) error {
		started <- struct{}{}
		<-ctx.Done()
		finished <- struct{}{}
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, []Role{
			testRole{name: "one", run: block},
			testRole{name: "two", run: block},
		})
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			cancel()
			t.Fatal("roles did not start")
		}
	}
	cancel()
	for range 2 {
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("role did not observe cancellation")
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not return after cancellation")
	}
}

func TestRunCancelsOtherRolesAfterFailure(t *testing.T) {
	failed := errors.New("role failed")
	started := make(chan struct{})
	finished := make(chan struct{})
	block := testRole{name: "blocking", run: func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		close(finished)
		return nil
	}}
	bad := testRole{name: "failing", run: func(context.Context) error {
		<-started
		return failed
	}}
	if err := Run(context.Background(), []Role{block, bad}); !errors.Is(err, failed) {
		t.Fatalf("Run() error = %v, want %v", err, failed)
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("blocking role did not observe failure cancellation")
	}
}

func TestRunRejectsEmptyOrNilRoles(t *testing.T) {
	if err := Run(context.Background(), nil); err == nil {
		t.Fatal("Run(nil) error = nil")
	}
	if err := Run(context.Background(), []Role{nil}); err == nil {
		t.Fatal("Run([nil]) error = nil")
	}
}

func TestBuiltinRegistryHasAllSupportedRoles(t *testing.T) {
	registry := NewBuiltinRegistry("test")
	want := "ai-worker,api,crawl,indexer,media,node-agent,scheduler,sync"
	if got := strings.Join(registry.Names(), ","); got != want {
		t.Fatalf("Names() = %q, want %q", got, want)
	}
	scheduler, ok := registry.Get("scheduler")
	if !ok || scheduler.Name() != "scheduler" {
		t.Fatal("registry did not return scheduler role")
	}
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		done <- scheduler.Run(ctx)
	}()
	<-started
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("scheduler Run() error = %v", err)
	}
}

func TestAPIRoleServesHealthAndStops(t *testing.T) {
	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve address: %v", err)
	}
	address := reservation.Addr().String()
	if err := reservation.Close(); err != nil {
		t.Fatalf("release address: %v", err)
	}

	role := NewAPIRole(address, "abc123")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- role.Run(ctx) }()

	client := &http.Client{Timeout: 100 * time.Millisecond}
	var response *http.Response
	for attempt := 0; attempt < 20; attempt++ {
		response, err = client.Get("http://" + address + "/healthz")
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		cancel()
		t.Fatalf("GET /healthz: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	if readErr != nil {
		cancel()
		t.Fatalf("read health response: %v", readErr)
	}
	if response.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("GET /healthz status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if !strings.Contains(string(body), `"version":"abc123"`) {
		cancel()
		t.Fatalf("GET /healthz body = %q, want version abc123", body)
	}
	if role.Name() != "api" {
		cancel()
		t.Fatalf("Name() = %q, want api", role.Name())
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("API role did not stop after cancellation")
	}
}

func TestAPIRoleReturnsListenError(t *testing.T) {
	if err := NewAPIRole("missing-port", "test").Run(context.Background()); err == nil {
		t.Fatal("Run() error = nil, want invalid listen address error")
	}
}

type stubSessions struct{}

func (stubSessions) Issue(context.Context, int64) (string, domain.Session, string, error) {
	return "", domain.Session{}, "", errors.New("sessions are not available")
}

func (stubSessions) Authenticate(context.Context, string) (identityapp.Authenticated, error) {
	return identityapp.Authenticated{}, identityapp.ErrUnauthenticated
}

func (stubSessions) Revoke(context.Context, string) error { return nil }

func (stubSessions) RevokeOthers(context.Context, int64, []byte) error { return nil }

func TestAPIRouterRequiresSessionAndCSRF(t *testing.T) {
	router := newAPIRouter(identityhttp.Handler{Version: "router-test", Sessions: stubSessions{}})
	health := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthResponse := httptest.NewRecorder()
	router.ServeHTTP(healthResponse, health)
	if healthResponse.Code != http.StatusOK || !strings.Contains(healthResponse.Body.String(), `"version":"router-test"`) {
		t.Fatalf("health status=%d body=%s", healthResponse.Code, healthResponse.Body.String())
	}

	me := httptest.NewRequest(http.MethodGet, "/me", nil)
	meResponse := httptest.NewRecorder()
	router.ServeHTTP(meResponse, me)
	if meResponse.Code != http.StatusUnauthorized {
		t.Fatalf("me status = %d", meResponse.Code)
	}

	login := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"email":"u@example.com","password":"secret12"}`))
	login.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	router.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusForbidden {
		t.Fatalf("login status = %d body = %s", loginResponse.Code, loginResponse.Body.String())
	}

	token, err := domain.NewOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	revoke := httptest.NewRequest(http.MethodDelete, "/auth/sessions", nil)
	revoke.AddCookie(&http.Cookie{Name: identityhttp.CSRFCookieName, Value: token})
	revoke.Header.Set(identityhttp.CSRFHeaderName, token)
	revokeResponse := httptest.NewRecorder()
	router.ServeHTTP(revokeResponse, revoke)
	if revokeResponse.Code != http.StatusUnauthorized {
		t.Fatalf("delete status = %d body = %s", revokeResponse.Code, revokeResponse.Body.String())
	}
}

func testRegistry(t *testing.T, names ...string) *Registry {
	t.Helper()
	registry := NewRegistry()
	for _, name := range names {
		if err := registry.Register(testRole{name: name}); err != nil {
			t.Fatalf("Register(%q): %v", name, err)
		}
	}
	return registry
}

func roleNames(roles []Role) []string {
	names := make([]string, len(roles))
	for index, role := range roles {
		names[index] = role.Name()
	}
	return names
}
