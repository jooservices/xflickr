package roles

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	redisv9 "github.com/redis/go-redis/v9"

	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/platform/queue"
	quotaPostgres "github.com/jooservices/xflickr/internal/quota/adapters/postgres"
	quotaRedis "github.com/jooservices/xflickr/internal/quota/adapters/redis"
	quotadomain "github.com/jooservices/xflickr/internal/quota/domain"
	scheduleriver "github.com/jooservices/xflickr/internal/schedule/adapters/river"
	scheduleapp "github.com/jooservices/xflickr/internal/schedule/app"
)

// TestHourlyCeilingHoldsPendingWorkUntilReset proves, with the real Redis
// governor and the real pump, that a key at the 3,500 automated-call ceiling
// (3,600 limit minus the 100-call reserve) keeps pending work buffered and
// releases it after the hourly window resets. The 4/s rate and burst of 5 are
// shared by the person and photo lanes, and the reserve stays for interactive use.
func TestHourlyCeilingHoldsPendingWorkUntilReset(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	cfg := crawlIntegrationConfig(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool := dbtest.Open(t, cfg.DB)
	redisClient := redisv9.NewClient(&redisv9.Options{Addr: fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port), Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	defer redisClient.Close()
	_, keyID, itemID, _ := seedCrawlFixture(t, ctx, pool, cfg, "pending", nil)
	if _, err := pool.Exec(ctx, `UPDATE api_keys SET per_hour_limit=3600, per_second_limit=4, burst=5 WHERE id=$1`, keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE work_items SET not_before=now() - interval '1 minute' WHERE id=$1`, itemID); err != nil {
		t.Fatal(err)
	}

	prefix := fmt.Sprintf("%sjooxf261:%d:", cfg.Redis.Prefix, keyID)
	base := fmt.Sprintf("%squota:key:%d:", prefix, keyID)
	clear := func() {
		for _, name := range []string{"bucket", "hourly", "window", "cooldown", "byclass", "ready"} {
			redisClient.Del(context.Background(), base+name)
		}
	}
	clear()
	t.Cleanup(clear)
	governor := quotaRedis.Governor{Client: redisClient, Prefix: prefix, Budgets: quotaPostgres.Budgets{Pool: pool}}
	budget, err := (quotaPostgres.Budgets{Pool: pool}).Budget(ctx, keyID)
	if err != nil {
		t.Fatal(err)
	}
	if budget.HourlyLimit != 3600 || budget.PerSecondRate != 4 || budget.Burst != 5 || budget.Reserve != 100 {
		t.Fatalf("budget = %+v; want 3600/h, 4/s, burst 5, reserve 100", budget)
	}

	// Burst: five calls are granted across both lanes, the sixth waits.
	for i, class := range []quotadomain.Class{quotadomain.Person, quotadomain.Photo, quotadomain.Person, quotadomain.Photo, quotadomain.Person} {
		if permit, _, err := governor.Acquire(ctx, keyID, class); err != nil || permit == nil {
			t.Fatalf("burst call %d (%s) = %v, %v; want granted", i+1, class, permit, err)
		}
	}
	for _, class := range []quotadomain.Class{quotadomain.Photo, quotadomain.Person} {
		if permit, wait, err := governor.Acquire(ctx, keyID, class); err != nil || permit != nil || wait <= 0 {
			t.Fatalf("call past burst (%s) = %v, wait %s, err %v; want denied by the shared 4/s bucket", class, permit, wait, err)
		}
	}

	// Ceiling: 3,500 automated calls spent in the current window.
	now := time.Now().UnixMilli()
	mustSet := func(name string, value any) {
		t.Helper()
		if err := redisClient.Set(ctx, base+name, value, time.Hour).Err(); err != nil {
			t.Fatal(err)
		}
	}
	mustSet("window", now)
	mustSet("hourly", 3500)
	redisClient.Del(ctx, base+"bucket")
	for _, class := range []quotadomain.Class{quotadomain.Person, quotadomain.Photo} {
		if permit, wait, err := governor.Acquire(ctx, keyID, class); err != nil || permit != nil || wait <= 0 {
			t.Fatalf("%s call at the ceiling = %v, wait %s, err %v; want held until reset", class, permit, wait, err)
		}
	}
	if capacity, err := governor.Capacity(ctx, keyID, 2*time.Minute); err != nil || capacity != 0 {
		t.Fatalf("Capacity() at the ceiling = %d, %v; want 0", capacity, err)
	}
	adapter, err := queue.NewRiver(pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	settings := scheduleapp.PumpSettings{PerKeyReadyCap: 10, NodeReadyCap: 1000, MediaReadyCap: 1000, AIReadyCap: 1000, SyncReadyCap: 1000, RetryMax: 5, RetryBackoff: 30 * time.Second, LeaseTimeout: 2 * time.Minute}
	store := scheduleriver.Store{Pool: pool}
	if _, err := store.Pump(ctx, settings, governor, adapter); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM work_items WHERE id=$1`, itemID).Scan(&state); err != nil || state != "pending" {
		t.Fatalf("item state at the ceiling = %q, %v; want pending (buffered)", state, err)
	}
	var jobs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE args->'args'->>'work_item_id'=$1`, fmt.Sprint(itemID)).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("River jobs at the ceiling = %d, %v; want none", jobs, err)
	}

	// Reserve: the interactive lane may still spend the last 100 calls.
	redisClient.Del(ctx, base+"bucket")
	if permit, _, err := governor.Acquire(ctx, keyID, quotadomain.Interactive); err != nil || permit == nil {
		t.Fatalf("interactive call in the reserve = %v, %v; want granted", permit, err)
	}

	// Reset: the hourly window rolls over and the buffered item is promoted.
	mustSet("window", now-int64(time.Hour/time.Millisecond)-1000)
	redisClient.Del(ctx, base+"bucket")
	if capacity, err := governor.Capacity(ctx, keyID, 2*time.Minute); err != nil || capacity <= 0 {
		t.Fatalf("Capacity() after reset = %d, %v; want credits again", capacity, err)
	}
	if _, err := store.Pump(ctx, settings, governor, adapter); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT state FROM work_items WHERE id=$1`, itemID).Scan(&state); err != nil || state != "ready" {
		t.Fatalf("item state after reset = %q, %v; want ready", state, err)
	}
	if permit, _, err := governor.Acquire(ctx, keyID, quotadomain.Person); err != nil || permit == nil {
		t.Fatalf("person call after reset = %v, %v; want granted", permit, err)
	}
}
