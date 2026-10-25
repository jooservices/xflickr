package roles

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	redisv9 "github.com/redis/go-redis/v9"

	accountshttp "github.com/jooservices/xflickr/internal/accounts/adapters/http"
	accountsettings "github.com/jooservices/xflickr/internal/accounts/adapters/settings"
	accountsapp "github.com/jooservices/xflickr/internal/accounts/app"
	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	identityredis "github.com/jooservices/xflickr/internal/identity/adapters/redis"
	identityapp "github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/config"
	schedulehttp "github.com/jooservices/xflickr/internal/schedule/adapters/http"
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

func TestBuildIdentityHandlerRejectsInvalidPasswordHashParameters(t *testing.T) {
	if _, err := buildIdentityHandler(nil, nil, config.Config{}, "test"); err == nil {
		t.Fatal("identity handler accepted invalid Argon2 settings")
	}
}

func TestBuildIdentityHandlerRejectsInvalidArgonConfiguration(t *testing.T) {
	cfg := config.Config{Identity: config.IdentityConfig{Argon2Parallelism: 1}}
	if _, err := buildIdentityHandler(&pgxpool.Pool{}, redisv9.NewClient(&redisv9.Options{Addr: "127.0.0.1:1"}), cfg, "test"); err == nil {
		t.Fatal("buildIdentityHandler() succeeded with invalid Argon2 configuration")
	}
}

func TestBuildPlanHandler(t *testing.T) {
	if _, err := buildPlanHandler(nil); err == nil {
		t.Fatal("buildPlanHandler accepted a nil database")
	}
	handler, err := buildPlanHandler(&pgxpool.Pool{})
	if err != nil {
		t.Fatal(err)
	}
	if handler.Plans == nil {
		t.Fatal("plan service is missing")
	}
	ctx := identityhttp.WithPrincipal(t.Context(), identityhttp.Principal{User: domain.User{ID: 17}})
	if actor, ok := handler.ActorID(ctx); !ok || actor != 17 {
		t.Fatalf("actor = %d %v", actor, ok)
	}
	if _, ok := handler.ActorID(t.Context()); ok {
		t.Fatal("missing principal was accepted")
	}
	if _, ok := handler.ActorID(identityhttp.WithPrincipal(t.Context(), identityhttp.Principal{})); ok {
		t.Fatal("zero user was accepted")
	}
}

func TestBuildConfiguredAPIHandler(t *testing.T) {
	client := redisv9.NewClient(&redisv9.Options{Addr: "127.0.0.1:1"})
	defer client.Close()
	if _, err := buildConfiguredAPIHandler(nil, client, config.Config{}, "test"); err == nil {
		t.Fatal("handler builder accepted a nil database")
	}
	encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	cfg := config.Config{
		Crypto:   config.CryptoConfig{MasterKeys: "test:" + encoded, ActiveMasterKeyID: "test"},
		Identity: config.IdentityConfig{Argon2Memory: 32 * 1024, Argon2Iterations: 1, Argon2Parallelism: 1, Argon2SaltLength: 16, Argon2KeyLength: 32},
		Redis:    config.RedisConfig{Prefix: "handler-test:"},
	}
	handler, err := buildConfiguredAPIHandler(&pgxpool.Pool{}, client, cfg, "configured-test")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"version":"configured-test"`) {
		t.Fatalf("health status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestConfiguredAPIRoleReturnsUnreachableWorktreeDatabaseError(t *testing.T) {
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

func TestConfiguredAPIRoleReturnsDatabaseConnectionError(t *testing.T) {
	for key, value := range map[string]string{
		"DB_HOST":                   "127.0.0.1",
		"DB_PORT":                   "1",
		"DB_NAME":                   "xflickr_wt_roles_unit",
		"DB_USER":                   "xflickr_roles_test",
		"REDIS_PREFIX":              "roles_unit:",
		"OPENSEARCH_INDEX_PREFIX":   "roles_unit_",
		"NODE_ID":                   "roles-unit-test",
		"XFLICKR_MASTER_KEYS":       "test:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32)),
		"XFLICKR_MASTER_KEY_ACTIVE": "test",
	} {
		t.Setenv(key, value)
	}
	if err := newConfiguredAPIRole(":0", "test").Run(context.Background()); err == nil {
		t.Fatal("configured API role started without a reachable database")
	}
}

func TestConfiguredAPIRoleStartsAgainstTheWorktreeServices(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use shared worktree services")
	}
	t.Setenv("REDIS_PREFIX", "roles_integration:")
	t.Setenv("OPENSEARCH_INDEX_PREFIX", "roles_integration_")
	t.Setenv("NODE_ID", "roles-integration")
	t.Setenv("XFLICKR_MASTER_KEYS", "test:"+base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32)))
	t.Setenv("XFLICKR_MASTER_KEY_ACTIVE", "test")
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../"))
	cfg, err := config.LoadWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatalf("load worktree runtime configuration: %v", err)
	}
	if !strings.HasPrefix(cfg.DB.Name, "xflickr_wt") {
		t.Fatal("configured API role requires a worktree database")
	}
	t.Setenv("DB_HOST", cfg.DB.Host)
	t.Setenv("DB_PORT", strconv.Itoa(cfg.DB.Port))
	t.Setenv("DB_NAME", cfg.DB.Name)
	t.Setenv("DB_USER", cfg.DB.User)
	t.Setenv("DB_PASSWORD", cfg.DB.Password)
	t.Setenv("DB_SSLMODE", cfg.DB.SSLMode)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- newConfiguredAPIRole("127.0.0.1:0", "integration-test").Run(ctx) }()
	select {
	case err := <-done:
		t.Fatalf("configured API role stopped during startup: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("configured API role shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("configured API role did not stop after cancellation")
	}
}

type testRole struct {
	name string
	run  func(context.Context) error
}

type rejectingListener struct{ err error }

func (listener rejectingListener) Accept() (net.Conn, error) { return nil, listener.err }
func (rejectingListener) Close() error                       { return nil }
func (rejectingListener) Addr() net.Addr                     { return &net.TCPAddr{} }

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
	cancel()
	if err := scheduler.Run(ctx); err != nil {
		t.Fatalf("scheduler Run() error = %v", err)
	}
}

func TestSchedulerRoleReportsMissingConfigurationAndIdleRoleStops(t *testing.T) {
	if err := (schedulerRole{}).Run(nil); err != nil {
		t.Fatalf("scheduler role with nil context = %v", err)
	}
	for _, key := range []string{"DB_HOST", "DB_NAME", "DB_USER", "REDIS_PREFIX", "OPENSEARCH_INDEX_PREFIX", "NODE_ID", "XFLICKR_MASTER_KEYS", "XFLICKR_MASTER_KEY_ACTIVE"} {
		t.Setenv(key, "")
	}
	if err := (schedulerRole{}).Run(context.Background()); err == nil {
		t.Fatal("scheduler role started without required configuration")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (idleRole{name: "media"}).Run(ctx); err != nil {
		t.Fatalf("idle worker role Run() = %v", err)
	}
}

func TestSchedulerRoleBuildsWithValidSettingsAndStopsOnDatabaseDeadline(t *testing.T) {
	for key, value := range map[string]string{
		"DB_HOST": "127.0.0.1", "DB_PORT": "1", "DB_NAME": "xflickr_wtunit", "DB_USER": "unused",
		"REDIS_PREFIX": "xflickr_wtunit:", "OPENSEARCH_INDEX_PREFIX": "xflickr_wtunit_", "NODE_ID": "scheduler-test",
		"XFLICKR_MASTER_KEYS": "test-v1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", "XFLICKR_MASTER_KEY_ACTIVE": "test-v1",
	} {
		t.Setenv(key, value)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := (schedulerRole{}).Run(ctx); err == nil {
		t.Fatal("scheduler role ignored its database deadline")
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

func TestAPIRoleReturnsServeError(t *testing.T) {
	role := NewAPIRole("unused", "test")
	role.listenerFactory = func(string) (net.Listener, error) {
		return rejectingListener{err: errors.New("forced accept failure")}, nil
	}
	err := role.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "forced accept failure") {
		t.Fatalf("Run() error = %v, want listener accept failure", err)
	}
}

type stubSessions struct{}

func (stubSessions) Issue(context.Context, int64, string, string) (string, domain.Session, string, error) {
	return "", domain.Session{}, "", errors.New("sessions are not available")
}

func (stubSessions) Authenticate(context.Context, string) (identityapp.Authenticated, error) {
	return identityapp.Authenticated{}, identityapp.ErrUnauthenticated
}

func (stubSessions) Revoke(context.Context, string) error { return nil }

func (stubSessions) RevokeOthers(context.Context, int64, []byte) error      { return nil }
func (stubSessions) List(context.Context, int64) ([]domain.Session, error)  { return nil, nil }
func (stubSessions) RevokeByID(context.Context, int64, int64) (bool, error) { return false, nil }

func TestBuildKeyHandlerRejectsInvalidConfiguration(t *testing.T) {
	if _, err := buildKeyHandler(&pgxpool.Pool{}, config.Config{}); err == nil || !strings.Contains(err.Error(), "master key") {
		t.Fatalf("empty config error = %v", err)
	}
	if _, err := buildKeyHandler(&pgxpool.Pool{}, config.Config{Crypto: config.CryptoConfig{MasterKeys: "active:not-base64", ActiveMasterKeyID: "active"}}); err == nil {
		t.Fatal("invalid master key was accepted")
	}
	encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))
	cfg := config.Config{Crypto: config.CryptoConfig{MasterKeys: "test:" + encoded, ActiveMasterKeyID: "other"}}
	if _, err := buildKeyHandler(&pgxpool.Pool{}, cfg); err == nil || strings.Contains(err.Error(), encoded) {
		t.Fatalf("active key error = %v", err)
	}
	if _, err := buildKeyHandler(nil, config.Config{Crypto: config.CryptoConfig{MasterKeys: "test:" + encoded, ActiveMasterKeyID: "test"}}); err == nil || strings.Contains(err.Error(), encoded) {
		t.Fatalf("nil database error = %v", err)
	}
}

func TestBuildKeyHandlerWiresEncryptionAndPrincipal(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))
	handler, err := buildKeyHandler(&pgxpool.Pool{}, config.Config{Crypto: config.CryptoConfig{MasterKeys: "test:" + encoded, ActiveMasterKeyID: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	service, ok := handler.Keys.(accountsapp.Keys)
	if !ok || service.Repository == nil || service.Probe == nil || service.Sealer == nil {
		t.Fatal("key service is incomplete")
	}
	reader, ok := service.Settings.(accountsettings.Reader)
	if !ok || reader.Settings == nil {
		t.Fatal("administrator caps are not wired")
	}
	ctx := identityhttp.WithPrincipal(t.Context(), identityhttp.Principal{User: domain.User{ID: 4}})
	if id, ok := handler.Principal(ctx); !ok || id != 4 {
		t.Fatalf("principal = %d %v", id, ok)
	}
	if _, ok := handler.Principal(t.Context()); ok {
		t.Fatal("missing principal was accepted")
	}
	if _, ok := handler.Principal(identityhttp.WithPrincipal(t.Context(), identityhttp.Principal{})); ok {
		t.Fatal("zero user was accepted")
	}
}

func TestAPIRouterRequiresSessionAndCSRF(t *testing.T) {
	router := newAPIRouter(identityhttp.Handler{Version: "router-test", Sessions: stubSessions{}}, accountshttp.KeyHandler{}, accountshttp.ConnectHandler{}, schedulehttp.Handler{})
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

	keys := httptest.NewRequest(http.MethodGet, "/keys", nil)
	keysResponse := httptest.NewRecorder()
	router.ServeHTTP(keysResponse, keys)
	if keysResponse.Code != http.StatusUnauthorized || !strings.Contains(keysResponse.Body.String(), `"code":"unauthenticated"`) {
		t.Fatalf("keys status = %d body = %s", keysResponse.Code, keysResponse.Body.String())
	}

	create := httptest.NewRequest(http.MethodPost, "/keys", strings.NewReader(`{"label":"Personal","key":"0123456789abcdef0123456789abcdef","secret":"not-logged"}`))
	create.Header.Set("Content-Type", "application/json")
	createResponse := httptest.NewRecorder()
	router.ServeHTTP(createResponse, create)
	if createResponse.Code != http.StatusForbidden || !strings.Contains(createResponse.Body.String(), `"code":"csrf_rejected"`) || strings.Contains(createResponse.Body.String(), "not-logged") {
		t.Fatalf("create status = %d body = %s", createResponse.Code, createResponse.Body.String())
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

func TestBuildConnectHandlerRejectsInvalidConfiguration(t *testing.T) {
	if _, err := buildConnectHandler(&pgxpool.Pool{}, nil, config.Config{}); err == nil || !strings.Contains(err.Error(), "master key") {
		t.Fatalf("empty config error = %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))
	if _, err := buildConnectHandler(nil, nil, config.Config{Crypto: config.CryptoConfig{MasterKeys: "test:" + encoded, ActiveMasterKeyID: "test"}}); err == nil || strings.Contains(err.Error(), encoded) {
		t.Fatalf("nil database error = %v", err)
	}
}

func TestBuildConnectHandlerWiresTheFlowAndPrincipal(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))
	cfg := config.Config{
		Crypto: config.CryptoConfig{MasterKeys: "test:" + encoded, ActiveMasterKeyID: "test"},
		Redis:  config.RedisConfig{Prefix: "xflickr_wt98:"},
		Flickr: config.FlickrConfig{CallbackURL: "http://127.0.0.1:8080/flickr-accounts/callback"},
	}
	handler, err := buildConnectHandler(&pgxpool.Pool{}, redisv9.NewClient(&redisv9.Options{Addr: "127.0.0.1:0"}), cfg)
	if err != nil {
		t.Fatal(err)
	}
	service, ok := handler.Accounts.(accountsapp.Connect)
	if !ok || service.Repository == nil || service.Authorizer == nil || service.States == nil || service.Cipher == nil {
		t.Fatal("connect service is incomplete")
	}
	if service.CallbackURL != cfg.Flickr.CallbackURL {
		t.Fatalf("callback = %q", service.CallbackURL)
	}
	ctx := identityhttp.WithPrincipal(t.Context(), identityhttp.Principal{User: domain.User{ID: 4}})
	if id, ok := handler.Principal(ctx); !ok || id != 4 {
		t.Fatalf("principal = %d %v", id, ok)
	}
}
