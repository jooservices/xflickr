package river

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/platform/queue"
	"github.com/jooservices/xflickr/internal/schedule/app"
	"github.com/jooservices/xflickr/internal/schedule/domain"
)

func TestPumpReturnsBacklogAndInflightReadErrors(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := internalIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	settings := app.PumpSettings{PerKeyReadyCap: 1, NodeReadyCap: 1, MediaReadyCap: 1, AIReadyCap: 1, SyncReadyCap: 1, RetryMax: 1, LeaseTimeout: time.Second}
	store := Store{Pool: pool, listKeys: func(context.Context, pgx.Tx) ([]int64, error) { return nil, errors.New("backlog read") }}
	if _, err := store.Pump(ctx, settings, internalCapacity(1), internalQueue{}); err == nil {
		t.Fatal("Pump ignored pending key query error")
	}
	store = Store{Pool: pool, listKeys: func(context.Context, pgx.Tx) ([]int64, error) { return []int64{1}, nil }, inflight: func(context.Context, pgx.Tx, int64, string) (int, error) { return 0, errors.New("inflight read") }}
	if _, err := store.Pump(ctx, settings, internalCapacity(1), internalQueue{}); err == nil {
		t.Fatal("Pump ignored inflight query error")
	}
}

func TestPumpWarnsAboutKeylessPendingWorkOncePerMinute(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := internalIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	subject := fmt.Sprintf("keyless-warn-%d", time.Now().UnixNano())
	if _, err := pool.Exec(ctx, `WITH u AS (INSERT INTO users(email,password_hash,role) VALUES('workitem-test@example.test','unused','user') ON CONFLICT (email) DO NOTHING RETURNING id) INSERT INTO work_items(kind,subject_key,user_id,state) SELECT 'check',$1,COALESCE((SELECT id FROM u),(SELECT id FROM users WHERE email='workitem-test@example.test')),'pending'`, subject); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE subject_key=$1`, subject) })
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	keylessWarnMu.Lock()
	keylessWarnLast = time.Time{}
	keylessWarnMu.Unlock()
	settings := app.PumpSettings{PerKeyReadyCap: 1, NodeReadyCap: 1, MediaReadyCap: 1, AIReadyCap: 1, SyncReadyCap: 1, RetryMax: 1, LeaseTimeout: time.Second}
	store := Store{Pool: pool, listKeys: func(context.Context, pgx.Tx) ([]int64, error) { return nil, nil }}
	for range 2 {
		if _, err := store.Pump(ctx, settings, internalCapacity(1), internalQueue{}); err != nil {
			t.Fatal(err)
		}
	}
	const message = "pending work cannot be dispatched"
	if got := strings.Count(logs.String(), message); got != 1 || !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "pending_without_key=") {
		t.Fatalf("two pumps logged %d keyless warnings, want exactly 1:\n%s", got, logs.String())
	}
	keylessWarnMu.Lock()
	keylessWarnLast = time.Now().Add(-2 * keylessWarnInterval)
	keylessWarnMu.Unlock()
	if _, err := store.Pump(ctx, settings, internalCapacity(1), internalQueue{}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(logs.String(), message); got != 2 {
		t.Fatalf("pump after the interval logged %d warnings in total, want 2", got)
	}
}

func TestKeylessQueriesReturnDatabaseErrors(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := internalIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := warnKeylessPending(ctx, tx); err == nil {
		t.Fatal("warnKeylessPending ignored the count query error")
	}
	if err := assignOwnerKeys(ctx, tx); err == nil {
		t.Fatal("assignOwnerKeys ignored the update error")
	}
}

func TestWeightsRescoreAdvancesAcrossBatches(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run with XFLICKR_INTEGRATION_TEST=1 to use the worktree database")
	}
	pool := internalIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	prefix := fmt.Sprintf("rescore-batches-%d", time.Now().UnixNano())
	if _, err := tx.Exec(ctx, `INSERT INTO work_items(kind,subject_key,state,score,depth,created_at)
		SELECT 'rescore-test',$1||'-'||g,'pending',0,g,now() FROM generate_series(1,5) AS g`, prefix); err != nil {
		t.Fatal(err)
	}
	store := Store{database: transactionScheduleDatabase{tx: tx}}
	if err := store.StoreWeightsFingerprint(ctx, "stale-for-test"); err != nil {
		t.Fatal(err)
	}
	job := app.WeightsRescore{
		Store: store, BatchSize: 2, MaxDepth: 4,
		Weights: domain.ScoreWeights{PlanWeight: 10, DirectAction: 100, Starred: 20, Depth: 5, AgeMinutes: .1},
	}
	for i, want := range []int{2, 2, 1, 0} {
		got, err := job.RunBatch(ctx)
		if err != nil || got != want {
			t.Fatalf("RunBatch %d = %d, %v; want %d", i+1, got, err, want)
		}
	}
	computed, err := store.ComputeWeightsFingerprint(ctx, job.Weights, job.MaxDepth)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.LoadWeightsFingerprint(ctx)
	if err != nil || stored != computed {
		t.Fatalf("stored fingerprint = %q, %v; want %q", stored, err, computed)
	}
	var unchanged int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM work_items WHERE subject_key LIKE $1 AND score=0`, prefix+"-%").Scan(&unchanged); err != nil {
		t.Fatal(err)
	}
	if unchanged != 0 {
		t.Fatalf("%d pending scores remain stale", unchanged)
	}
}

func TestWeightsRescoreHandlesExpiredSelectionWithMatchingFingerprint(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run with XFLICKR_INTEGRATION_TEST=1 to use the worktree database")
	}
	pool := internalIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	prefix := fmt.Sprintf("expired-selection-%d", time.Now().UnixNano())
	var userID int64
	if err := tx.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'unused','user') RETURNING id`, prefix+"@example.test").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via,selected_until)
		VALUES($1,$2,1,'integration-test',now()-interval '1 minute')`, userID, prefix); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state,direct_action,score,depth,created_at)
		VALUES('person',$1,$2,'pending',TRUE,work_items_static_score(1,TRUE,FALSE,1,now()),1,now())`, prefix, userID); err != nil {
		t.Fatal(err)
	}
	store := Store{database: transactionScheduleDatabase{tx: tx}}
	weights := domain.ScoreWeights{PlanWeight: 10, DirectAction: 100, Starred: 20, Depth: 5, AgeMinutes: .1}
	wantFingerprint, err := store.ComputeWeightsFingerprint(ctx, weights, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.StoreWeightsFingerprint(ctx, wantFingerprint); err != nil {
		t.Fatal(err)
	}
	job := app.WeightsRescore{Store: store, BatchSize: 10, MaxDepth: 4, Weights: weights}
	if got, err := job.RunBatch(ctx); err != nil || got != 1 {
		t.Fatalf("RunBatch for expired selection = %d, %v; want 1", got, err)
	}
	var score, expected float64
	if err := tx.QueryRow(ctx, `SELECT score,work_items_static_score(1,FALSE,FALSE,1,created_at)
		FROM work_items WHERE subject_key=$1`, prefix).Scan(&score, &expected); err != nil {
		t.Fatal(err)
	}
	if score != expected {
		t.Fatalf("expired selection score = %v, want no direct-action bonus %v", score, expected)
	}
}

type transactionScheduleDatabase struct {
	scheduleDatabase
	tx pgx.Tx
}

func (database transactionScheduleDatabase) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	return database.tx.Exec(ctx, query, args...)
}

func (database transactionScheduleDatabase) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	return database.tx.Query(ctx, query, args...)
}

func (database transactionScheduleDatabase) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	return database.tx.QueryRow(ctx, query, args...)
}

type internalCapacity int

func (c internalCapacity) Capacity(context.Context, int64, time.Duration) (int, error) {
	return int(c), nil
}

type internalQueue struct{}

func (internalQueue) Enqueue(context.Context, pgx.Tx, queue.Job, queue.Options) error { return nil }
func (internalQueue) Register(string, queue.Handler) error                            { return nil }

func internalIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return dbtest.Open(t, cfg)
}
