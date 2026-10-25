package postgres_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/schedule/adapters/postgres"
	"github.com/jooservices/xflickr/internal/schedule/domain"
)

func TestBacklogConcurrentDedupeAndLifecycle(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := openBacklogPool(t)
	subject := fmt.Sprintf("integration-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE subject_key = $1`, subject)
	})

	const writers = 12
	start := make(chan struct{})
	errs := make(chan error, writers)
	var wait sync.WaitGroup
	for i := 0; i < writers; i++ {
		wait.Add(1)
		go func(priority int) {
			defer wait.Done()
			<-start
			tx, err := pool.Begin(ctx)
			if err != nil {
				errs <- err
				return
			}
			defer tx.Rollback(context.Background())
			item := domain.WorkItem{Kind: "person", SubjectKey: subject, PriorityBase: priority}
			if err := postgres.Enqueue(ctx, tx, item); err != nil {
				errs <- err
				return
			}
			errs <- tx.Commit(ctx)
		}(i)
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	var id int64
	var state string
	var priority int
	if err := pool.QueryRow(ctx, `SELECT id, state, priority_base FROM work_items WHERE kind = 'person' AND subject_key = $1 AND user_id IS NULL`, subject).Scan(&id, &state, &priority); err != nil {
		t.Fatal(err)
	}
	if state != "pending" || priority != writers-1 {
		t.Fatalf("deduplicated row state=%q priority=%d", state, priority)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM work_items WHERE kind = 'person' AND subject_key = $1 AND user_id IS NULL`, subject).Scan(&count); err != nil || count != 1 {
		t.Fatalf("dedupe row count=%d err=%v", count, err)
	}

	if _, err := pool.Exec(ctx, `UPDATE work_items SET state = 'running' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	advanceTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := postgres.Advance(ctx, advanceTx, id, []byte(`{"page":2}`)); err != nil {
		_ = advanceTx.Rollback(context.Background())
		t.Fatal(err)
	}
	if err := advanceTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var cursor []byte
	if err := pool.QueryRow(ctx, `SELECT state, cursor FROM work_items WHERE id = $1`, id).Scan(&state, &cursor); err != nil {
		t.Fatal(err)
	}
	if state != "pending" || string(cursor) != `{"page": 2}` {
		t.Fatalf("advanced row state=%q cursor=%s", state, cursor)
	}

	if _, err := pool.Exec(ctx, `UPDATE work_items SET state = 'done', attempts = 3, last_error = 'old' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	withoutReopen, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := postgres.Enqueue(ctx, withoutReopen, domain.WorkItem{Kind: "person", SubjectKey: subject, PriorityBase: 1}); err != nil {
		_ = withoutReopen.Rollback(context.Background())
		t.Fatal(err)
	}
	if err := withoutReopen.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT state FROM work_items WHERE id = $1`, id).Scan(&state); err != nil || state != "done" {
		t.Fatalf("done item without reopen state=%q err=%v", state, err)
	}

	reopenTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := postgres.Enqueue(ctx, reopenTx, domain.WorkItem{Kind: "person", SubjectKey: subject, PriorityBase: 1, Reopen: true}); err != nil {
		_ = reopenTx.Rollback(context.Background())
		t.Fatal(err)
	}
	if err := reopenTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT state, attempts, cursor FROM work_items WHERE id = $1`, id).Scan(&state, &attempts, &cursor); err != nil {
		t.Fatal(err)
	}
	if state != "pending" || attempts != 0 || string(cursor) != `{}` {
		t.Fatalf("reopened row state=%q attempts=%d cursor=%s", state, attempts, cursor)
	}
}

func openBacklogPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return dbtest.Open(t, cfg)
}
