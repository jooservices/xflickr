package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	"github.com/jooservices/xflickr/internal/identity/adapters/postgres"
	"github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestSessionStoreSlidingExpiryAndCSRF(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool := openSessionPool(t)
	repository := postgres.New(pool)
	owner := insertSessionUser(t, ctx, pool, repository, "owner")
	other := insertSessionUser(t, ctx, pool, repository, "other")
	service := app.Sessions{Repository: repository, Users: repository}

	var columns []string
	rows, err := pool.Query(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'sessions' ORDER BY ordinal_position`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	wantColumns := []string{"id", "user_id", "token_hash", "expires_at", "last_seen_at", "created_at"}
	if !slices.Equal(columns, wantColumns) {
		t.Fatalf("sessions columns = %v", columns)
	}

	raw, session, csrf, err := service.Issue(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	otherRaw, _, _, err := service.Issue(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	foreignRaw, _, _, err := service.Issue(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	var stored []byte
	if err := pool.QueryRow(ctx, `SELECT token_hash FROM sessions WHERE id = $1`, session.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, domain.HashSessionToken(raw)) || bytes.Contains(stored, []byte(raw)) {
		t.Fatal("session row does not store the SHA-256 hash of the opaque id")
	}
	if _, err := repository.InsertSession(ctx, owner.ID, bytes.Repeat([]byte{1}, 16), time.Now().Add(time.Hour), time.Now()); err == nil {
		t.Fatal("short token hash was stored")
	}

	updated, err := repository.TouchSession(ctx, domain.HashSessionToken(raw), time.Now().Add(domain.SessionLifetime), time.Now().Add(time.Second))
	if err != nil || updated {
		t.Fatalf("touch inside the minute updated=%v err=%v", updated, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET last_seen_at = $2 WHERE id = $1`, session.ID, time.Now().Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	authenticated, err := service.Authenticate(ctx, raw)
	if err != nil || !authenticated.Slid || authenticated.User.ID != owner.ID {
		t.Fatalf("authenticate = %+v, %v", authenticated, err)
	}
	again, err := service.Authenticate(ctx, raw)
	if err != nil || again.Slid {
		t.Fatalf("second authenticate = %+v, %v", again, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET expires_at = $2 WHERE id = $1`, session.ID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.FindSessionByHash(ctx, domain.HashSessionToken(raw), time.Now()); !errors.Is(err, app.ErrUnauthenticated) {
		t.Fatalf("expired session error = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET expires_at = $2, last_seen_at = $3 WHERE id = $1`, session.ID, time.Now().Add(domain.SessionLifetime), time.Now()); err != nil {
		t.Fatal(err)
	}

	router := chi.NewRouter()
	router.Use(identityhttp.CSRF)
	router.Use(identityhttp.ProtectRoutes(service))
	server := httptest.NewServer(gen.HandlerFromMux(gen.NewStrictHandler(identityhttp.StrictHandler{Handler: identityhttp.Handler{Sessions: service, Users: repository, Version: "test"}}, nil), router))
	t.Cleanup(server.Close)

	health, err := server.Client().Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	healthBody, _ := io.ReadAll(health.Body)
	health.Body.Close()
	if health.StatusCode != http.StatusOK || !bytes.Contains(healthBody, []byte(`"version":"test"`)) {
		t.Fatalf("health status=%d body=%s", health.StatusCode, healthBody)
	}

	missing := deleteSessions(t, server, raw, "", "")
	if missing.StatusCode != http.StatusForbidden || errorCode(t, missing) != "csrf_rejected" {
		t.Fatalf("missing csrf status=%d", missing.StatusCode)
	}
	mismatched, err := domain.NewOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	rejected := deleteSessions(t, server, raw, csrf, mismatched)
	if rejected.StatusCode != http.StatusForbidden || errorCode(t, rejected) != "csrf_rejected" {
		t.Fatalf("mismatched csrf status=%d", rejected.StatusCode)
	}
	accepted := deleteSessions(t, server, raw, csrf, csrf)
	if accepted.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(accepted.Body)
		accepted.Body.Close()
		t.Fatalf("accepted csrf status=%d body=%s", accepted.StatusCode, body)
	}
	accepted.Body.Close()
	if _, err := repository.FindSessionByHash(ctx, domain.HashSessionToken(raw), time.Now()); err != nil {
		t.Fatalf("current session error = %v", err)
	}
	if _, err := repository.FindSessionByHash(ctx, domain.HashSessionToken(otherRaw), time.Now()); !errors.Is(err, app.ErrUnauthenticated) {
		t.Fatalf("other session error = %v", err)
	}
	if _, err := repository.FindSessionByHash(ctx, domain.HashSessionToken(foreignRaw), time.Now()); err != nil {
		t.Fatalf("foreign session error = %v", err)
	}
	if err := repository.DeleteSessionByHash(ctx, domain.HashSessionToken(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.FindSessionByHash(ctx, domain.HashSessionToken(raw), time.Now()); !errors.Is(err, app.ErrUnauthenticated) {
		t.Fatalf("revoked session error = %v", err)
	}

	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err := repository.InsertSession(canceled, owner.ID, domain.HashSessionToken(raw), time.Now().Add(time.Hour), time.Now()); err == nil {
		t.Fatal("insert succeeded with a canceled context")
	}
	if _, err := repository.FindSessionByHash(canceled, domain.HashSessionToken(foreignRaw), time.Now()); err == nil {
		t.Fatal("find succeeded with a canceled context")
	}
	if _, err := repository.TouchSession(canceled, domain.HashSessionToken(foreignRaw), time.Now().Add(time.Hour), time.Now()); err == nil {
		t.Fatal("touch succeeded with a canceled context")
	}
	if err := repository.DeleteOtherSessions(canceled, owner.ID, domain.HashSessionToken(foreignRaw)); err == nil {
		t.Fatal("delete others succeeded with a canceled context")
	}
	if err := repository.DeleteSessionByHash(canceled, domain.HashSessionToken(foreignRaw)); err == nil {
		t.Fatal("delete succeeded with a canceled context")
	}
	if _, err := repository.FindByID(ctx, owner.ID+9_000_000_000); !errors.Is(err, app.ErrUserNotFound) {
		t.Fatalf("missing user error = %v", err)
	}
}

func openSessionPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../"))
	dbConfig, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return dbtest.Open(t, dbConfig)
}

func insertSessionUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repository *postgres.Repository, label string) domain.User {
	t.Helper()
	email := fmt.Sprintf("jooxf94-%s-%d@example.invalid", label, time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)
	})
	params := domain.Argon2Params{Memory: 32 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	register := app.Register{Repository: repository, Policy: integrationRegistrationPolicy{}, Params: params}
	user, err := register.Execute(ctx, email, "integration-password", "")
	if err != nil {
		t.Fatal(err)
	}
	return user
}

func deleteSessions(t *testing.T, server *httptest.Server, raw, csrf, header string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodDelete, server.URL+"/auth/sessions", nil)
	if err != nil {
		t.Fatal(err)
	}
	if raw != "" {
		request.AddCookie(&http.Cookie{Name: identityhttp.SessionCookieName, Value: raw})
	}
	if csrf != "" {
		request.AddCookie(&http.Cookie{Name: identityhttp.CSRFCookieName, Value: csrf})
	}
	if header != "" {
		request.Header.Set(identityhttp.CSRFHeaderName, header)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func errorCode(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return payload.Code
}
