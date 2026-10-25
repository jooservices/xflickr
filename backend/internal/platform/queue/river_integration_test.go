package queue_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/platform/queue"
)

func TestRiverEnqueueProcessRollbackRetryDeadAndRouting(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	queues := map[string]int{"crawl@node-" + suffix: 2, "media_" + suffix: 1}
	adapter, err := queue.NewRiver(pool, queues)
	if err != nil {
		t.Fatal(err)
	}
	crawlQueue, mediaQueue := "crawl@node-"+suffix, "media_"+suffix
	processed := make(chan string, 4)
	var attempts atomic.Int32
	for _, kind := range []string{"success_" + suffix, "media_" + suffix, "rollback_" + suffix} {
		kind := kind
		target := crawlQueue
		if strings.HasPrefix(kind, "media_") {
			target = mediaQueue
		}
		if err := adapter.RegisterWithOptions(kind, func(_ context.Context, job queue.Job) error {
			processed <- job.Kind
			return nil
		}, queue.Options{Queue: target, MaxAttempts: 2, LeaseTimeout: 2 * time.Second, Backoff: 10 * time.Millisecond}); err != nil {
			t.Fatal(err)
		}
	}
	deadKind := "dead_" + suffix
	if err := adapter.RegisterWithOptions(deadKind, func(context.Context, queue.Job) error {
		attempts.Add(1)
		return errors.New("permanent failure")
	}, queue.Options{Queue: crawlQueue, MaxAttempts: 2, LeaseTimeout: 2 * time.Second, Backoff: 10 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Start(ctx); err != nil {
		t.Fatalf("start River client: %v", err)
	}
	defer func() {
		if err := adapter.Stop(ctx); err != nil {
			t.Errorf("stop River client: %v", err)
		}
		<-adapter.Stopped()
	}()
	defer cleanupTestJobs(t, pool, suffix)

	rollbackTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Enqueue(ctx, rollbackTx, testJob("rollback_"+suffix), queue.Options{}); err != nil {
		t.Fatalf("enqueue rollback job: %v", err)
	}
	if err := rollbackTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := enqueue(ctx, pool, adapter, testJob("success_"+suffix), queue.Options{}); err != nil {
		t.Fatalf("enqueue rollback sentinel: %v", err)
	}
	select {
	case kind := <-processed:
		if kind != "success_"+suffix {
			t.Fatalf("processed rolled-back job %q before sentinel", kind)
		}
	case <-ctx.Done():
		t.Fatalf("wait for committed rollback sentinel: %v", ctx.Err())
	}
	assertNoRolledBackJob(t, ctx, pool, "rollback_"+suffix)

	if err := enqueue(ctx, pool, adapter, testJob("success_"+suffix), queue.Options{}); err != nil {
		t.Fatalf("enqueue crawl job: %v", err)
	}
	if err := enqueue(ctx, pool, adapter, testJob("media_"+suffix), queue.Options{}); err != nil {
		t.Fatalf("enqueue media job: %v", err)
	}
	want := map[string]bool{"success_" + suffix: false, "media_" + suffix: false}
	for len(want) > 0 {
		select {
		case kind := <-processed:
			delete(want, kind)
		case <-ctx.Done():
			t.Fatalf("wait for routed jobs: %v", ctx.Err())
		}
	}
	assertQueue(t, ctx, pool, "success_"+suffix, "crawl", suffix)
	assertQueue(t, ctx, pool, "media_"+suffix, mediaQueue, "")

	if err := enqueue(ctx, pool, adapter, testJob(deadKind), queue.Options{}); err != nil {
		t.Fatalf("enqueue retry job: %v", err)
	}
	assertEventually(t, ctx, func() (bool, error) {
		var state string
		err := pool.QueryRow(ctx, `SELECT state::text FROM river_job WHERE args->>'kind' = $1`, deadKind).Scan(&state)
		return state == "discarded", err
	})
	if got := attempts.Load(); got != 2 {
		t.Fatalf("failed job attempts = %d, want 2", got)
	}
	assertEventually(t, ctx, func() (bool, error) {
		var count int
		err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE topic = 'job.dead' AND payload->>'kind' = $1`, deadKind).Scan(&count)
		return count == 1, err
	})
	// Simulate the work-end publisher failing before the discarded state was
	// reconciled. The periodic reconciler must restore the missing event.
	if _, err := pool.Exec(ctx, `DELETE FROM outbox_events WHERE topic = 'job.dead' AND payload->>'kind' = $1`, deadKind); err != nil {
		t.Fatalf("remove dead-letter event for reconciliation check: %v", err)
	}
	assertEventually(t, ctx, func() (bool, error) {
		var count int
		err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE topic = 'job.dead' AND payload->>'kind' = $1`, deadKind).Scan(&count)
		return count == 1, err
	})

	rescuedKind := "rescued_" + suffix
	var rescuedID int64
	rescuedArgs, err := json.Marshal(map[string]any{"kind": rescuedKind, "args": map[string]bool{"test": true}, "queue": crawlQueue})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO river_job (state, attempt, max_attempts, finalized_at, args, kind, queue)
		VALUES ('discarded', 3, 3, now(), $1, 'xflickr_job', $2)
		RETURNING id`, rescuedArgs, "q_"+fmt.Sprintf("%x", []byte(crawlQueue))).Scan(&rescuedID); err != nil {
		t.Fatalf("insert rescuer-discarded job: %v", err)
	}
	assertEventually(t, ctx, func() (bool, error) {
		var count int
		err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE topic = 'job.dead' AND key = $1`, fmt.Sprintf("%d", rescuedID)).Scan(&count)
		return count == 1, err
	})
}

func assertNoRolledBackJob(t *testing.T, ctx context.Context, pool *pgxpool.Pool, kind string) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE args->>'kind' = $1`, kind).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rolled-back job %q left %d River rows", kind, count)
	}
}

func testJob(kind string) queue.Job {
	return queue.Job{Kind: kind, Args: json.RawMessage(`{"test":true}`)}
}

func enqueue(ctx context.Context, pool *pgxpool.Pool, adapter *queue.River, job queue.Job, opts queue.Options) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := adapter.Enqueue(ctx, tx, job, opts); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func assertQueue(t *testing.T, ctx context.Context, pool *pgxpool.Pool, kind, expected, suffix string) {
	t.Helper()
	var got string
	if err := pool.QueryRow(ctx, `SELECT queue FROM river_job WHERE args->>'kind' = $1`, kind).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if suffix != "" {
		expected = "q_" + fmt.Sprintf("%x", []byte(expected+"@node-"+suffix))
	}
	if got != expected {
		t.Fatalf("job %s queue = %q, want %q", kind, got, expected)
	}
}

func assertEventually(t *testing.T, ctx context.Context, check func() (bool, error)) {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		ok, err := check()
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("condition not met before deadline: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}

func cleanupTestJobs(t *testing.T, pool *pgxpool.Pool, suffix string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = pool.Exec(ctx, `DELETE FROM outbox_events WHERE topic = 'job.dead' AND payload->>'kind' LIKE $1`, "%_"+suffix)
		_, _ = pool.Exec(ctx, `DELETE FROM river_job WHERE args->>'kind' LIKE $1`, "%_"+suffix)
	})
}

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate queue integration test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return dbtest.Open(t, cfg)
}
