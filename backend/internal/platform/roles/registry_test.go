package roles

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	redisv9 "github.com/redis/go-redis/v9"

	accountshttp "github.com/jooservices/xflickr/internal/accounts/adapters/http"
	accountsettings "github.com/jooservices/xflickr/internal/accounts/adapters/settings"
	accountsapp "github.com/jooservices/xflickr/internal/accounts/app"
	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	identityredis "github.com/jooservices/xflickr/internal/identity/adapters/redis"
	identityapp "github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
	mediahttp "github.com/jooservices/xflickr/internal/media/adapters/http"
	mediaapp "github.com/jooservices/xflickr/internal/media/app"
	mediadomain "github.com/jooservices/xflickr/internal/media/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/config"
	quotaapp "github.com/jooservices/xflickr/internal/quota/app"
	quotadomain "github.com/jooservices/xflickr/internal/quota/domain"
	schedulehttp "github.com/jooservices/xflickr/internal/schedule/adapters/http"
	scheduleapp "github.com/jooservices/xflickr/internal/schedule/app"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
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

func TestBuildCrawlHandler(t *testing.T) {
	if _, err := buildCrawlHandler(nil); err == nil {
		t.Fatal("buildCrawlHandler accepted a nil database")
	}
	handler, err := buildCrawlHandler(&pgxpool.Pool{})
	if err != nil {
		t.Fatal(err)
	}
	if handler.Controls == nil || handler.ActorID == nil {
		t.Fatal("crawl handler dependencies are missing")
	}
}

func TestFeatureHandlerBuildersRequireDatabase(t *testing.T) {
	client := redisv9.NewClient(&redisv9.Options{Addr: "127.0.0.1:1"})
	defer client.Close()
	if _, err := buildKeyHandler(nil, config.Config{}); err == nil {
		t.Fatal("key handler accepted a nil database")
	}
	if _, err := buildConnectHandler(nil, client, config.Config{}); err == nil {
		t.Fatal("connect handler accepted a nil database")
	}
}

func TestAPIRoleReturnsListenerError(t *testing.T) {
	want := errors.New("listen failed")
	role := NewAPIRole(":0", "test")
	role.listenerFactory = func(string) (net.Listener, error) { return nil, want }
	if err := role.Run(context.Background()); !errors.Is(err, want) {
		t.Fatalf("APIRole.Run() error = %v", err)
	}
}

func TestBuildConfiguredAPIHandler(t *testing.T) {
	client := redisv9.NewClient(&redisv9.Options{Addr: "127.0.0.1:1"})
	defer client.Close()
	if _, err := buildConfiguredAPIHandler(nil, client, config.Config{}, "test"); err == nil {
		t.Fatal("handler builder accepted a nil database")
	}
	if _, err := buildConfiguredAPIHandler(&pgxpool.Pool{}, client, config.Config{Identity: config.IdentityConfig{Argon2Memory: 32 * 1024, Argon2Iterations: 1, Argon2Parallelism: 1, Argon2SaltLength: 16, Argon2KeyLength: 32}}, "test"); err == nil {
		t.Fatal("handler builder accepted missing keyring configuration")
	}
	encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	cfg := config.Config{
		Crypto:   config.CryptoConfig{MasterKeys: "test:" + encoded, ActiveMasterKeyID: "test"},
		Identity: config.IdentityConfig{Argon2Memory: 32 * 1024, Argon2Iterations: 1, Argon2Parallelism: 1, Argon2SaltLength: 16, Argon2KeyLength: 32},
		Redis:    config.RedisConfig{Prefix: "handler-test:"},
		Blob:     config.BlobConfig{Driver: "local", LocalDir: t.TempDir()},
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

func TestCrawlRoleStopsForCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (crawlRole{}).Run(ctx); err != nil {
		t.Fatalf("crawlRole.Run() error = %v", err)
	}
}

func TestCrawlRoleReturnsConfigurationError(t *testing.T) {
	t.Setenv("DB_HOST", "")
	if err := (crawlRole{}).Run(context.Background()); err == nil {
		t.Fatal("crawl role started with missing database configuration")
	}
}

func TestSyncRoleStopsForCanceledContextAndReportsConfigurationErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (syncRole{}).Run(ctx); err != nil {
		t.Fatalf("syncRole.Run(canceled) error = %v", err)
	}
	t.Setenv("DB_HOST", "")
	if err := (syncRole{}).Run(context.Background()); err == nil {
		t.Fatal("sync role started with missing database configuration")
	}
}

func TestSyncRoleReturnsDatabaseConnectionError(t *testing.T) {
	for key, value := range map[string]string{
		"DB_HOST": "127.0.0.1", "DB_PORT": "1", "DB_NAME": "xflickr_wt_sync_unit", "DB_USER": "unused",
		"REDIS_PREFIX": "sync_unit:", "OPENSEARCH_INDEX_PREFIX": "sync_unit_", "NODE_ID": "sync-unit-test",
	} {
		t.Setenv(key, value)
	}
	if err := (syncRole{}).Run(context.Background()); err == nil {
		t.Fatal("sync role started without a reachable database")
	}
}

type photoSizesQueryPool struct{ row pgx.Row }

func (p photoSizesQueryPool) QueryRow(context.Context, string, ...any) pgx.Row { return p.row }

type photoSizesQueryRow struct {
	key, status string
	err         error
}

func (r photoSizesQueryRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*string), *dest[1].(*string) = r.key, r.status
	return nil
}

type photoSizesTestPermit struct {
	meta quotadomain.Meta
	err  error
}

func (p *photoSizesTestPermit) Done(meta quotadomain.Meta) error { p.meta = meta; return p.err }

type photoSizesTestGovernor struct {
	permit quotadomain.Permit
	err    error
	class  quotadomain.Class
	wait   time.Duration
}

func (g *photoSizesTestGovernor) Acquire(_ context.Context, _ int64, class quotadomain.Class) (quotadomain.Permit, time.Duration, error) {
	g.class = class
	return g.permit, g.wait, g.err
}

func TestPhotoSizesClientUsesValidKeyAndPhotoQuota(t *testing.T) {
	pool := photoSizesQueryPool{row: photoSizesQueryRow{key: "public-key", status: "revoked"}}
	governor := &photoSizesTestGovernor{}
	if _, err := (photoSizesClient{Pool: pool, Client: flickrclient.NewClient("", nil), Quota: governor}).GetSizes(t.Context(), 4, "9"); err == nil {
		t.Fatal("revoked key was used")
	}
	permit := &photoSizesTestPermit{}
	governor.permit = permit
	pool.row = photoSizesQueryRow{key: "public-key", status: "valid"}
	var gotMethod, gotPhoto string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse Flickr form: %v", err)
		}
		gotMethod, gotPhoto = r.Form.Get("method"), r.Form.Get("photo_id")
		_, _ = w.Write([]byte(`{"sizes":{"size":[{"label":"Original","width":100,"height":80,"source":"https://example.test/photo","media":"photo"}]}}`))
	}))
	defer server.Close()
	client := photoSizesClient{Pool: pool, Client: flickrclient.NewClient(server.URL, server.Client()), Quota: governor}
	if _, err := client.GetSizesClass(t.Context(), 4, "9", "unknown"); !errors.Is(err, quotadomain.ErrInvalidClass) {
		t.Fatalf("invalid quota class error = %v", err)
	}
	sizes, err := client.GetSizesClass(t.Context(), 4, "9", quotadomain.Interactive)
	if err != nil || len(sizes) != 1 || gotMethod != "flickr.photos.getSizes" || gotPhoto != "9" || governor.class != quotadomain.Interactive || permit.meta.Err != nil {
		t.Fatalf("GetSizes() = %#v, %v; method=%q photo=%q class=%q meta=%+v", sizes, err, gotMethod, gotPhoto, governor.class, permit.meta)
	}
}

func TestPhotoSizesClientReportsDependencyQuotaAndCallFailures(t *testing.T) {
	if _, err := (photoSizesClient{}).GetSizesClass(t.Context(), 4, "9", quotadomain.Photo); err == nil {
		t.Fatal("missing dependencies were accepted")
	}
	pool := photoSizesQueryPool{row: photoSizesQueryRow{key: "public-key", status: "valid"}}
	boom := errors.New("quota unavailable")
	governor := &photoSizesTestGovernor{err: boom}
	client := photoSizesClient{Pool: pool, Client: flickrclient.NewClient("http://127.0.0.1:1", nil), Quota: governor}
	if _, err := client.GetSizesClass(t.Context(), 4, "9", quotadomain.Photo); !errors.Is(err, boom) {
		t.Fatalf("quota error=%v", err)
	}
	governor.err = nil
	governor.wait = time.Second
	if _, err := client.GetSizesClass(t.Context(), 4, "9", quotadomain.Photo); err == nil {
		t.Fatal("quota wait without permit was accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	defer server.Close()
	permit := &photoSizesTestPermit{err: boom}
	governor.permit = permit
	client.Client = flickrclient.NewClient(server.URL, server.Client())
	if _, err := client.GetSizesClass(t.Context(), 4, "9", quotadomain.Photo); !errors.Is(err, boom) {
		t.Fatalf("permit release error=%v", err)
	}
	permit.err = nil
	if _, err := client.GetSizesClass(t.Context(), 4, "9", quotadomain.Photo); err == nil || permit.meta.Err == nil {
		t.Fatalf("Flickr error=%v permit metadata=%+v", err, permit.meta)
	}
}

type rolesDownloadService struct{}

func (rolesDownloadService) Create(_ context.Context, input mediaapp.CreateDownloadInput) (mediadomain.Download, error) {
	return mediadomain.Download{ID: 4, UserID: input.UserID, Kind: input.Kind, Subject: "album", Status: "pending", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func TestAPIServerForwardsDownloadRequest(t *testing.T) {
	handler := mediahttp.DownloadHandler{
		Downloads: rolesDownloadService{},
		Principal: func(context.Context) (int64, bool) { return 7, true },
	}
	server := apiServer{Downloads: handler}
	subject := "album-1"
	response, err := server.CreateDownload(t.Context(), gen.CreateDownloadRequestObject{Body: &gen.CreateDownloadRequest{Kind: gen.CreateDownloadRequestKindAlbum, Subject: &subject}})
	if err != nil {
		t.Fatal(err)
	}
	created, ok := response.(gen.CreateDownload201JSONResponse)
	if !ok || created.ID != 4 {
		t.Fatalf("CreateDownload() = %#v", response)
	}
}

func TestBuildDownloadHandlerRejectsMissingPool(t *testing.T) {
	if _, err := buildDownloadHandler(nil); err == nil {
		t.Fatal("accepted missing database pool")
	}
}

func TestCrawlAndSchedulerRolesReturnDatabaseErrors(t *testing.T) {
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
	if err := (crawlRole{}).Run(context.Background()); err == nil {
		t.Fatal("crawl role started without a reachable database")
	}
	if err := (schedulerRole{}).Run(context.Background()); err == nil {
		t.Fatal("scheduler role started without a reachable database")
	}
}

func TestFeatureHandlerBuildersReturnKeyringErrors(t *testing.T) {
	if _, err := buildKeyHandler(&pgxpool.Pool{}, config.Config{}); err == nil {
		t.Fatal("key handler accepted missing keyring configuration")
	}
	if _, err := buildConnectHandler(&pgxpool.Pool{}, nil, config.Config{}); err == nil {
		t.Fatal("connect handler accepted missing keyring configuration")
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

func TestNewSchedulerPumpWiresQueueStatePublisher(t *testing.T) {
	client := redisv9.NewClient(&redisv9.Options{Addr: "127.0.0.1:1"})
	defer client.Close()
	settings := scheduleapp.PumpSettings{RetryMax: 4}
	quota := newQuotaRuntime(&pgxpool.Pool{}, client, config.Config{})
	pump, err := newSchedulerPump(&pgxpool.Pool{}, quota.Governor, nil, nil, settings)
	if err != nil {
		t.Fatalf("newSchedulerPump() error = %v", err)
	}
	if pump.QueueState == nil || pump.ChangeChecks == nil || pump.PhotoLane == nil || pump.Settings.RetryMax != settings.RetryMax {
		t.Fatalf("scheduler pump wiring = %+v", pump)
	}
}

func TestNewQuotaRuntimeWiresRebuildFlushAndCallLog(t *testing.T) {
	client := redisv9.NewClient(&redisv9.Options{Addr: "127.0.0.1:1"})
	defer client.Close()
	quota := newQuotaRuntime(&pgxpool.Pool{}, client, config.Config{Redis: config.RedisConfig{Prefix: "wt:"}})
	governor := quota.Governor
	if governor.Rebuilder == nil || governor.Recorder == nil || governor.Budgets == nil || governor.Prefix != "wt:" {
		t.Fatalf("governor wiring = %+v", governor)
	}
	if governor.Rebuilder != any(quota.Rebuilder) {
		t.Fatal("governor and start rebuild use different rebuilders")
	}
	f := quota.Flusher
	if f.Store == nil || f.Counters == nil || f.Pruner == nil || quota.Rebuilder.Store == nil || quota.Rebuilder.Calls == nil || quota.Rebuilder.Counters == nil {
		t.Fatalf("flusher or rebuilder wiring is incomplete: %+v", quota)
	}
}

func TestQuotaRuntimeStartRebuildsFlushesAndStops(t *testing.T) {
	store := &startStore{rebuilt: make(chan struct{}, 1), flushed: make(chan struct{}, 8)}
	quota := quotaRuntime{
		Rebuilder: &quotaapp.Rebuilder{Store: store, Calls: startCalls{}, Counters: startCounters{}},
		Flusher:   quotaapp.Flusher{Store: store, Counters: startCounters{}, Interval: 5 * time.Millisecond},
	}
	stop := quota.start(context.Background(), nil)
	select {
	case <-store.rebuilt:
	default:
		t.Fatal("start did not rebuild counters before returning")
	}
	select {
	case <-store.flushed:
	case <-time.After(2 * time.Second):
		t.Fatal("flusher did not run in the background")
	}
	stop()
	stop()
}

func TestQuotaRuntimeStartLogsRebuildFailureAndKeepsFlushing(t *testing.T) {
	store := &startStore{rebuilt: make(chan struct{}, 1), flushed: make(chan struct{}, 8), listErr: errors.New("down")}
	quota := quotaRuntime{
		Rebuilder: &quotaapp.Rebuilder{Store: store, Calls: startCalls{}, Counters: startCounters{}},
		Flusher:   quotaapp.Flusher{Store: store, Counters: startCounters{}, Interval: 5 * time.Millisecond},
	}
	var logs bytes.Buffer
	stop := quota.start(context.Background(), slog.New(slog.NewTextHandler(&logs, nil)))
	time.Sleep(50 * time.Millisecond)
	stop()
	if !strings.Contains(logs.String(), "rebuild at start failed") || !strings.Contains(logs.String(), "flush failed") {
		t.Fatalf("logs = %q", logs.String())
	}
}

type startStore struct {
	rebuilt, flushed chan struct{}
	listErr          error
}

func (s *startStore) Save(context.Context, quotaapp.Usage) error { return nil }
func (s *startStore) Latest(context.Context, int64, time.Time) (quotaapp.Usage, bool, error) {
	select {
	case s.rebuilt <- struct{}{}:
	default:
	}
	return quotaapp.Usage{}, false, nil
}

func (s *startStore) KeyIDs(context.Context) ([]int64, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	select {
	case s.flushed <- struct{}{}:
	default:
	}
	return []int64{1}, nil
}

type startCalls struct{}

func (startCalls) Usage(context.Context, int64, time.Time) (quotaapp.CallUsage, error) {
	return quotaapp.CallUsage{}, nil
}

type startCounters struct{}

func (startCounters) Snapshot(context.Context, int64) (quotaapp.Usage, bool, error) {
	return quotaapp.Usage{}, false, nil
}
func (startCounters) Restore(context.Context, quotaapp.Usage) (bool, error) { return true, nil }

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
