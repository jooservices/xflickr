package roles

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	identitypostgres "github.com/jooservices/xflickr/internal/identity/adapters/postgres"
	identityapp "github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/platform/api/rest"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

// TestDiscoveryLinkJourneyIntegration uses the real HTTP stack and PostgreSQL
// sessions, then follows every advertised collection without a provider call.
func TestDiscoveryLinkJourneyIntegration(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("requires shared worktree PostgreSQL")
	}
	_, source, _, _ := runtime.Caller(0)
	dbCfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(filepath.Dir(source), "../../../../.env"))
	if err != nil {
		t.Fatal(err)
	}
	pool := dbtest.Open(t, dbCfg)
	cfg := config.Config{DB: dbCfg, Blob: config.BlobConfig{Driver: "local", LocalDir: t.TempDir()}, Crypto: config.CryptoConfig{MasterKeys: "integration:" + base64.StdEncoding.EncodeToString(make([]byte, 32)), ActiveMasterKeyID: "integration"}, Identity: config.IdentityConfig{Argon2Memory: 8192, Argon2Iterations: 1, Argon2Parallelism: 1, Argon2SaltLength: 16, Argon2KeyLength: 32}}
	handler, err := buildConfiguredAPIHandler(pool, nil, cfg, "discovery-integration")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()
	client.Timeout = 5 * time.Second
	repo := identitypostgres.New(pool)
	sessions := identityapp.Sessions{Repository: repo, Users: repo}
	for _, role := range []string{"anonymous", "user", "admin"} {
		t.Run(role, func(t *testing.T) {
			raw := ""
			if role != "anonymous" {
				var id int64
				if err := pool.QueryRow(context.Background(), `INSERT INTO users(email,password_hash,role) VALUES($1,'unused',$2) RETURNING id`, fmt.Sprintf("discovery-%s-%d@example.invalid", role, time.Now().UnixNano()), role).Scan(&id); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					_, _ = pool.Exec(context.Background(), `DELETE FROM sessions WHERE user_id=$1`, id)
					_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, id)
				})
				var err error
				raw, _, _, err = sessions.Issue(context.Background(), id, "discovery-test", "127.0.0.1")
				if err != nil {
					t.Fatal(err)
				}
			}
			get := func(path string) *http.Response {
				t.Helper()
				r, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
				if err != nil {
					t.Fatal(err)
				}
				if raw != "" {
					r.AddCookie(&http.Cookie{Name: "xf_session", Value: raw})
				}
				response, err := client.Do(r)
				if err != nil {
					t.Fatal(err)
				}
				return response
			}
			response := get("/api/v1")
			var entry struct {
				Data struct {
					Links rest.Links `json:"links"`
				} `json:"data"`
			}
			err := json.NewDecoder(response.Body).Decode(&entry)
			_ = response.Body.Close()
			if err != nil || response.StatusCode != 200 {
				t.Fatal(response.StatusCode, err)
			}
			if (entry.Data.Links["users"].Href != "") != (role == "admin") {
				t.Fatal("admin permission leak", entry)
			}
			for relation, link := range entry.Data.Links {
				response := get(link.Href)
				_ = response.Body.Close()
				if response.StatusCode != 200 {
					t.Errorf("%s relation %s returned %d", role, relation, response.StatusCode)
				}
			}
		})
	}
}
