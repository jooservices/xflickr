package river_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	crawlpostgres "github.com/jooservices/xflickr/internal/crawl/adapters/postgres"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/platform/queue"
	quotaapp "github.com/jooservices/xflickr/internal/quota/app"
	scheduleriver "github.com/jooservices/xflickr/internal/schedule/adapters/river"
	"github.com/jooservices/xflickr/internal/schedule/app"
	"github.com/jooservices/xflickr/internal/schedule/domain"
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

func TestPumpSkipsUnavailableKeyAndDispatchesValidKey(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("unavailable-%d", time.Now().UnixNano())
	unavailableID := seedKey(t, ctx, pool, suffix+"-revoked")
	validID := seedKey(t, ctx, pool, suffix+"-valid")
	t.Cleanup(func() {
		cleanupPumpFixture(t, pool, suffix)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE email = ANY($1)`, []string{suffix + "-revoked@example.test", suffix + "-valid@example.test"})
	})
	if _, err := pool.Exec(ctx, `UPDATE api_keys SET status='revoked' WHERE id=$1`, unavailableID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,key_id,state,priority_base) VALUES ('sync_person',$1,$2,'pending',2),('sync_person',$3,$4,'pending',1)`, suffix+"-revoked-row", unavailableID, suffix+"-valid-row", validID); err != nil {
		t.Fatal(err)
	}
	adapter, err := queue.NewRiver(pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	settings := app.PumpSettings{PerKeyReadyCap: 10, NodeReadyCap: 10, MediaReadyCap: 10, AIReadyCap: 10, SyncReadyCap: 10, RetryMax: 5, RetryBackoff: 30 * time.Second, LeaseTimeout: 2 * time.Minute}
	dispatched, err := (scheduleriver.Store{Pool: pool}).Pump(ctx, settings, unavailableKeyCapacity{unavailableID: unavailableID}, adapter)
	if err != nil {
		t.Fatal(err)
	}
	if dispatched != 1 {
		t.Fatalf("Pump() dispatched %d rows, want valid key's one row", dispatched)
	}
	var revokedState, validState string
	if err := pool.QueryRow(ctx, `SELECT state FROM work_items WHERE subject_key=$1`, suffix+"-revoked-row").Scan(&revokedState); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT state FROM work_items WHERE subject_key=$1`, suffix+"-valid-row").Scan(&validState); err != nil {
		t.Fatal(err)
	}
	if revokedState != "pending" || validState != "ready" {
		t.Fatalf("row states: revoked=%q valid=%q; want pending and ready", revokedState, validState)
	}
}

func TestPumpDoesNotDispatchPausedContact(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("paused-%d", time.Now().UnixNano())
	keyID := seedKey(t, ctx, pool, suffix)
	t.Cleanup(func() { cleanupPumpFixture(t, pool, suffix) })
	var userID int64
	if err := pool.QueryRow(ctx, `SELECT user_id FROM api_keys WHERE id=$1`, keyID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via,paused_at) VALUES($1,$2,1,'contact',now())`, userID, suffix+"-contact"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,key_id,state) VALUES('person',$1,$2,$3,'pending')`, suffix+"-contact", userID, keyID); err != nil {
		t.Fatal(err)
	}
	adapter, err := queue.NewRiver(pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	settings := app.PumpSettings{PerKeyReadyCap: 10, NodeReadyCap: 10, MediaReadyCap: 10, AIReadyCap: 10, SyncReadyCap: 10, RetryMax: 5, RetryBackoff: 30 * time.Second, LeaseTimeout: 2 * time.Minute}
	dispatched, err := (scheduleriver.Store{Pool: pool}).Pump(ctx, settings, fixedCapacity(10), adapter)
	if err != nil {
		t.Fatal(err)
	}
	if dispatched != 0 {
		t.Fatalf("Pump dispatched %d paused rows, want 0", dispatched)
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM work_items WHERE subject_key=$1`, suffix+"-contact").Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "pending" {
		t.Fatalf("paused backlog state=%q, want pending", state)
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
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,state,attempts,lease_expires_at) VALUES('sync_person',$1,'running',0,now()-interval '1 second')`, suffix); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupPumpFixture(t, pool, suffix) })
	store := scheduleriver.Store{Pool: pool}
	if count, err := store.Reclaim(ctx, 5, 30*time.Second, time.Now()); err != nil || count < 1 {
		t.Fatalf("Reclaim() = %d, %v; want at least the expired fixture", count, err)
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

func TestFailedWorkIsSelfHealedAndRequeued(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("heal-%d", time.Now().UnixNano())
	for name, stale := range map[string]string{"-old": "1 hour", "-new": "1 minute"} {
		if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,state,attempts,last_error,updated_at) VALUES('sync_person',$1,'failed',5,'lease expired',now()-$2::interval)`, suffix+name, stale); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,state,attempts,last_error,updated_at) VALUES('sync_person',$1,'failed',5,'flickr 5xx',now()-interval '1 hour')`, suffix+"-other"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupPumpFixture(t, pool, suffix) })
	state := func(name string) (string, int) {
		var st string
		var attempts int
		if err := pool.QueryRow(ctx, `SELECT state,attempts FROM work_items WHERE subject_key=$1`, suffix+name).Scan(&st, &attempts); err != nil {
			t.Fatal(err)
		}
		return st, attempts
	}
	store := scheduleriver.Store{Pool: pool}
	if _, err := store.Reclaim(ctx, 5, 30*time.Second, time.Now()); err != nil {
		t.Fatal(err)
	}
	if st, a := state("-old"); st != "pending" || a != 0 {
		t.Fatalf("old lease-expired row state=%s attempts=%d, want pending/0", st, a)
	}
	if st, _ := state("-new"); st != "failed" {
		t.Fatalf("recent failure state=%s, want failed (backoff)", st)
	}
	if st, _ := state("-other"); st != "failed" {
		t.Fatalf("non-lease failure state=%s, want failed", st)
	}
	var id int64
	if err := pool.QueryRow(ctx, `SELECT id FROM work_items WHERE subject_key=$1`, suffix+"-other").Scan(&id); err != nil {
		t.Fatal(err)
	}
	if n, err := store.Requeue(ctx, []int64{id}, time.Now()); err != nil || n != 1 {
		t.Fatalf("Requeue(id) = %d, %v", n, err)
	}
	if st, a := state("-other"); st != "pending" || a != 0 {
		t.Fatalf("requeued row state=%s attempts=%d", st, a)
	}
	if n, err := store.Requeue(ctx, nil, time.Now()); err != nil || n < 1 {
		t.Fatalf("Requeue(all) = %d, %v", n, err)
	}
	if st, _ := state("-new"); st != "pending" {
		t.Fatalf("requeue-all left row state=%s", st)
	}
}

func TestReclaimLeavesItemWithLiveRiverJobAlone(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("lease-job-%d", time.Now().UnixNano())
	keyID := seedKey(t, ctx, pool, suffix)
	t.Cleanup(func() { cleanupPumpFixture(t, pool, suffix) })
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,key_id,state,lease_expires_at) VALUES('sync_person',$1,$2,'ready',now()-interval '1 second')`, suffix, keyID); err != nil {
		t.Fatal(err)
	}
	adapter, err := queue.NewRiver(pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	var workID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM work_items WHERE subject_key=$1`, suffix).Scan(&workID); err != nil {
		t.Fatal(err)
	}
	args := []byte(fmt.Sprintf(`{"work_item_id":%d,"kind":"sync_person","subject_key":%q}`, workID, suffix))
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Enqueue(ctx, tx, queue.Job{Kind: "sync_person", Args: args}, queue.Options{Queue: "sync", MaxAttempts: 5, LeaseTimeout: time.Minute}); err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	store := scheduleriver.Store{Pool: pool}
	// The job still waits in the queue: the expired lease must not be reaped
	// and the River job must stay live.
	if _, err := store.Reclaim(ctx, 5, 30*time.Second, time.Now()); err != nil {
		t.Fatal(err)
	}
	var state string
	var attempts int
	var jobState string
	if err := pool.QueryRow(ctx, `SELECT w.state, w.attempts, (SELECT j.state FROM river_job j WHERE j.args->'args'->>'work_item_id'=$1) FROM work_items w WHERE w.id=$1::bigint`, fmt.Sprint(workID)).Scan(&state, &attempts, &jobState); err != nil {
		t.Fatal(err)
	}
	if state != "ready" || attempts != 0 || jobState != "available" {
		t.Fatalf("live job: item state=%s attempts=%d job=%s, want ready/0/available", state, attempts, jobState)
	}
	// Once the job is no longer live the lease is reaped as before.
	if _, err := pool.Exec(ctx, `UPDATE river_job SET state='cancelled', finalized_at=now() WHERE args->'args'->>'work_item_id'=$1`, fmt.Sprint(workID)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reclaim(ctx, 5, 30*time.Second, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT state, attempts FROM work_items WHERE id=$1`, workID).Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "pending" || attempts != 1 {
		t.Fatalf("orphan lease: item state=%s attempts=%d, want pending/1", state, attempts)
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
	if settings.RetryMax != 5 || settings.RetryBackoff != 30*time.Second || settings.LeaseTimeout != 2*time.Minute || settings.PerKeyReadyCap != 100 || settings.AgingBatchSize != 500 || settings.MaxDepth != 2 || settings.ScoreWeights.PlanWeight != 10 || settings.ScoreWeights.AgeMinutes != .1 {
		t.Fatalf("pump settings = %+v", settings)
	}
}

func TestRecomputeScoresStoresFormulaResultInBatches(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	ctx := t.Context()
	suffix := fmt.Sprintf("score-%d", time.Now().UnixNano())
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,state,direct_action,starred,depth,created_at) VALUES('sync_person',$1,'pending',TRUE,TRUE,1,now()-interval '10 minutes')`, suffix); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupPumpFixture(t, pool, suffix) })
	store := scheduleriver.Store{Pool: pool}
	weights := domain.ScoreWeights{PlanWeight: 10, DirectAction: 100, Starred: 20, Depth: 5, AgeMinutes: .1}
	// Other tests or application work may leave older pending rows in the shared
	// worktree database. Keep the batch size small and let the rotating aging
	// query reach this fixture instead of assuming it is the only eligible row.
	for attempts := 0; attempts < 1000; attempts++ {
		count, err := store.RecomputeScores(ctx, 1, weights, 4)
		if err != nil {
			t.Fatalf("RecomputeScores() error: %v", err)
		}
		var updated bool
		if err := pool.QueryRow(ctx, `SELECT score_updated_at IS NOT NULL FROM work_items WHERE subject_key=$1`, suffix).Scan(&updated); err != nil {
			t.Fatal(err)
		}
		if updated {
			if count != 1 {
				t.Fatalf("RecomputeScores() count = %d after updating fixture", count)
			}
			break
		}
		if count == 0 {
			t.Fatal("RecomputeScores() found no rows before reaching fixture")
		}
		if attempts == 999 {
			t.Fatal("RecomputeScores() did not reach fixture within 1000 batches")
		}
	}
	var score float64
	if err := pool.QueryRow(ctx, `SELECT score FROM work_items WHERE subject_key=$1`, suffix).Scan(&score); err != nil {
		t.Fatal(err)
	}
	if score < 145.9 || score > 146.1 {
		t.Fatalf("stored score = %v, want about 146", score)
	}
}

func TestRecomputeScoresStopsSelectionBonusAfterSevenDays(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("selection-expiry-%d", time.Now().UnixNano())
	keyID := seedKey(t, ctx, pool, suffix)
	t.Cleanup(func() { cleanupPumpFixture(t, pool, suffix) })
	var userID int64
	if err := pool.QueryRow(ctx, `SELECT user_id FROM api_keys WHERE id=$1`, keyID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		suffix string
		until  time.Time
	}{{"active", time.Now().Add(time.Hour)}, {"expired", time.Now().Add(-time.Hour)}} {
		subject := suffix + "-" + item.suffix
		if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via,selected_until) VALUES($1,$2,0,'contact',$3)`, userID, subject, item.until); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,key_id,state,direct_action,created_at) VALUES('person',$1,$2,$3,'pending',true,now())`, subject, userID, keyID); err != nil {
			t.Fatal(err)
		}
	}
	store := scheduleriver.Store{Pool: pool}
	if _, err := store.RecomputeScores(ctx, 100, domain.ScoreWeights{DirectAction: 100}, 0); err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		suffix string
		score  float64
	}{{"active", 100}, {"expired", 0}} {
		var score float64
		if err := pool.QueryRow(ctx, `SELECT score FROM work_items WHERE subject_key=$1`, suffix+"-"+want.suffix).Scan(&score); err != nil {
			t.Fatal(err)
		}
		if score != want.score {
			t.Errorf("%s selection score=%v want %v", want.suffix, score, want.score)
		}
	}
}

func TestRescorePersonReordersBacklog(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("rescore-person-%d", time.Now().UnixNano())
	keyID := seedKey(t, ctx, pool, suffix)
	t.Cleanup(func() { cleanupPumpFixture(t, pool, suffix) })
	var userID int64
	if err := pool.QueryRow(ctx, `SELECT user_id FROM api_keys WHERE id=$1`, keyID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	for _, subject := range []string{suffix + "-a", suffix + "-b"} {
		if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via) VALUES($1,$2,0,'contact')`, userID, subject); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,key_id,state,score,created_at) VALUES('person',$1,$2,$3,'pending',5,now())`, subject, userID, keyID); err != nil {
			t.Fatal(err)
		}
	}
	// Star and select "b" the way the controls adapter does, then rescore only it.
	if _, err := pool.Exec(ctx, `UPDATE crawl_interests SET starred=TRUE,selected_until=now()+interval '7 days' WHERE user_id=$1 AND person_nsid=$2`, userID, suffix+"-b"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE work_items SET starred=TRUE,direct_action=TRUE,score_updated_at=NULL WHERE user_id=$1 AND subject_key=$2`, userID, suffix+"-b"); err != nil {
		t.Fatal(err)
	}
	weights := domain.ScoreWeights{DirectAction: 100, Starred: 20}
	count, err := (scheduleriver.Store{Pool: pool}).RescorePerson(ctx, userID, suffix+"-b", weights, 0)
	if err != nil || count != 1 {
		t.Fatalf("RescorePerson() count=%d err=%v", count, err)
	}
	var a, b float64
	if err := pool.QueryRow(ctx, `SELECT (SELECT score FROM work_items WHERE subject_key=$1),(SELECT score FROM work_items WHERE subject_key=$2)`, suffix+"-a", suffix+"-b").Scan(&a, &b); err != nil {
		t.Fatal(err)
	}
	if a != 5 || b != 120 {
		t.Fatalf("scores a=%v b=%v, want 5 and 120", a, b)
	}
	var first string
	if err := pool.QueryRow(ctx, `SELECT subject_key FROM work_items WHERE user_id=$1 AND state='pending' ORDER BY score DESC, not_before, id LIMIT 1`, userID).Scan(&first); err != nil || first != suffix+"-b" {
		t.Fatalf("dispatch order first=%q err=%v", first, err)
	}
}

func TestRecomputeScoresReturnsInvalidScoreError(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("score-invalid-%d", time.Now().UnixNano())
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,state) VALUES('person',$1,'pending')`, suffix); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE subject_key=$1`, suffix) })
	if _, err := (scheduleriver.Store{Pool: pool}).RecomputeScores(ctx, 100, domain.ScoreWeights{PlanWeight: math.NaN()}, 2); err == nil {
		t.Fatal("RecomputeScores accepted an invalid score weight")
	}
}

func TestRecomputeScoresReturnsBacklogWriteFailure(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("score-write-failure-%d", time.Now().UnixNano())
	function := "fail_score_" + fmt.Sprint(time.Now().UnixNano())
	trigger := "trigger_score_" + fmt.Sprint(time.Now().UnixNano())
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,state) VALUES('person',$1,'pending')`, suffix); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE subject_key=$1`, suffix) })
	statement := fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.subject_key = '%s' AND NEW.score_updated_at IS NOT NULL THEN RAISE EXCEPTION 'forced score write failure'; END IF; RETURN NEW; END $$`, function, suffix)
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
	if _, err := (scheduleriver.Store{Pool: pool}).RecomputeScores(ctx, 100, domain.ScoreWeights{}, 2); err == nil {
		t.Fatal("RecomputeScores ignored a backlog score write failure")
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

type unavailableKeyCapacity struct{ unavailableID int64 }

func (c unavailableKeyCapacity) Capacity(_ context.Context, keyID int64, _ time.Duration) (int, error) {
	if keyID == c.unavailableID {
		return 0, quotaapp.ErrKeyUnavailable
	}
	return 1, nil
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

func TestPumpAssignsOwnerKeyToKeylessPendingWork(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("keyless-%d", time.Now().UnixNano())
	keyID := seedKey(t, ctx, pool, suffix)
	t.Cleanup(func() { cleanupPumpFixture(t, pool, suffix) })
	if _, err := pool.Exec(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed) SELECT user_id,id,$1,'owner','{}','{}' FROM api_keys WHERE id=$2`, suffix+"@N00", keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state) SELECT 'check',$1 || '-owned',user_id,'pending' FROM api_keys WHERE id=$2`, suffix, keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,state) VALUES('check',$1,'pending')`, suffix+"-orphan"); err != nil {
		t.Fatal(err)
	}
	adapter, err := queue.NewRiver(pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	settings := app.PumpSettings{PerKeyReadyCap: 10, NodeReadyCap: 1000, MediaReadyCap: 10, AIReadyCap: 10, SyncReadyCap: 10, RetryMax: 5, RetryBackoff: 30 * time.Second, LeaseTimeout: 2 * time.Minute}
	store := scheduleriver.Store{Pool: pool}
	if n, err := store.Pump(ctx, settings, fixedCapacity(0), adapter); err != nil || n != 0 {
		t.Fatalf("Pump() without quota = %d, %v", n, err)
	}
	var assigned int64
	var state string
	if err := pool.QueryRow(ctx, `SELECT key_id, state FROM work_items WHERE subject_key=$1`, suffix+"-owned").Scan(&assigned, &state); err != nil || assigned != keyID || state != "pending" {
		t.Fatalf("owned row key=%d state=%s err=%v", assigned, state, err)
	}
	if n, err := store.Pump(ctx, settings, fixedCapacity(10), adapter); err != nil || n < 1 {
		t.Fatalf("Pump() with quota = %d, %v", n, err)
	}
	if err := pool.QueryRow(ctx, `SELECT state FROM work_items WHERE subject_key=$1`, suffix+"-owned").Scan(&state); err != nil || state != "ready" {
		t.Fatalf("owned row state=%s err=%v", state, err)
	}
	var orphanKey *int64
	if err := pool.QueryRow(ctx, `SELECT key_id FROM work_items WHERE subject_key=$1`, suffix+"-orphan").Scan(&orphanKey); err != nil || orphanKey != nil {
		t.Fatalf("orphan row key=%v err=%v", orphanKey, err)
	}
}

// logCapacity models the governor: the hourly budget minus the 100-call
// reserve, less every call already in api_call_log.
type logCapacity struct {
	pool   *pgxpool.Pool
	hourly int
}

func (c logCapacity) Capacity(ctx context.Context, keyID int64, _ time.Duration) (int, error) {
	var used int
	if err := c.pool.QueryRow(ctx, `SELECT count(*) FROM api_call_log WHERE key_id=$1 AND called_at > now() - interval '1 hour'`, keyID).Scan(&used); err != nil {
		return 0, err
	}
	return max(0, c.hourly-100-used), nil
}

type fakeFlickrClient struct{ calls int }

func (f *fakeFlickrClient) Call(string) { f.calls++ }

func TestConnectedAccountCrawlRunsCycleThenPumpThenWorkerWithinQuota(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("e2e-%d", time.Now().UnixNano())
	keyID := seedKey(t, ctx, pool, suffix)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM api_call_log WHERE key_id=$1`, keyID)
		cleanupPumpFixture(t, pool, suffix)
	})
	// Connect the account: an ok Flickr account on the key, and the owner as the depth-0 interest.
	person := suffix + "-owner"
	if _, err := pool.Exec(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed) SELECT user_id,id,$1,'owner','{}','{}' FROM api_keys WHERE id=$2`, suffix+"@N00", keyID); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err := pool.QueryRow(ctx, `SELECT user_id FROM api_keys WHERE id=$1`, keyID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via) VALUES($1,$2,0,'connected_account')`, userID, person); err != nil {
		t.Fatal(err)
	}
	// A second item of the same account is waiting, so quota is the only limit.
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,key_id,state) VALUES('check',$1,$2,$3,'pending')`, suffix+"-second", userID, keyID); err != nil {
		t.Fatal(err)
	}

	// Run a cycle: the new check item must carry the account's key.
	cycles, err := crawlpostgres.NewCycleRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	if queued, err := cycles.ScheduleNext(ctx, userID, time.Now().UTC(), 24*time.Hour); err != nil || !queued {
		t.Fatalf("ScheduleNext() = %v, %v; want queued", queued, err)
	}
	var cycleKey *int64
	var cycleState string
	if err := pool.QueryRow(ctx, `SELECT key_id, state FROM work_items WHERE user_id=$1 AND subject_key=$2`, userID, person).Scan(&cycleKey, &cycleState); err != nil || cycleKey == nil || *cycleKey != keyID || cycleState != "pending" {
		t.Fatalf("cycle item key=%v state=%s err=%v, want key %d pending", cycleKey, cycleState, err, keyID)
	}

	// Leave exactly one call of the hourly budget (3600 - 100 reserve) unused.
	const hourly = 3600
	if _, err := pool.Exec(ctx, `INSERT INTO api_call_log(key_id,class) SELECT $1,'person' FROM generate_series(1,$2)`, keyID, hourly-100-1); err != nil {
		t.Fatal(err)
	}
	adapter, err := queue.NewRiver(pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	settings := app.PumpSettings{PerKeyReadyCap: 10, NodeReadyCap: 1000, MediaReadyCap: 10, AIReadyCap: 10, SyncReadyCap: 10, RetryMax: 5, RetryBackoff: 30 * time.Second, LeaseTimeout: 2 * time.Minute}
	store := scheduleriver.Store{Pool: pool}
	capacity := logCapacity{pool: pool, hourly: hourly}
	dispatched, err := store.Pump(ctx, settings, capacity, adapter)
	if err != nil || dispatched != 1 {
		t.Fatalf("Pump() = %d, %v; want exactly the one call of remaining quota", dispatched, err)
	}
	var ready, jobs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE state='ready') FROM work_items WHERE user_id=$1`, userID).Scan(&ready); err != nil || ready != 1 {
		t.Fatalf("ready items = %d, %v; want 1", ready, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE queue='sync' AND args->'args'->>'subject_key' LIKE $1`, suffix+"-%").Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("River jobs = %d, %v; want 1", jobs, err)
	}

	// Worker: run each ready item against the fake Flickr and log the call.
	flickr := &fakeFlickrClient{}
	rows, err := pool.Query(ctx, `SELECT id FROM work_items WHERE user_id=$1 AND state='ready'`, userID)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		flickr.Call("flickr.people.getInfo")
		if _, err := pool.Exec(ctx, `INSERT INTO api_call_log(key_id,class) VALUES($1,'person')`, keyID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE work_items SET state='done' WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
	}
	var logged int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM api_call_log WHERE key_id=$1`, keyID).Scan(&logged); err != nil || flickr.calls != 1 || logged != hourly-100 {
		t.Fatalf("flickr calls=%d api_call_log rows=%d err=%v; want 1 call and %d rows", flickr.calls, logged, err, hourly-100)
	}

	// Quota is spent: the remaining item stays pending until the window resets.
	if dispatched, err := store.Pump(ctx, settings, capacity, adapter); err != nil || dispatched != 0 {
		t.Fatalf("Pump() with exhausted quota = %d, %v; want 0", dispatched, err)
	}
	var pending int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM work_items WHERE user_id=$1 AND state='pending'`, userID).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("pending after exhausted quota = %d, %v; want 1", pending, err)
	}
}
