package redis

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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

// TestReadyMarkerSurvivesSteadyTraffic proves a grant extends an existing ready
// marker TTL so the marker stays live without a rebuild. Deterministic: no
// wall-clock loops or sleeps.
func TestReadyMarkerSurvivesSteadyTraffic(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the worktree Redis")
	}
	values, err := godotenv.Read(filepath.Join(integrationRoot(t), ".env"))
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping worktree Redis: %v", err)
	}

	const keyID int64 = 344344344
	rolePrefix := prefix + "jooxf344ready:"
	budget := domain.Budget{HourlyLimit: 3600, PerSecondRate: 10000, Burst: 10000, Reserve: 0, PersonLaneShare: 0}
	var rebuilds atomic.Int64
	inner := rebuilderFor(Governor{Client: client, Prefix: rolePrefix}, memStore{}, memCalls{})
	governor, err := NewGovernor(client, rolePrefix, StaticBudgets{keyID: budget}, countingRebuilder{inner: inner, n: &rebuilds}, nil)
	if err != nil {
		t.Fatal(err)
	}
	keys := governor.keys(keyID)
	readyKey := keys[5]
	laneKey := keys[1]
	t.Cleanup(func() {
		for _, key := range keys {
			_ = client.Del(context.Background(), key).Err()
		}
	})
	for _, key := range keys {
		if err := client.Del(ctx, key).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := (Counters{Client: client, Prefix: rolePrefix}).Restore(ctx, app.Usage{KeyID: keyID}); err != nil {
		t.Fatal(err)
	}
	const shortTTL = time.Second
	if err := client.PExpire(ctx, readyKey, shortTTL).Err(); err != nil {
		t.Fatal(err)
	}

	permit, _, err := governor.Acquire(ctx, keyID, domain.Interactive)
	if err != nil || permit == nil {
		t.Fatalf("grant after short ready TTL = %v %v (rebuilds=%d)", permit, err, rebuilds.Load())
	}
	ttl, err := client.PTTL(ctx, readyKey).Result()
	if err != nil {
		t.Fatal(err)
	}
	if ttl <= time.Hour {
		t.Fatalf("grant must extend ready marker TTL above 1h, PTTL=%s", ttl)
	}
	if rebuilds.Load() != 0 {
		t.Fatalf("rebuild ran %d times; a grant must only extend the ready marker", rebuilds.Load())
	}
	if n, err := client.Exists(ctx, readyKey).Result(); err != nil || n != 1 {
		t.Fatalf("ready marker missing after grant: exists=%d err=%v", n, err)
	}
	used, err := client.ZCard(ctx, laneKey).Result()
	if err != nil || used != 1 {
		t.Fatalf("rolling window count = %d err=%v, want 1", used, err)
	}
}

// TestRebuildAfterMarkerLossDoesNotDoubleCount proves that after the ready
// marker alone is lost, a rebuild from the call log replaces lane members so
// live <now>:<seq> names and log: identities never both count.
func TestRebuildAfterMarkerLossDoesNotDoubleCount(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the worktree Redis")
	}
	values, err := godotenv.Read(filepath.Join(integrationRoot(t), ".env"))
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping worktree Redis: %v", err)
	}

	const keyID int64 = 344344345
	const prior = 12
	rolePrefix := prefix + "jooxf344dedupe:"
	budget := domain.Budget{HourlyLimit: 3600, PerSecondRate: 10000, Burst: 10000, Reserve: 0, PersonLaneShare: 0}
	log := &recordingCallLog{}
	var rebuilds atomic.Int64
	inner := rebuilderFor(Governor{Client: client, Prefix: rolePrefix}, memStore{}, log)
	governor, err := NewGovernor(client, rolePrefix, StaticBudgets{keyID: budget}, countingRebuilder{inner: inner, n: &rebuilds}, log)
	if err != nil {
		t.Fatal(err)
	}
	keys := governor.keys(keyID)
	readyKey := keys[5]
	laneKey := keys[1]
	t.Cleanup(func() {
		for _, key := range keys {
			_ = client.Del(context.Background(), key).Err()
		}
	})
	for _, key := range keys {
		if err := client.Del(ctx, key).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := (Counters{Client: client, Prefix: rolePrefix}).Restore(ctx, app.Usage{KeyID: keyID}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < prior; i++ {
		permit, _, err := governor.Acquire(ctx, keyID, domain.Interactive)
		if err != nil || permit == nil {
			t.Fatalf("live grant %d = %v %v", i, permit, err)
		}
	}
	if n := log.len(); n != prior {
		t.Fatalf("call log after live grants = %d, want %d", n, prior)
	}
	liveBefore, err := client.ZCard(ctx, laneKey).Result()
	if err != nil || liveBefore != prior {
		t.Fatalf("lane before marker loss = %d err=%v, want %d", liveBefore, err, prior)
	}
	if err := client.Del(ctx, readyKey).Err(); err != nil {
		t.Fatal(err)
	}
	rebuilds.Store(0)

	permit, _, err := governor.Acquire(ctx, keyID, domain.Interactive)
	if err != nil || permit == nil {
		t.Fatalf("acquire after marker loss = %v %v", permit, err)
	}
	if rebuilds.Load() != 1 {
		t.Fatalf("rebuilds = %d, want 1 after marker loss", rebuilds.Load())
	}
	distinct := log.len()
	if distinct != prior+1 {
		t.Fatalf("call log after rebuild grant = %d, want %d", distinct, prior+1)
	}
	used, err := client.ZCard(ctx, laneKey).Result()
	if err != nil {
		t.Fatal(err)
	}
	if used != int64(distinct) {
		t.Fatalf("rolling window count = %d, want %d distinct grants (not %d from live+log double count)", used, distinct, 2*prior+1)
	}
}

func TestGovernorUsableQuotaConcurrentRoles(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the isolated worktree services")
	}
	root := integrationRoot(t)
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	pool := dbtest.Open(t, cfg)
	values, err := godotenv.Read(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal("read worktree Redis configuration")
	}
	basePrefix := values["REDIS_PREFIX"]
	db, _ := strconv.Atoi(values["REDIS_DB"])
	if basePrefix == "" || basePrefix == "xflickr:" || db == 7 {
		t.Fatal("refusing the live Redis namespace")
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
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping worktree Redis: %v", err)
	}

	for key, value := range map[string]string{
		"usable_hourly_quota":    "20",
		"daily_call_limit":       "0",
		"quota_reserve":          "0",
		"person_lane_share":      "0",
		"photo_lane_floor_share": "0",
	} {
		setRedisIntegrationSetting(ctx, t, pool, key, value)
	}
	unique := fmt.Sprintf("jooxf366-%d", time.Now().UnixNano())
	var userID, keyID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, role, locale)
		VALUES ($1, 'integration-hash', 'user', 'en') RETURNING id`, unique+"@example.invalid").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM users WHERE id=$1`, userID); err != nil {
			t.Errorf("cleanup quota test user: %v", err)
		}
	})
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys
		(user_id, label, key_public, secret_sealed, per_hour_limit, per_second_limit, burst)
		VALUES ($1, $2, $2, '{}'::jsonb, 100, 1000, 1000) RETURNING id`, userID, unique).Scan(&keyID); err != nil {
		t.Fatal(err)
	}

	prefix := fmt.Sprintf("%sjooxf366:%d:", basePrefix, keyID)
	counters := Counters{Client: client, Prefix: prefix}
	rebuilder := &app.Rebuilder{
		Store:    quotapostgres.Usage{Pool: pool},
		Calls:    quotapostgres.CallLog{Pool: pool},
		Counters: counters,
	}
	budgets := quotapostgres.Budgets{Pool: pool}
	crawl, err := NewGovernor(client, prefix, budgets, rebuilder, nil)
	if err != nil {
		t.Fatal(err)
	}
	scheduler, err := NewGovernor(client, prefix, budgets, rebuilder, nil)
	if err != nil {
		t.Fatal(err)
	}
	if crawl.keys(keyID)[1] != scheduler.keys(keyID)[1] {
		t.Fatal("roles do not share the same rolling ledger")
	}
	t.Cleanup(func() {
		for _, key := range crawl.keys(keyID) {
			_ = client.Del(context.Background(), key).Err()
		}
	})
	for _, key := range crawl.keys(keyID) {
		if err := client.Del(ctx, key).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := counters.Restore(ctx, app.Usage{KeyID: keyID}); err != nil {
		t.Fatalf("prepare rolling ledger: %v", err)
	}

	roles := []Governor{crawl, scheduler}
	start := make(chan struct{})
	results := make(chan error, len(roles))
	var grants atomic.Int64
	var workers sync.WaitGroup
	for _, role := range roles {
		role := role
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for attempt := 0; attempt < 24; attempt++ {
				permit, _, err := role.Acquire(ctx, keyID, domain.Interactive)
				if err != nil {
					results <- err
					return
				}
				if permit != nil {
					grants.Add(1)
				}
			}
			results <- nil
		}()
	}
	close(start)
	workers.Wait()
	for range roles {
		if err := <-results; err != nil {
			t.Fatalf("concurrent role acquire: %v", err)
		}
	}
	if grants.Load() != 20 {
		t.Fatalf("concurrent roles granted %d calls, want usable quota 20", grants.Load())
	}
	used := int64(0)
	for _, index := range []int{1, 2, 4} {
		count, err := client.ZCard(ctx, crawl.keys(keyID)[index]).Result()
		if err != nil {
			t.Fatal(err)
		}
		used += count
	}
	if used != 20 {
		t.Fatalf("shared rolling ledger contains %d grants, want 20", used)
	}
}

func setRedisIntegrationSetting(ctx context.Context, t *testing.T, pool *pgxpool.Pool, key, value string) {
	t.Helper()
	var original []byte
	if err := pool.QueryRow(ctx, `SELECT value FROM app_settings WHERE key=$1`, key).Scan(&original); err != nil {
		t.Fatalf("read %s setting: %v", key, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE app_settings SET value=$1::jsonb WHERE key=$2`, value, key); err != nil {
		t.Fatalf("update %s setting: %v", key, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanupCtx, `UPDATE app_settings SET value=$1::jsonb WHERE key=$2`, string(original), key); err != nil {
			t.Errorf("restore %s setting: %v", key, err)
		}
	})
}
