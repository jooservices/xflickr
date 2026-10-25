package roles

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	redisv9 "github.com/redis/go-redis/v9"

	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	"github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

// TestRoutesWithSessionIntegration logs an admin in through the real router
// and checks that every declared route answers its own status, never 401/403,
// with the session cookie and 401 without it.
func TestRoutesWithSessionIntegration(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	_, source, _, _ := runtime.Caller(0)
	envFile := filepath.Join(filepath.Clean(filepath.Join(filepath.Dir(source), "../../../..")), ".env")
	env, err := godotenv.Read(envFile)
	if err != nil {
		t.Fatal(err)
	}
	dbCfg, err := config.LoadDatabaseWithDotEnv(envFile)
	if err != nil {
		t.Fatal(err)
	}
	pool := dbtest.Open(t, dbCfg)
	params := domain.Argon2Params{Memory: 32 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	const password = "route-wiring-secret"
	hash, err := domain.HashPassword(password, params)
	if err != nil {
		t.Fatal(err)
	}
	email := fmt.Sprintf("route-wiring-%d@example.invalid", time.Now().UnixNano())
	var userID int64
	ctx := context.Background()
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,$2,'admin') RETURNING id`, email, hash).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM sessions WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	redisPort, err := strconv.Atoi(env["REDIS_PORT"])
	if err != nil {
		t.Fatal(err)
	}
	redisDB, err := strconv.Atoi(env["REDIS_DB"])
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		DB:       dbCfg,
		Redis:    config.RedisConfig{Host: env["REDIS_HOST"], Port: redisPort, DB: redisDB, Password: env["REDIS_PASSWORD"], Prefix: env["REDIS_PREFIX"]},
		Crypto:   config.CryptoConfig{MasterKeys: "integration:" + base64.StdEncoding.EncodeToString(make([]byte, 32)), ActiveMasterKeyID: "integration"},
		Identity: config.IdentityConfig{Argon2Memory: params.Memory, Argon2Iterations: params.Iterations, Argon2Parallelism: params.Parallelism, Argon2SaltLength: params.SaltLength, Argon2KeyLength: params.KeyLength},
	}
	redisClient := redisv9.NewClient(&redisv9.Options{Addr: fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port), Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	t.Cleanup(func() { _ = redisClient.Close() })
	handler, err := buildConfiguredAPIHandler(pool, redisClient, cfg, "routes-integration")
	if err != nil {
		t.Fatal(err)
	}

	csrf := strings.Repeat("c", 43)
	send := func(method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: identityhttp.CSRFCookieName, Value: csrf})
		req.Header.Set(identityhttp.CSRFHeaderName, csrf)
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	login := send(http.MethodPost, "/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, password))
	if login.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	var session *http.Cookie
	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == identityhttp.SessionCookieName {
			session = cookie
		}
	}
	if session == nil {
		t.Fatal("login did not issue a session cookie")
	}

	keys := make([]string, 0, len(routeAccess))
	for key, access := range routeAccess {
		if access != identityhttp.AccessPublic {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		method, pattern := splitRoute(key)
		path := samplePath(pattern)
		if rec := send(method, path, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without session = %d, want 401", key, rec.Code)
		}
		rec := send(method, path, "", session)
		// Empty bodies and unknown ids answer their own domain statuses (400/404/409...).
		unknownID := strings.Contains(pattern, "{")
		if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden || rec.Code >= http.StatusInternalServerError ||
			rec.Code == http.StatusMethodNotAllowed || rec.Code == http.StatusNotFound && !unknownID {
			t.Errorf("%s with session = %d body=%s", key, rec.Code, bytes.TrimSpace(rec.Body.Bytes()))
		}
		t.Logf("%-34s %d", key, rec.Code)
	}

	unknown := send(http.MethodGet, "/keys/usage", "", session)
	if unknown.Code != http.StatusBadRequest || !strings.Contains(unknown.Body.String(), `"code":"invalid_request"`) {
		t.Fatalf("GET /keys/usage = %d %s", unknown.Code, unknown.Body.String())
	}
}
