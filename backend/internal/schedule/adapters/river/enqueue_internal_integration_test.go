package river

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestPlanBacklogCountIsBoundedAndPlanAware(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run with XFLICKR_INTEGRATION_TEST=1 to use the worktree database")
	}
	pool := internalIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	suffix := fmt.Sprintf("plan-backlog-%d", time.Now().UnixNano())
	var freePlanID, proPlanID int64
	for name, target := range map[string]*int64{"free": &freePlanID, "pro": &proPlanID} {
		if err := tx.QueryRow(ctx, `SELECT id FROM plans WHERE name=$1`, name).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	var freeUserID, proUserID, keyID int64
	if err := tx.QueryRow(ctx, `INSERT INTO users(email,password_hash,role,plan_id) VALUES($1,'unused','user',$2) RETURNING id`, suffix+"-free@example.test", freePlanID).Scan(&freeUserID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO users(email,password_hash,role,plan_id) VALUES($1,'unused','user',$2) RETURNING id`, suffix+"-pro@example.test", proPlanID).Scan(&proUserID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,'backlog test',$2,'{}',3600,4,5) RETURNING id`, freeUserID, suffix).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	var indexBytesBefore int64
	if err := tx.QueryRow(ctx, `SELECT pg_relation_size('work_items_pending_plan_backlog_idx')`).Scan(&indexBytesBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,key_id,state,score)
		SELECT 'sync_person',$1||'-'||g,$2,$3,'pending',400000-g
		FROM generate_series(1,400000) AS g`, suffix, proUserID, keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `ANALYZE work_items`); err != nil {
		t.Fatal(err)
	}

	plan, err := explainStatement(ctx, tx, planBacklogSQL, keyID, "sync", 100)
	if err != nil {
		t.Fatal(err)
	}
	sharedBuffers := plan.Plan.SharedHitBlocks + plan.Plan.SharedReadBlocks
	var indexBytesAfter int64
	if err := tx.QueryRow(ctx, `SELECT pg_relation_size('work_items_pending_plan_backlog_idx')`).Scan(&indexBytesAfter); err != nil {
		t.Fatal(err)
	}
	indexGrowth := indexBytesAfter - indexBytesBefore
	t.Logf("after: execution_ms=%.2f shared_buffers=%d work_items_seq_scan=%t backlog_index_growth_bytes=%d", plan.ExecutionTime, sharedBuffers, containsWorkItemsSeqScan(plan.Plan), indexGrowth)
	if containsWorkItemsSeqScan(plan.Plan) {
		t.Errorf("plan backlog count performs a Seq Scan on work_items")
	}
	if !containsWorkItemsBacklogIndex(plan.Plan) {
		t.Errorf("plan backlog count does not use work_items_pending_plan_backlog_idx")
	}
	if sharedBuffers >= 500 {
		t.Errorf("plan backlog count touched %d buffers, want fewer than 500", sharedBuffers)
	}
	if indexGrowth >= 64<<20 {
		t.Errorf("400,000 pending rows grew the plan backlog index by %d bytes, want less than 64 MiB", indexGrowth)
	}
	groups, err := readPlanBacklog(ctx, tx, planBacklogSQL, keyID, "sync", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].ID != proPlanID || groups[0].Available != 100 {
		t.Errorf("empty free plan or capped pro backlog groups = %+v, want only pro capped at 100", groups)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,key_id,state,score) VALUES('sync_person',$1,$2,$3,'pending',-1)`, suffix+"-late-free", freeUserID, keyID); err != nil {
		t.Fatal(err)
	}
	groups, err = readPlanBacklog(ctx, tx, planBacklogSQL, keyID, "sync", 100)
	if err != nil {
		t.Fatal(err)
	}
	allocations, err := app.SplitShare(100, groups)
	if err != nil {
		t.Fatal(err)
	}
	if allocations[freePlanID] != 1 || allocations[proPlanID] != 99 {
		t.Errorf("allocations for late free work = %v, want free=1 and pro=99", allocations)
	}
}

type explainDocument struct {
	Plan          explainNode `json:"Plan"`
	ExecutionTime float64     `json:"Execution Time"`
}

type explainNode struct {
	NodeType         string        `json:"Node Type"`
	RelationName     string        `json:"Relation Name"`
	IndexName        string        `json:"Index Name"`
	SharedHitBlocks  int           `json:"Shared Hit Blocks"`
	SharedReadBlocks int           `json:"Shared Read Blocks"`
	Plans            []explainNode `json:"Plans"`
}

func explainStatement(ctx context.Context, tx pgx.Tx, query string, args ...any) (explainDocument, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) `+query, args...).Scan(&raw); err != nil {
		return explainDocument{}, err
	}
	var documents []explainDocument
	if err := json.Unmarshal(raw, &documents); err != nil {
		return explainDocument{}, err
	}
	if len(documents) != 1 {
		return explainDocument{}, fmt.Errorf("EXPLAIN returned %d documents, want one", len(documents))
	}
	return documents[0], nil
}

func containsWorkItemsSeqScan(node explainNode) bool {
	if strings.Contains(node.NodeType, "Seq Scan") && node.RelationName == "work_items" {
		return true
	}
	for _, child := range node.Plans {
		if containsWorkItemsSeqScan(child) {
			return true
		}
	}
	return false
}

func containsWorkItemsBacklogIndex(node explainNode) bool {
	if node.RelationName == "work_items" && node.IndexName == "work_items_pending_plan_backlog_idx" {
		return true
	}
	for _, child := range node.Plans {
		if containsWorkItemsBacklogIndex(child) {
			return true
		}
	}
	return false
}

func readPlanBacklog(ctx context.Context, tx pgx.Tx, query string, args ...any) ([]app.ShareGroup, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := make([]app.ShareGroup, 0)
	for rows.Next() {
		var group app.ShareGroup
		if err := rows.Scan(&group.ID, &group.Share, &group.Available); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

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
