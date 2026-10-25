package http_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	schedulehttp "github.com/jooservices/xflickr/internal/schedule/adapters/http"
	"github.com/jooservices/xflickr/internal/schedule/adapters/queuepostgres"
	"github.com/jooservices/xflickr/internal/schedule/app"
)

func TestQueueEndpointsIntegration(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	pool := dbtest.Open(t, cfg)
	suffix := fmt.Sprintf("queue-endpoint-%d", time.Now().UnixNano())
	var userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'test','user') RETURNING id`, suffix+"@example.invalid").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE subject_key LIKE $1`, suffix+"%")
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	for _, kind := range []string{"person", "photo_sizes"} {
		if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state) VALUES($1,$2,$3,'pending')`, kind, suffix+"-"+kind, userID); err != nil {
			t.Fatal(err)
		}
	}
	handler := schedulehttp.Handler{Queue: app.Queue{Store: queuepostgres.New(pool)}, ActorID: func(context.Context) (int64, bool) { return userID, true }}
	router := gen.HandlerFromMux(queueEndpointServer{handler: handler}, chi.NewRouter())

	userResponse := httptest.NewRecorder()
	router.ServeHTTP(userResponse, httptest.NewRequest(http.MethodGet, "/me/crawl/queue", nil))
	if userResponse.Code != http.StatusOK {
		t.Fatalf("GET /me/crawl/queue status=%d body=%s", userResponse.Code, userResponse.Body.String())
	}
	var userQueue gen.CrawlQueue
	if err := json.NewDecoder(userResponse.Body).Decode(&userQueue); err != nil {
		t.Fatal(err)
	}
	if len(userQueue.Lanes) != 2 || userQueue.Lanes[0].BacklogCalls != 1 || userQueue.Lanes[1].BacklogCalls != 1 {
		t.Fatalf("user queue=%+v", userQueue)
	}

	adminResponse := httptest.NewRecorder()
	router.ServeHTTP(adminResponse, httptest.NewRequest(http.MethodGet, "/admin/queue", nil))
	if adminResponse.Code != http.StatusOK {
		t.Fatalf("GET /admin/queue status=%d body=%s", adminResponse.Code, adminResponse.Body.String())
	}
	var adminQueue gen.AdminQueue
	if err := json.NewDecoder(adminResponse.Body).Decode(&adminQueue); err != nil {
		t.Fatal(err)
	}
	foundPlanLane := false
	for _, item := range adminQueue.BacklogByPlan {
		if item.BacklogCalls >= 1 && strings.Contains(item.Plan, "free") && (item.Lane == "person" || item.Lane == "sizes") {
			foundPlanLane = true
		}
	}
	if !foundPlanLane || adminQueue.OldestItemAgeSeconds < 0 {
		t.Fatalf("admin queue=%+v", adminQueue)
	}
}

type queueEndpointServer struct {
	gen.Unimplemented
	handler schedulehttp.Handler
}

func (s queueEndpointServer) GetMeCrawlQueue(w http.ResponseWriter, r *http.Request) {
	response, err := s.handler.GetMeCrawlQueue(r.Context(), gen.GetMeCrawlQueueRequestObject{})
	if err != nil {
		http.Error(w, "queue request failed", http.StatusInternalServerError)
		return
	}
	_ = response.VisitGetMeCrawlQueueResponse(w)
}

func (s queueEndpointServer) GetAdminQueue(w http.ResponseWriter, r *http.Request) {
	response, err := s.handler.GetAdminQueue(r.Context(), gen.GetAdminQueueRequestObject{})
	if err != nil {
		http.Error(w, "queue request failed", http.StatusInternalServerError)
		return
	}
	_ = response.VisitGetAdminQueueResponse(w)
}
