package redis

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
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

func TestRebuildFromCallLog3000Grants(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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
	db, _ := strconv.Atoi(values["REDIS_DB"])
	if prefix == "" || prefix == "xflickr:" || db == 7 {
		t.Fatal("refusing the live Redis index")
	}
	port, _ := strconv.Atoi(values["REDIS_PORT"])
	if port == 0 {
		port = 16382
	}
	host := values["REDIS_HOST"]
	if host == "" {
		host = "127.0.0.1"
	}
	client := redisv9.NewClient(&redisv9.Options{
		Addr:     net.JoinHostPort(host, strconv.Itoa(port)),
		Password: values["REDIS_PASSWORD"],
		DB:       db,
	})
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping shared Redis: %v", err)
	}

	var userID, keyID int64
	email := fmt.Sprintf("jooxf344-%d@example.invalid", time.Now().UnixNano())
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, role, locale) VALUES ($1, 'integration-hash', 'user', 'en') RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID); err != nil {
			t.Errorf("cleanup user: %v", err)
		}
	})
	const hourly = 3600
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys (user_id, label, key_public, secret_sealed, per_hour_limit, per_second_limit, burst) VALUES ($1, 'quota-344', 'k344', '{}'::jsonb, $2, 1000, 1000) RETURNING id`, userID, hourly).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	prefix = fmt.Sprintf("%sjooxf344:%d:", prefix, keyID)
	clearKeys := func() {
		pattern := prefix + "quota:key:" + strconv.FormatInt(keyID, 10) + ":rolling:*"
		iter := client.Scan(context.Background(), 0, pattern, 100).Iterator()
		for iter.Next(context.Background()) {
			_ = client.Del(context.Background(), iter.Val()).Err()
		}
		for _, name := range (Governor{Prefix: prefix}).keys(keyID) {
			_ = client.Del(context.Background(), name).Err()
		}
	}
	clearKeys()
	t.Cleanup(clearKeys)

	const prior = 3000
	if _, err := pool.Exec(ctx, `INSERT INTO api_call_log(key_id, class, at, called_at, outcome)
		SELECT $1, 'interactive', now() - interval '1 minute', now() - interval '1 minute', NULL
		FROM generate_series(1, $2)`, keyID, prior); err != nil {
		t.Fatal(err)
	}

	budget := domain.Budget{HourlyLimit: hourly, PerSecondRate: 1000, Burst: 1000, Reserve: 0, PersonLaneShare: 0}
	counters := Counters{Client: client, Prefix: prefix}
	rebuilder := &app.Rebuilder{Store: quotapostgres.Usage{Pool: pool}, Calls: quotapostgres.CallLog{Pool: pool}, Counters: counters}
	crawl, err := NewGovernor(client, prefix, StaticBudgets{keyID: budget}, rebuilder, quotapostgres.CallLog{Pool: pool})
	if err != nil {
		t.Fatal(err)
	}
	scheduler, err := NewGovernor(client, prefix, StaticBudgets{keyID: budget}, rebuilder, quotapostgres.CallLog{Pool: pool})
	if err != nil {
		t.Fatal(err)
	}

	clearKeys()
	permit, _, err := crawl.Acquire(ctx, keyID, domain.Interactive)
	if err != nil || permit == nil {
		t.Fatalf("first grant after Redis loss = %v %v", permit, err)
	}
	used := int64(0)
	for _, lane := range []int{1, 2, 4} {
		n, err := client.ZCard(ctx, crawl.keys(keyID)[lane]).Result()
		if err != nil {
			t.Fatal(err)
		}
		used += n
	}
	if used != prior+1 {
		t.Fatalf("ledger after first grant = %d, want %d (call log + grant)", used, prior+1)
	}
	remaining := hourly - int(used)
	for i := 0; i < remaining; i++ {
		g := crawl
		if i%2 == 1 {
			g = scheduler
		}
		if permit, _, err := g.Acquire(ctx, keyID, domain.Interactive); err != nil || permit == nil {
			t.Fatalf("fill grant %d = %v %v", i, permit, err)
		}
	}
	if permit, wait, err := crawl.Acquire(ctx, keyID, domain.Interactive); err != nil || permit != nil || wait <= 0 {
		t.Fatalf("overspend after rebuilt ledger: permit=%v wait=%s err=%v", permit, wait, err)
	}
	final := int64(0)
	for _, lane := range []int{1, 2, 4} {
		n, err := client.ZCard(ctx, crawl.keys(keyID)[lane]).Result()
		if err != nil {
			t.Fatal(err)
		}
		final += n
	}
	if final != hourly {
		t.Fatalf("final ledger = %d, want hourly limit %d", final, hourly)
	}
}
