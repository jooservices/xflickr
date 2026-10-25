package http_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/joho/godotenv"
	redisv9 "github.com/redis/go-redis/v9"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	quotahttp "github.com/jooservices/xflickr/internal/quota/adapters/http"
	quotapostgres "github.com/jooservices/xflickr/internal/quota/adapters/postgres"
	quotaredis "github.com/jooservices/xflickr/internal/quota/adapters/redis"
	"github.com/jooservices/xflickr/internal/quota/app"
	"github.com/jooservices/xflickr/internal/quota/domain"
)

func TestQuotaEndpointsAfterCallsIntegration(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	pool := dbtest.Open(t, cfg)
	values, err := godotenv.Read(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal("read shared worktree environment")
	}
	prefix := values["REDIS_PREFIX"]
	if prefix == "" {
		t.Fatal("REDIS_PREFIX is required so the test stays inside the worktree namespace")
	}
	port, _ := strconv.Atoi(values["REDIS_PORT"])
	if port == 0 {
		port = 16382
	}
	host := values["REDIS_HOST"]
	if host == "" {
		host = "127.0.0.1"
	}
	db, _ := strconv.Atoi(values["REDIS_DB"])
	client := redisv9.NewClient(&redisv9.Options{Addr: net.JoinHostPort(host, strconv.Itoa(port)), Password: values["REDIS_PASSWORD"], DB: db})
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping shared Redis: %v", err)
	}

	suffix := time.Now().UnixNano()
	var ownerID, strangerID int64
	for i, id := range []*int64{&ownerID, &strangerID} {
		email := fmt.Sprintf("jooxf110-%d-%d@example.invalid", i, suffix)
		if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, role, locale) VALUES ($1, 'integration-hash', 'user', 'en') RETURNING id`, email).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1)`, []int64{ownerID, strangerID})
	})
	insertKey := func(user int64, label string, hourly int) int64 {
		var id int64
		err := pool.QueryRow(ctx, `INSERT INTO api_keys (user_id, label, key_public, secret_sealed, per_hour_limit, per_second_limit, burst) VALUES ($1, $2, $3, '{}'::jsonb, $4, 100, 100) RETURNING id`, user, label, fmt.Sprintf("k110-%s-%d", label, suffix), hourly).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	keyID := insertKey(ownerID, "mine", 3600)
	otherKeyID := insertKey(strangerID, "theirs", 3600)
	prefix = fmt.Sprintf("%sjooxf110:%d:", prefix, suffix)
	clearKeys := func() {
		iter := client.Scan(context.Background(), 0, prefix+"*", 100).Iterator()
		for iter.Next(context.Background()) {
			client.Del(context.Background(), iter.Val())
		}
	}
	clearKeys()
	t.Cleanup(clearKeys)

	budgets := quotapostgres.Budgets{Pool: pool}
	governor := quotaredis.Governor{Client: client, Prefix: prefix, Budgets: budgets}
	for _, class := range []domain.Class{domain.Interactive, domain.Person, domain.Person, domain.Photo} {
		if permit, _, err := governor.Acquire(ctx, keyID, class); err != nil || permit == nil {
			t.Fatalf("acquire %s = %v %v", class, permit, err)
		}
	}

	keys := quotapostgres.Keys{Pool: pool}
	states := app.States{Keys: keys, Budgets: budgets, Live: quotaredis.Counters{Client: client, Prefix: prefix}, History: keys}
	current := ownerID
	handler := quotahttp.QuotaHandler{States: states, Node: "node-it", ActorID: func(context.Context) (int64, bool) { return current, true }}
	router := gen.HandlerFromMux(quotaEndpointServer{handler: handler}, chi.NewRouter())

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/keys/%d/quota", keyID), nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /keys/{id}/quota status=%d body=%s", response.Code, response.Body.String())
	}
	var quota gen.KeyQuota
	if err := json.NewDecoder(response.Body).Decode(&quota); err != nil {
		t.Fatal(err)
	}
	if quota.KeyID != keyID || quota.Used != 4 || quota.Limit != 3600 || quota.State != gen.KeyQuotaStateOk ||
		quota.Lanes != (gen.LaneUsage{Interactive: 1, Person: 2, Photo: 1}) || quota.ResetAt == nil || time.Until(*quota.ResetAt) < 55*time.Minute {
		t.Fatalf("quota = %+v", quota)
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/keys/%d/quota", otherKeyID), nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("other user's key status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin/quota", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /admin/quota status=%d body=%s", response.Code, response.Body.String())
	}
	var admin gen.AdminQuota
	if err := json.NewDecoder(response.Body).Decode(&admin); err != nil {
		t.Fatal(err)
	}
	var mine *gen.AdminKeyQuota
	for i := range admin.Keys {
		if admin.Keys[i].KeyID == keyID {
			mine = &admin.Keys[i]
		}
	}
	if mine == nil || mine.Used != 4 || mine.Limit != 3600 || mine.Node != "node-it" || mine.State != gen.AdminKeyQuotaStateOk || mine.Trend != gen.Rising || mine.Label != "mine" {
		t.Fatalf("admin keys = %+v", admin.Keys)
	}
}

type quotaEndpointServer struct {
	gen.Unimplemented
	handler quotahttp.QuotaHandler
}

func (s quotaEndpointServer) GetKeyQuota(w http.ResponseWriter, r *http.Request, id int64) {
	response, err := s.handler.GetKeyQuota(r.Context(), gen.GetKeyQuotaRequestObject{ID: id})
	if err != nil {
		http.Error(w, "quota request failed", http.StatusInternalServerError)
		return
	}
	_ = response.VisitGetKeyQuotaResponse(w)
}

func (s quotaEndpointServer) GetAdminQuota(w http.ResponseWriter, r *http.Request) {
	response, err := s.handler.GetAdminQuota(r.Context(), gen.GetAdminQuotaRequestObject{})
	if err != nil {
		http.Error(w, "quota request failed", http.StatusInternalServerError)
		return
	}
	_ = response.VisitGetAdminQuotaResponse(w)
}
