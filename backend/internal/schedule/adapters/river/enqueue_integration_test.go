package river_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/platform/queue"
	scheduleriver "github.com/jooservices/xflickr/internal/schedule/adapters/river"
	"github.com/jooservices/xflickr/internal/schedule/app"
)

func TestPumpDispatchesExactlyOneHundredOfTenThousandRows(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("pump-%d", time.Now().UnixNano())
	keyID := seedKey(t, ctx, pool, suffix)
	t.Cleanup(func() { cleanupPumpFixture(t, pool, suffix) })
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,key_id,state,priority_base) SELECT 'sync_person',$1 || '-' || n,$2,'pending',n FROM generate_series(1,10000) n`, suffix, keyID); err != nil {
		t.Fatal(err)
	}
	adapter, err := queue.NewRiver(pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	settings := app.PumpSettings{PerKeyReadyCap: 100, NodeReadyCap: 1000, MediaReadyCap: 1000, AIReadyCap: 1000, SyncReadyCap: 1000, RetryMax: 5, RetryBackoff: 30 * time.Second, LeaseTimeout: 2 * time.Minute}
	dispatched, err := (scheduleriver.Store{Pool: pool}).Pump(ctx, settings, fixedCapacity(100), adapter)
	if err != nil {
		t.Fatal(err)
	}
	if dispatched != 100 {
		t.Fatalf("Pump() dispatched %d rows, want 100", dispatched)
	}
	var ready, riverJobs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE state='ready'), count(*) FROM work_items WHERE subject_key LIKE $1`, suffix+"-%").Scan(&ready, &riverJobs); err != nil {
		t.Fatal(err)
	}
	if ready != 100 {
		t.Fatalf("ready rows = %d, want 100", ready)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE queue='sync' AND args->'args'->>'subject_key' LIKE $1`, suffix+"-%").Scan(&riverJobs); err != nil {
		t.Fatal(err)
	}
	if riverJobs != 100 {
		t.Fatalf("River jobs = %d, want 100", riverJobs)
	}
	zeroCredits, err := (scheduleriver.Store{Pool: pool}).Pump(ctx, settings, fixedCapacity(0), adapter)
	if err != nil || zeroCredits != 0 {
		t.Fatalf("Pump() with no remaining credits = %d, %v", zeroCredits, err)
	}
}

func TestExpiredLeaseReturnsToPendingThenExhausts(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("lease-%d", time.Now().UnixNano())
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,state,attempts,lease_expires_at) VALUES('sync_person',$1,'ready',0,now()-interval '1 second')`, suffix); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupPumpFixture(t, pool, suffix) })
	store := scheduleriver.Store{Pool: pool}
	if count, err := store.Reclaim(ctx, 5, 30*time.Second, time.Now()); err != nil || count != 1 {
		t.Fatalf("Reclaim() = %d, %v", count, err)
	}
	var state string
	var attempts int
	var notBefore time.Time
	if err := pool.QueryRow(ctx, `SELECT state,attempts,not_before FROM work_items WHERE subject_key=$1`, suffix).Scan(&state, &attempts, &notBefore); err != nil {
		t.Fatal(err)
	}
	if state != "pending" || attempts != 1 || time.Until(notBefore) < 20*time.Second {
		t.Fatalf("reclaimed row state=%s attempts=%d not_before=%s", state, attempts, notBefore)
	}
	if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready',attempts=1,lease_expires_at=now()-interval '1 second' WHERE subject_key=$1`, suffix); err != nil {
		t.Fatal(err)
	}
	if count, err := store.Reclaim(ctx, 5, 30*time.Second, time.Now()); err != nil || count != 1 {
		t.Fatalf("second retry Reclaim() = %d, %v", count, err)
	}
	if err := pool.QueryRow(ctx, `SELECT not_before FROM work_items WHERE subject_key=$1`, suffix).Scan(&notBefore); err != nil {
		t.Fatal(err)
	}
	if time.Until(notBefore) < 50*time.Second {
		t.Fatalf("second retry backoff = %s, want at least one minute", time.Until(notBefore))
	}
	if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready',attempts=4,lease_expires_at=now()-interval '1 second' WHERE subject_key=$1`, suffix); err != nil {
		t.Fatal(err)
	}
	if count, err := store.Reclaim(ctx, 5, 30*time.Second, time.Now()); err != nil || count != 1 {
		t.Fatalf("exhaust Reclaim() = %d, %v", count, err)
	}
	if err := pool.QueryRow(ctx, `SELECT state,attempts FROM work_items WHERE subject_key=$1`, suffix).Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "failed" || attempts != 5 {
		t.Fatalf("exhausted row state=%s attempts=%d", state, attempts)
	}
	var deadEvents int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE topic='job.dead' AND key=(SELECT id::text FROM work_items WHERE subject_key=$1)`, suffix).Scan(&deadEvents); err != nil || deadEvents != 1 {
		t.Fatalf("dead events=%d err=%v", deadEvents, err)
	}
}

func TestReclaimRollsBackWhenFailedRowCannotBeUpdated(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("reclaim-update-fail-%d", time.Now().UnixNano())
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,state,attempts,lease_expires_at) VALUES('sync_person',$1,'ready',4,now()-interval '1 second')`, suffix); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE subject_key=$1`, suffix) })
	function := "fail_reclaim_update_" + fmt.Sprint(time.Now().UnixNano())
	trigger := "trigger_reclaim_update_" + fmt.Sprint(time.Now().UnixNano())
	statement := fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.subject_key = '%s' AND NEW.state = 'failed' THEN RAISE EXCEPTION 'forced reclaim update failure'; END IF; RETURN NEW; END $$`, function, suffix)
	if _, err := pool.Exec(ctx, statement); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TRIGGER %s BEFORE UPDATE ON work_items FOR EACH ROW EXECUTE FUNCTION %s()`, trigger, function)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON work_items`, trigger))
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, function))
	})
	if _, err := (scheduleriver.Store{Pool: pool}).Reclaim(ctx, 5, 30*time.Second, time.Now()); err == nil {
		t.Fatal("Reclaim ignored failed work update")
	}
	var state string
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT state,attempts FROM work_items WHERE subject_key=$1`, suffix).Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "ready" || attempts != 4 {
		t.Fatalf("row after rollback state=%s attempts=%d", state, attempts)
	}
}

func TestReclaimReturnsOutboxFailure(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("reclaim-outbox-fail-%d", time.Now().UnixNano())
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,state,attempts,lease_expires_at) VALUES('sync_person',$1,'ready',4,now()-interval '1 second')`, suffix); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE subject_key=$1`, suffix) })
	function := "fail_reclaim_outbox_" + fmt.Sprint(time.Now().UnixNano())
	trigger := "trigger_reclaim_outbox_" + fmt.Sprint(time.Now().UnixNano())
	statement := fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.topic = 'job.dead' AND NEW.payload->>'work_item_id' = (SELECT id::text FROM work_items WHERE subject_key = '%s') THEN RAISE EXCEPTION 'forced outbox failure'; END IF; RETURN NEW; END $$`, function, suffix)
	if _, err := pool.Exec(ctx, statement); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION %s()`, trigger, function)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON outbox_events`, trigger))
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, function))
	})
	if _, err := (scheduleriver.Store{Pool: pool}).Reclaim(ctx, 5, 30*time.Second, time.Now()); err == nil {
		t.Fatal("Reclaim ignored outbox publication error")
	}
	var state string
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT state,attempts FROM work_items WHERE subject_key=$1`, suffix).Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "ready" || attempts != 4 {
		t.Fatalf("row after outbox rollback state=%s attempts=%d", state, attempts)
	}
}

func TestLoadPumpSettings(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	settings, err := scheduleriver.LoadSettings(t.Context(), pool)
	if err != nil {
		t.Fatal(err)
	}
	if settings.RetryMax != 5 || settings.RetryBackoff != 30*time.Second || settings.LeaseTimeout != 2*time.Minute || settings.PerKeyReadyCap != 100 {
		t.Fatalf("pump settings = %+v", settings)
	}
}

func TestLoadPumpSettingsRejectsInvalidAndMissingValues(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	ctx := t.Context()
	var original []byte
	if err := pool.QueryRow(ctx, `SELECT value FROM app_settings WHERE key='per_key_ready_cap'`).Scan(&original); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `UPDATE app_settings SET value=$1 WHERE key='per_key_ready_cap'`, original)
	if _, err := pool.Exec(ctx, `UPDATE app_settings SET value='"invalid"'::jsonb WHERE key='per_key_ready_cap'`); err != nil {
		t.Fatal(err)
	}
	if _, err := scheduleriver.LoadSettings(ctx, pool); err == nil {
		t.Fatal("LoadSettings accepted a non-integer cap")
	}
	if _, err := pool.Exec(ctx, `UPDATE app_settings SET value=$1 WHERE key='per_key_ready_cap'`, original); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM app_settings WHERE key='per_key_ready_cap'`); err != nil {
		t.Fatal(err)
	}
	if _, err := scheduleriver.LoadSettings(ctx, pool); err == nil {
		t.Fatal("LoadSettings accepted a missing cap")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO app_settings(key,value,user_overridable,min) VALUES('per_key_ready_cap',$1,FALSE,0)`, original); err != nil {
		t.Fatal(err)
	}
}

func TestPumpStopsWhenQuotaGovernorIsOffline(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("offline-%d", time.Now().UnixNano())
	keyID := seedKey(t, ctx, pool, suffix)
	t.Cleanup(func() { cleanupPumpFixture(t, pool, suffix) })
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,key_id,state) VALUES('sync_person',$1,$2,'pending')`, suffix+"-only", keyID); err != nil {
		t.Fatal(err)
	}
	adapter, err := queue.NewRiver(pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	settings := app.PumpSettings{PerKeyReadyCap: 10, NodeReadyCap: 10, MediaReadyCap: 10, AIReadyCap: 10, SyncReadyCap: 10, RetryMax: 5, RetryBackoff: 30 * time.Second, LeaseTimeout: 2 * time.Minute}
	if _, err := (scheduleriver.Store{Pool: pool}).Pump(ctx, settings, failingCapacity{}, adapter); err == nil {
		t.Fatal("Pump continued while quota governor was offline")
	}
	var pending, jobs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE state='pending') FROM work_items WHERE subject_key=$1`, suffix+"-only").Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE args->'args'->>'subject_key'=$1`, suffix+"-only").Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if pending != 1 || jobs != 0 {
		t.Fatalf("offline result pending=%d river jobs=%d", pending, jobs)
	}
}

func TestPumpReturnsWhenAnotherInstanceLeads(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(113)`).Scan(&locked); err != nil || !locked {
		t.Fatalf("acquire test leader lock = %v, %v", locked, err)
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock(113)`)
	adapter, err := queue.NewRiver(pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	settings := app.PumpSettings{PerKeyReadyCap: 1, NodeReadyCap: 1, MediaReadyCap: 1, AIReadyCap: 1, SyncReadyCap: 1, RetryMax: 1, LeaseTimeout: time.Second}
	dispatched, err := (scheduleriver.Store{Pool: pool}).Pump(ctx, settings, fixedCapacity(1), adapter)
	if err != nil || dispatched != 0 {
		t.Fatalf("non-leader Pump() = %d, %v", dispatched, err)
	}
}

func TestPumpRollsBackWhenRiverInsertFails(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("insert-fail-%d", time.Now().UnixNano())
	keyID := seedKey(t, ctx, pool, suffix)
	t.Cleanup(func() { cleanupPumpFixture(t, pool, suffix) })
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,key_id,state) VALUES('sync_person',$1,$2,'pending')`, suffix+"-only", keyID); err != nil {
		t.Fatal(err)
	}
	settings := app.PumpSettings{PerKeyReadyCap: 10, NodeReadyCap: 10, MediaReadyCap: 10, AIReadyCap: 10, SyncReadyCap: 10, RetryMax: 5, RetryBackoff: 30 * time.Second, LeaseTimeout: 2 * time.Minute}
	if _, err := (scheduleriver.Store{Pool: pool}).Pump(ctx, settings, fixedCapacity(1), failingQueue{}); err == nil {
		t.Fatal("Pump ignored failed River insert")
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM work_items WHERE subject_key=$1`, suffix+"-only").Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "pending" {
		t.Fatalf("row state after rollback = %q", state)
	}
}

func TestPumpRollsBackWhenBacklogUpdateFails(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("update-fail-%d", time.Now().UnixNano())
	keyID := seedKey(t, ctx, pool, suffix)
	t.Cleanup(func() { cleanupPumpFixture(t, pool, suffix) })
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,key_id,state) VALUES('sync_person',$1,$2,'pending')`, suffix+"-only", keyID); err != nil {
		t.Fatal(err)
	}
	function := "fail_pump_update_" + fmt.Sprint(time.Now().UnixNano())
	trigger := "trigger_pump_update_" + fmt.Sprint(time.Now().UnixNano())
	statement := fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.subject_key = '%s-only' AND NEW.state = 'ready' THEN RAISE EXCEPTION 'forced backlog update failure'; END IF; RETURN NEW; END $$`, function, suffix)
	if _, err := pool.Exec(ctx, statement); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TRIGGER %s BEFORE UPDATE ON work_items FOR EACH ROW EXECUTE FUNCTION %s()`, trigger, function)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON work_items`, trigger))
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, function))
	})
	settings := app.PumpSettings{PerKeyReadyCap: 10, NodeReadyCap: 10, MediaReadyCap: 10, AIReadyCap: 10, SyncReadyCap: 10, RetryMax: 5, RetryBackoff: 30 * time.Second, LeaseTimeout: 2 * time.Minute}
	adapter, err := queue.NewRiver(pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (scheduleriver.Store{Pool: pool}).Pump(ctx, settings, fixedCapacity(1), adapter); err == nil {
		t.Fatal("Pump ignored failed backlog update")
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM work_items WHERE subject_key=$1`, suffix+"-only").Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "pending" {
		t.Fatalf("row state after update rollback = %q", state)
	}
}

type fixedCapacity int

func (c fixedCapacity) Capacity(context.Context, int64, time.Duration) (int, error) {
	return int(c), nil
}

type failingCapacity struct{}

func (failingCapacity) Capacity(context.Context, int64, time.Duration) (int, error) {
	return 0, errors.New("governor offline")
}

type failingQueue struct{}

func (failingQueue) Enqueue(context.Context, pgx.Tx, queue.Job, queue.Options) error {
	return errors.New("River unavailable")
}
func (failingQueue) Register(string, queue.Handler) error { return nil }

func seedKey(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string) int64 {
	t.Helper()
	var userID, keyID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'unused','user') RETURNING id`, suffix+"@example.test").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,'pump test',$2,'{}',3600,4,5) RETURNING id`, userID, suffix).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	return keyID
}

func cleanupPumpFixture(t *testing.T, pool *pgxpool.Pool, suffix string) {
	t.Helper()
	_, _ = pool.Exec(context.Background(), `DELETE FROM outbox_events WHERE topic='job.dead' AND payload->>'work_item_id' IN (SELECT id::text FROM work_items WHERE subject_key LIKE $1)`, suffix+"-%")
	_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE queue='sync' AND args->'args'->>'subject_key' LIKE $1`, suffix+"-%")
	_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE subject_key LIKE $1`, suffix+"-%")
	_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE email=$1`, suffix+"@example.test")
}

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return dbtest.Open(t, cfg)
}
