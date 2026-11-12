package roles

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	redisv9 "github.com/redis/go-redis/v9"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/platform/queue"
	quotaPostgres "github.com/jooservices/xflickr/internal/quota/adapters/postgres"
	quotaRedis "github.com/jooservices/xflickr/internal/quota/adapters/redis"
	quotaapp "github.com/jooservices/xflickr/internal/quota/app"
	quotadomain "github.com/jooservices/xflickr/internal/quota/domain"
	scheduleriver "github.com/jooservices/xflickr/internal/schedule/adapters/river"
	scheduleapp "github.com/jooservices/xflickr/internal/schedule/app"
)

type quotaHoldFixture struct {
	ctx         context.Context
	pool        *pgxpool.Pool
	redisClient *redisv9.Client
	governor    quotaRedis.Governor
	store       scheduleriver.Store
	keyID       int64
	itemID      int64
	base        string
}

// TestHourlyCeilingHoldsPendingWorkUntilReset proves the 3400 effective budget
// holds background work at 3300 grants and resumes it after rolling expiry.
func TestHourlyCeilingHoldsPendingWorkUntilReset(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	f := newQuotaHoldFixture(t)
	assertQuotaBudget(t, f)
	assertSharedBurst(t, f)
	assertBackgroundHeld(t, f)
	assertInteractiveReserve(t, f)
	assertExpiredGrantsResume(t, f)
}

func newQuotaHoldFixture(t *testing.T) *quotaHoldFixture {
	t.Helper()
	cfg := crawlIntegrationConfig(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	t.Cleanup(cancel)
	pool := dbtest.Open(t, cfg.DB)
	client := redisv9.NewClient(&redisv9.Options{Addr: fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port), Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close quota Redis: %v", err)
		}
	})
	_, keyID, itemID, _ := seedCrawlFixture(t, ctx, pool, cfg, "pending", nil)
	if _, err := pool.Exec(ctx, `UPDATE api_keys SET per_hour_limit=3600, per_second_limit=4, burst=5 WHERE id=$1`, keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE work_items SET not_before=now() - interval '1 minute' WHERE id=$1`, itemID); err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("%sjooxf389:%d:", cfg.Redis.Prefix, keyID)
	f := &quotaHoldFixture{ctx: ctx, pool: pool, redisClient: client, store: scheduleriver.Store{Pool: pool}, keyID: keyID, itemID: itemID, base: fmt.Sprintf("%squota:key:%d:", prefix, keyID)}
	clearQuotaFixture(t, f)
	t.Cleanup(func() { clearQuotaFixture(t, f) })
	var err error
	f.governor, _, err = newQuotaGovernor(pool, client, config.Config{Redis: config.RedisConfig{Prefix: prefix}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (quotaRedis.Counters{Client: client, Prefix: prefix}).Restore(ctx, quotaapp.Usage{KeyID: keyID}); err != nil {
		t.Fatal(err)
	}
	return f
}

func clearQuotaFixture(t *testing.T, f *quotaHoldFixture) {
	t.Helper()
	clearQuotaKeys(t, f, "bucket", "rolling:interactive", "rolling:person", "rolling:photo", "cooldown", "rolling:ready", "rolling:sequence", "photo_demand")
}

func clearQuotaKeys(t *testing.T, f *quotaHoldFixture, names ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, name := range names {
		if err := f.redisClient.Del(ctx, f.base+name).Err(); err != nil {
			t.Fatalf("clear quota %s: %v", name, err)
		}
	}
}

func assertQuotaBudget(t *testing.T, f *quotaHoldFixture) {
	t.Helper()
	budget, err := (quotaPostgres.Budgets{Pool: f.pool}).Budget(f.ctx, f.keyID)
	if err != nil {
		t.Fatal(err)
	}
	if budget.HourlyLimit != 3400 || budget.PerSecondRate != 4 || budget.Burst != 5 || budget.Reserve != 100 {
		t.Fatalf("budget = %+v; want registry defaults 3400/h, 4/s, burst 5, reserve 100", budget)
	}
}

func assertSharedBurst(t *testing.T, f *quotaHoldFixture) {
	t.Helper()
	// Burst: five calls are granted across both lanes, the sixth waits.
	for i, class := range []quotadomain.Class{quotadomain.Person, quotadomain.Photo, quotadomain.Person, quotadomain.Photo, quotadomain.Person} {
		if permit, _, err := f.governor.Acquire(f.ctx, f.keyID, class); err != nil || permit == nil {
			t.Fatalf("burst call %d (%s) = %v, %v; want granted", i+1, class, permit, err)
		}
	}
	for _, class := range []quotadomain.Class{quotadomain.Photo, quotadomain.Person} {
		if permit, wait, err := f.governor.Acquire(f.ctx, f.keyID, class); err != nil || permit != nil || wait <= 0 {
			t.Fatalf("call past burst (%s) = %v, wait %s, err %v; want denied by the shared burst of 5", class, permit, wait, err)
		}
	}
}

func assertBackgroundHeld(t *testing.T, f *quotaHoldFixture) {
	t.Helper()
	// Ceiling: 3300 background calls exhaust the 3400 budget minus reserve.
	clearQuotaKeys(t, f, "rolling:interactive", "rolling:person", "rolling:photo", "bucket")
	seedQuotaGrants(t, f, quotaGrantSeed{"person", 3300, time.Now().UnixMilli()})
	for _, class := range []quotadomain.Class{quotadomain.Person, quotadomain.Photo} {
		if permit, wait, err := f.governor.Acquire(f.ctx, f.keyID, class); err != nil || permit != nil || wait <= 0 {
			t.Fatalf("%s call at the ceiling = %v, wait %s, err %v; want held until reset", class, permit, wait, err)
		}
	}
	if capacity, err := f.governor.Capacity(f.ctx, f.keyID, 2*time.Minute); err != nil || capacity != 0 {
		t.Fatalf("Capacity() at the ceiling = %d, %v; want 0", capacity, err)
	}
	// River job ids only grow, so the watermark and the range scan below stay on the primary key.
	var watermark int64
	if err := f.pool.QueryRow(f.ctx, `SELECT COALESCE(max(id), 0) FROM river_job`).Scan(&watermark); err != nil {
		t.Fatal(err)
	}
	pumpQuotaWork(t, f)
	var state string
	if err := f.pool.QueryRow(f.ctx, `SELECT state FROM work_items WHERE id=$1`, f.itemID).Scan(&state); err != nil || state != "pending" {
		t.Fatalf("item state at the ceiling = %q, %v; want pending (buffered)", state, err)
	}
	var created int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM (SELECT 1 FROM river_job WHERE id > $1 LIMIT 10) AS newer`, watermark).Scan(&created); err != nil || created != 0 {
		t.Fatalf("River jobs created at the ceiling = %d, %v; want none while quota is exhausted", created, err)
	}
}

func assertInteractiveReserve(t *testing.T, f *quotaHoldFixture) {
	t.Helper()
	// The first 99 reserve grants remain recent throughout the expiry phase.
	seedQuotaGrants(t, f, quotaGrantSeed{"interactive", 99, time.Now().UnixMilli()})
	clearQuotaKeys(t, f, "bucket")
	if permit, _, err := f.governor.Acquire(f.ctx, f.keyID, quotadomain.Interactive); err != nil || permit == nil {
		t.Fatalf("interactive reserve grant 100 = %v, %v; want granted", permit, err)
	}
	clearQuotaKeys(t, f, "bucket")
	if permit, wait, err := f.governor.Acquire(f.ctx, f.keyID, quotadomain.Interactive); err != nil || permit != nil || wait <= 0 {
		t.Fatalf("interactive grant 101 at 3400 = %v, wait %s, err %v; want denied", permit, wait, err)
	}
}

func assertExpiredGrantsResume(t *testing.T, f *quotaHoldFixture) {
	t.Helper()
	// Expire only the 3300 background grants; all 100 reserve grants stay recent.
	seedQuotaGrants(t, f, quotaGrantSeed{"person", 3300, time.Now().Add(-time.Hour - time.Second).UnixMilli()})
	clearQuotaKeys(t, f, "bucket")
	live, err := (quotaRedis.Counters{Client: f.redisClient, Prefix: f.governor.Prefix}).Live(f.ctx, f.keyID)
	if err != nil {
		t.Fatal(err)
	}
	if live.Used != 100 || live.ByClass[string(quotadomain.Interactive)] != 100 {
		t.Fatalf("usage after background expiry = %+v; want only 100 interactive grants", live)
	}
	// A full-hour window exposes hourly headroom instead of the short-window rate cap.
	if capacity, err := f.governor.Capacity(f.ctx, f.keyID, time.Hour); err != nil || capacity != 3200 {
		t.Fatalf("Capacity() after background expiry = %d, %v; want 3400 - 100 used - 100 reserve = 3200", capacity, err)
	}
	pumpQuotaWork(t, f)
	var state string
	if err := f.pool.QueryRow(f.ctx, `SELECT state FROM work_items WHERE id=$1`, f.itemID).Scan(&state); err != nil || state != "ready" {
		t.Fatalf("item state after reset = %q, %v; want ready", state, err)
	}
	if permit, _, err := f.governor.Acquire(f.ctx, f.keyID, quotadomain.Person); err != nil || permit == nil {
		t.Fatalf("person call after reset = %v, %v; want granted", permit, err)
	}
}

type quotaGrantSeed struct {
	class     string
	count     int
	timestamp int64
}

func seedQuotaGrants(t *testing.T, f *quotaHoldFixture, seed quotaGrantSeed) {
	t.Helper()
	grants := make([]redisv9.Z, seed.count)
	for i := range grants {
		grants[i] = redisv9.Z{Score: float64(seed.timestamp), Member: fmt.Sprintf("fixture:%d", i)}
	}
	if err := f.redisClient.ZAdd(f.ctx, f.base+"rolling:"+seed.class, grants...).Err(); err != nil {
		t.Fatal(err)
	}
}

func pumpQuotaWork(t *testing.T, f *quotaHoldFixture) {
	t.Helper()
	adapter, err := queue.NewRiver(f.pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	settings := scheduleapp.PumpSettings{PerKeyReadyCap: 10, NodeReadyCap: 1000, MediaReadyCap: 1000, AIReadyCap: 1000, SyncReadyCap: 1000, RetryMax: 5, RetryBackoff: 30 * time.Second, LeaseTimeout: 2 * time.Minute, ReclaimBatchSize: 100}
	if _, err := f.store.Pump(f.ctx, settings, f.governor, adapter); err != nil {
		t.Fatal(err)
	}
}
