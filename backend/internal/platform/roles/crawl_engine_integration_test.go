package roles

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	redisv9 "github.com/redis/go-redis/v9"

	accountsdomain "github.com/jooservices/xflickr/internal/accounts/domain"
	"github.com/jooservices/xflickr/internal/fakeflickr"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/platform/queue"
	scheduleriver "github.com/jooservices/xflickr/internal/schedule/adapters/river"
	scheduleapp "github.com/jooservices/xflickr/internal/schedule/app"
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
	// Use a unique seeded person so another test user's interest in a shared
	// seed account cannot make the key pool select a key sealed with another
	// test's master key.
	person := suffix + "@N00"
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
	dataset.People = append(dataset.People, fakeflickr.Person{NSID: person, Username: suffix, RealName: "Crawl Integration Test"})
	dataset.Photos = append(dataset.Photos, fakeflickr.Photo{ID: suffix, Owner: person, Title: "Crawl integration photo", Shape: "landscape"})
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
	waitRiverJobState(t, ctx, pool, itemID, "completed")
	if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready' WHERE id=$1`, itemID); err != nil {
		t.Fatal(err)
	}
	enqueueCrawlJob(t, ctx, pool, adapter, itemID, person)
	waitRiverJobState(t, ctx, pool, itemID, "completed")
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

// TestCrawlWorkerKeepsRiverJobCompletedOnRecoverableFlickrFailure runs the
// real River worker against a Flickr that answers 429 and 105: the job must
// complete (not retry, not discard, no job.dead event) while the work item
// stays pending with an attempt consumed and a later not_before.
func TestCrawlWorkerKeepsRiverJobCompletedOnRecoverableFlickrFailure(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	for _, tc := range []struct {
		name string
		code int
		text string
	}{
		{name: "429", code: 429, text: "Too many requests"},
		{name: "105", code: 105, text: "Service currently unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := crawlIntegrationConfig(t)
			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			defer cancel()
			pool := dbtest.Open(t, cfg.DB)
			redisClient := redisv9.NewClient(&redisv9.Options{Addr: fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port), Password: cfg.Redis.Password, DB: cfg.Redis.DB})
			defer redisClient.Close()
			_, _, itemID, person := seedCrawlFixture(t, ctx, pool, cfg, "ready", time.Now().Add(2*time.Minute))

			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprintf(w, `{"stat":"fail","code":%d,"message":%q}`, tc.code, tc.text)
			}))
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

			waitItemState(t, ctx, pool, itemID, "pending")
			var attempts int
			var notBefore time.Time
			var lastError string
			if err := pool.QueryRow(ctx, `SELECT attempts, not_before, COALESCE(last_error,'') FROM work_items WHERE id=$1`, itemID).Scan(&attempts, &notBefore, &lastError); err != nil {
				t.Fatal(err)
			}
			if attempts != 1 || !notBefore.After(time.Now()) || lastError == "" {
				t.Fatalf("work item attempts=%d not_before=%s last_error=%q; want 1 attempt, a future not_before and a reason", attempts, notBefore, lastError)
			}
			var state string
			var riverAttempt int
			deadline := time.Now().Add(20 * time.Second)
			for {
				err := pool.QueryRow(ctx, `SELECT state, attempt FROM river_job WHERE args->'args'->>'work_item_id'=$1`, fmt.Sprint(itemID)).Scan(&state, &riverAttempt)
				if err != nil {
					t.Fatal(err)
				}
				if state == "completed" || time.Now().After(deadline) {
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
			if state != "completed" || riverAttempt != 1 {
				t.Fatalf("river job state=%s attempt=%d; want completed after one attempt, not retryable or discarded", state, riverAttempt)
			}
			var dead int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE topic='job.dead' AND payload->'args'->>'work_item_id'=$1`, fmt.Sprint(itemID)).Scan(&dead); err != nil || dead != 0 {
				t.Fatalf("job.dead events = %d, err %v; want none", dead, err)
			}
		})
	}
}

// seedCrawlFixture creates an account, key, depth-0 interest and one check
// work item in the given state, and returns the ids the test needs.
func seedCrawlFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, cfg config.Config, state string, lease any) (userID, keyID, itemID int64, person string) {
	t.Helper()
	ring, err := keyringFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	suffix := fmt.Sprintf("crawl-%d", time.Now().UnixNano())
	// Keep this fixture's key pool isolated from other users' interests: the
	// quota selector may choose any usable key for a shared Flickr person.
	person = suffix + "@N00"
	var accountID int64
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
	if err := pool.QueryRow(ctx, `INSERT INTO work_items(kind,subject_key,user_id,key_id,state,cursor,lease_expires_at) VALUES('check',$1,$2,$3,$5,$4,$6) RETURNING id`, person, userID, keyID, cursor, state, lease).Scan(&itemID); err != nil {
		t.Fatal(err)
	}

	return userID, keyID, itemID, person
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
		if lastError == nil {
			t.Fatalf("work item %d state = %q (last_error <nil>), want %q", itemID, state, want)
		}
		t.Fatalf("work item %d state = %q (last_error %q), want %q", itemID, state, *lastError, want)
	}
}

func waitRiverJobState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID int64, want string) {
	t.Helper()
	var state string
	deadline := time.Now().Add(30 * time.Second)
	for {
		err := pool.QueryRow(ctx, `SELECT state FROM river_job WHERE args->'args'->>'work_item_id'=$1 ORDER BY id DESC LIMIT 1`, fmt.Sprint(itemID)).Scan(&state)
		if err == nil && state == want {
			return
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("River job for work item %d state = %q, want %q (query error %v)", itemID, state, want, err)
		}
		time.Sleep(200 * time.Millisecond)
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

type fixedCredits int

func (c fixedCredits) Capacity(context.Context, int64, time.Duration) (int, error) {
	return int(c), nil
}

// TestRequeuedFailedItemRunsThroughPumpAndWorkerToDone proves a failed item
// returns to pending on requeue, is dispatched only by the pump, and the crawl
// worker then runs it against the fake Flickr and finishes it.
func TestRequeuedFailedItemRunsThroughPumpAndWorkerToDone(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	cfg := crawlIntegrationConfig(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool := dbtest.Open(t, cfg.DB)
	redisClient := redisv9.NewClient(&redisv9.Options{Addr: fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port), Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	defer redisClient.Close()
	_, keyID, itemID, person := seedCrawlFixture(t, ctx, pool, cfg, "failed", nil)
	if _, err := pool.Exec(ctx, `UPDATE work_items SET attempts=5, last_error='lease expired' WHERE id=$1`, itemID); err != nil {
		t.Fatal(err)
	}

	store := scheduleriver.Store{Pool: pool}
	if n, err := store.Requeue(ctx, []int64{itemID}, time.Now().Add(-time.Second)); err != nil || n != 1 {
		t.Fatalf("Requeue() = %d, %v; want 1", n, err)
	}
	waitItemState(t, ctx, pool, itemID, "pending")

	dataset, err := fakeflickr.LoadDataset("")
	if err != nil {
		t.Fatal(err)
	}
	dataset.People = append(dataset.People, fakeflickr.Person{NSID: person, Username: person, RealName: "Crawl Integration Test"})
	dataset.Photos = append(dataset.Photos, fakeflickr.Photo{ID: person + "-photo", Owner: person, Title: "Crawl integration photo", Shape: "landscape"})
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
	settings := scheduleapp.PumpSettings{PerKeyReadyCap: 10, NodeReadyCap: 1000, MediaReadyCap: 10, AIReadyCap: 10, SyncReadyCap: 10, RetryMax: 5, RetryBackoff: 30 * time.Second, LeaseTimeout: 2 * time.Minute}
	pump := func() {
		t.Helper()
		// Pump is synchronous: the item leaves pending before the worker can finish it.
		if _, err := store.Pump(ctx, settings, fixedCredits(100), adapter); err != nil {
			t.Fatalf("Pump() = %v", err)
		}
	}

	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- (queue.Worker{Queue: adapter}).Run(runCtx) }()
	defer func() {
		stop()
		if err := <-done; err != nil {
			t.Errorf("worker stopped with error: %v", err)
		}
	}()

	// One job is one page: the photo probe leaves the item pending for the
	// favorite probe, which the pump dispatches as a second job.
	pump()
	waitItemState(t, ctx, pool, itemID, "pending")
	pump()
	waitItemState(t, ctx, pool, itemID, "done")
	var calls int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM api_call_log WHERE key_id=$1`, keyID).Scan(&calls); err != nil || calls < 2 {
		t.Fatalf("api_call_log rows = %d, %v; want the worker's photo and favorite probes", calls, err)
	}
}
