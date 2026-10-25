package roles

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	redisv9 "github.com/redis/go-redis/v9"

	accountsdomain "github.com/jooservices/xflickr/internal/accounts/domain"
	"github.com/jooservices/xflickr/internal/fakeflickr"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/platform/queue"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

func crawlIntegrationConfig(t *testing.T) config.Config {
	t.Helper()
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../.."))
	env, err := godotenv.Read(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	dbCfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"DB_HOST": dbCfg.Host, "DB_PORT": fmt.Sprint(dbCfg.Port), "DB_NAME": dbCfg.Name, "DB_USER": dbCfg.User, "DB_PASSWORD": dbCfg.Password,
		"REDIS_HOST": env["REDIS_HOST"], "REDIS_PORT": env["REDIS_PORT"], "REDIS_DB": env["REDIS_DB"], "REDIS_PASSWORD": env["REDIS_PASSWORD"], "REDIS_PREFIX": env["REDIS_PREFIX"],
		"OPENSEARCH_INDEX_PREFIX": "xflickr_wtk251_", "NODE_ID": "jooxf-251-integration",
		"XFLICKR_MASTER_KEYS": "integration:" + base64.StdEncoding.EncodeToString(make([]byte, 32)), "XFLICKR_MASTER_KEY_ACTIVE": "integration",
	} {
		t.Setenv(name, value)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestCrawlWorkerRunsSyncJobAgainstFakeFlickr proves the crawl worker consumes
// the sync River queue: an enqueued check item is run by the engine, calls the
// fake Flickr under the quota governor and finishes.
func TestCrawlWorkerRunsSyncJobAgainstFakeFlickr(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	cfg := crawlIntegrationConfig(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool := dbtest.Open(t, cfg.DB)
	redisClient := redisv9.NewClient(&redisv9.Options{Addr: fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port), Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	defer redisClient.Close()
	ring, err := keyringFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	suffix := fmt.Sprintf("crawl-%d", time.Now().UnixNano())
	person := "10000000@N00"
	var userID, keyID, accountID, itemID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'unused','user') RETURNING id`, suffix+"@example.test").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupCrawlFixture(pool, userID, suffix) })
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,'crawl test',$2,'{}',3600,10,10) RETURNING id`, userID, suffix).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	sealedSecret, err := ring.Seal([]byte("key-secret"), accountsdomain.SecretAAD(keyID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE api_keys SET secret_sealed=$2 WHERE id=$1`, keyID, sealedSecret); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed) VALUES($1,$2,$3,'owner','{}','{}') RETURNING id`, userID, keyID, person).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	sealedToken, err := ring.Seal([]byte("token"), accountsdomain.TokenAAD(accountID))
	if err != nil {
		t.Fatal(err)
	}
	sealedTokenSecret, err := ring.Seal([]byte("token-secret"), accountsdomain.TokenSecretAAD(accountID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE flickr_accounts SET token_sealed=$2, secret_sealed=$3 WHERE id=$1`, accountID, sealedToken, sealedTokenSecret); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via) VALUES($1,$2,0,'test')`, userID, person); err != nil {
		t.Fatal(err)
	}
	cursor, err := json.Marshal(map[string]any{"page": 1, "method": "flickr.people.getPhotos", "checkpoint_at": time.Now().Unix(), "params": map[string]string{
		"user_id": person, "min_upload_date": "0", "min_fave_date": "0", "per_page": "1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO work_items(kind,subject_key,user_id,key_id,state,cursor,lease_expires_at) VALUES('check',$1,$2,$3,'ready',$4,now()+interval '2 minutes') RETURNING id`, person, userID, keyID, cursor).Scan(&itemID); err != nil {
		t.Fatal(err)
	}

	dataset, err := fakeflickr.LoadDataset("")
	if err != nil {
		t.Fatal(err)
	}
	fake := httptest.NewServer(fakeflickr.NewServer(dataset, nil))
	defer fake.Close()
	engine, err := newCrawlEngine(pool, redisClient, cfg, flickrclient.NewClient(fake.URL+"/services/rest", nil))
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := queue.NewRiver(pool, map[string]int{"sync": 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := registerCrawlJobs(adapter, engine); err != nil {
		t.Fatal(err)
	}
	// The job waits in the queue before any worker runs.
	enqueueCrawlJob(t, ctx, pool, adapter, itemID, person)

	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- (queue.Worker{Queue: adapter}).Run(runCtx) }()
	defer func() {
		stop()
		if err := <-done; err != nil {
			t.Errorf("worker stopped with error: %v", err)
		}
	}()

	// One job is one page: the photo probe advances the item to the favorite
	// probe (pending), which the pump dispatches again as a second job.
	waitItemState(t, ctx, pool, itemID, "pending")
	if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready' WHERE id=$1`, itemID); err != nil {
		t.Fatal(err)
	}
	enqueueCrawlJob(t, ctx, pool, adapter, itemID, person)
	waitItemState(t, ctx, pool, itemID, "done")
	var followUps int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM work_items WHERE user_id=$1 AND kind='photos' AND subject_key=$2`, userID, person).Scan(&followUps); err != nil || followUps != 1 {
		t.Fatalf("incremental photos items = %d, err %v; want 1", followUps, err)
	}
	var calls int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM api_call_log WHERE key_id=$1`, keyID).Scan(&calls); err != nil {
		t.Fatal(err)
	}
	if calls < 2 {
		t.Fatalf("api_call_log rows = %d, want a photo probe and a favorite probe", calls)
	}
}

func waitItemState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID int64, want string) {
	t.Helper()
	var state string
	deadline := time.Now().Add(30 * time.Second)
	for {
		if err := pool.QueryRow(ctx, `SELECT state FROM work_items WHERE id=$1`, itemID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == want || time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if state != want {
		var lastError *string
		_ = pool.QueryRow(ctx, `SELECT last_error FROM work_items WHERE id=$1`, itemID).Scan(&lastError)
		t.Fatalf("work item %d state = %q (last_error %v), want %q", itemID, state, lastError, want)
	}
}

func enqueueCrawlJob(t *testing.T, ctx context.Context, pool *pgxpool.Pool, adapter *queue.River, itemID int64, person string) {
	t.Helper()
	args, err := json.Marshal(map[string]any{"work_item_id": itemID, "kind": "check", "subject_key": person})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Enqueue(ctx, tx, queue.Job{Kind: "check", Args: args}, queue.Options{Queue: "sync", MaxAttempts: 3, LeaseTimeout: time.Minute}); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func cleanupCrawlFixture(pool *pgxpool.Pool, userID int64, suffix string) {
	ctx := context.Background()
	_, _ = pool.Exec(ctx, `DELETE FROM river_job WHERE args->'args'->>'work_item_id' IN (SELECT id::text FROM work_items WHERE user_id=$1)`, userID)
	_, _ = pool.Exec(ctx, `DELETE FROM work_items WHERE user_id=$1`, userID)
	_, _ = pool.Exec(ctx, `DELETE FROM crawl_interests WHERE user_id=$1`, userID)
	_, _ = pool.Exec(ctx, `DELETE FROM users WHERE email=$1`, suffix+"@example.test")
}
