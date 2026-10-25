package redis

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/joho/godotenv"
	redisv9 "github.com/redis/go-redis/v9"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	quotapostgres "github.com/jooservices/xflickr/internal/quota/adapters/postgres"
	"github.com/jooservices/xflickr/internal/quota/app"
	"github.com/jooservices/xflickr/internal/quota/domain"
)

func integrationRoot(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../"))
}

func TestRedisLossMidWindowDoesNotOverspend(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	root := integrationRoot(t)
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

	var userID, keyID int64
	email := fmt.Sprintf("jooxf107-%d@example.invalid", time.Now().UnixNano())
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, role, locale) VALUES ($1, 'integration-hash', 'user', 'en') RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID); err != nil {
			t.Errorf("cleanup user: %v", err)
		}
	})
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys (user_id, label, key_public, secret_sealed, per_hour_limit, per_second_limit, burst) VALUES ($1, 'quota-107', 'k107', '{}'::jsonb, 20, 100, 100) RETURNING id`, userID).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	prefix = fmt.Sprintf("%sjooxf107:%d:", prefix, keyID)
	clearKeys := func() {
		for _, name := range (Governor{Prefix: prefix}).keys(keyID) {
			client.Del(context.Background(), name)
		}
	}
	clearKeys()
	t.Cleanup(clearKeys)

	const limit = 20
	budget := domain.Budget{HourlyLimit: limit, PerSecondRate: 100, Burst: 100, Reserve: 0, PersonLaneShare: 0}
	counters := Counters{Client: client, Prefix: prefix}
	store := quotapostgres.Usage{Pool: pool}
	rebuilder := &app.Rebuilder{Store: store, Calls: quotapostgres.CallLog{Pool: pool}, Counters: counters}
	governor := Governor{Client: client, Prefix: prefix, Budgets: StaticBudgets{keyID: budget}, Rebuilder: rebuilder}
	flusher := app.Flusher{Store: store, Counters: counters}

	granted := func(n int) int {
		var wg sync.WaitGroup
		var mu sync.Mutex
		count := 0
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				permit, _, err := governor.Acquire(ctx, keyID, domain.Interactive)
				if err != nil {
					t.Errorf("Acquire: %v", err)
					return
				}
				if permit != nil {
					mu.Lock()
					count++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		return count
	}

	before := granted(8)
	if before != 8 {
		t.Fatalf("granted before loss = %d", before)
	}
	if err := flusher.FlushOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var flushed int64
	var byClass string
	if err := pool.QueryRow(ctx, `SELECT used, by_class::text FROM quota_usage WHERE key_id=$1`, keyID).Scan(&flushed, &byClass); err != nil {
		t.Fatal(err)
	}
	if flushed != 8 || byClass != `{"interactive": 8}` {
		t.Fatalf("flushed used=%d by_class=%s", flushed, byClass)
	}

	// Kill every Redis key for this API key mid-window.
	clearKeys()
	after := granted(100)
	if before+after > limit {
		t.Fatalf("overspent: %d before loss + %d after loss > %d", before, after, limit)
	}
	if after != limit-before {
		t.Fatalf("granted after loss = %d, want %d", after, limit-before)
	}
}
