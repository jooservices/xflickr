package queuepostgres_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/schedule/adapters/queuepostgres"
	"github.com/jooservices/xflickr/migrations"
)

const oldestAgeSQL = `SELECT COALESCE(GREATEST(0, floor(extract(epoch FROM now()-min(created_at)))), 0)::bigint
FROM work_items
WHERE state IN ('pending', 'ready', 'running', 'waiting');`
const oldestIndex = "work_items_active_created_at_idx"
const oldestMigration = "20261112120300_work_items_active_created_at_index.sql"

func oldestConfig(t *testing.T) config.DBConfig {
	t.Helper()
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 for isolated PostgreSQL tests")
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test")
	}
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(filepath.Dir(source), "../../../../../.env"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name == "xflickr_test" {
		t.Fatal("a private worktree database is required")
	}
	return dbtest.EmptyDatabase(t, cfg)
}

func TestOldestActiveAgeIntegration(t *testing.T) {
	pool := dbtest.Open(t, oldestConfig(t))
	for _, state := range []string{"pending", "ready", "running", "waiting"} {
		t.Run(state, func(t *testing.T) {
			oldestExec(t, pool, `DELETE FROM work_items`)
			oldestExec(t, pool, `INSERT INTO work_items(kind,subject_key,state,created_at) VALUES
    ('person','done','done',now()-interval '1 day'),
    ('person','failed','failed',now()-interval '2 days'),
    ('person','newer','pending',now()-interval '10 seconds')`)
			_, err := pool.Exec(t.Context(), `INSERT INTO work_items(kind,subject_key,state,created_at)
    VALUES('person','oldest',$1,now()-interval '120 seconds')`, state)
			if err != nil {
				t.Fatal(err)
			}
			assertOldestAge(t, pool, 120)
		})
	}
	t.Run("empty", func(t *testing.T) {
		oldestExec(t, pool, `DELETE FROM work_items`)
		assertOldestAge(t, pool, 0)
	})
	t.Run("inactive only", func(t *testing.T) {
		oldestExec(t, pool, `INSERT INTO work_items(kind,subject_key,state,created_at)
   VALUES('person','inactive','done',now()-interval '1 day')`)
		assertOldestAge(t, pool, 0)
	})
	t.Run("future", func(t *testing.T) {
		oldestExec(t, pool, `INSERT INTO work_items(kind,subject_key,state,created_at)
   VALUES('person','future','waiting',now()+interval '1 day')`)
		assertOldestAge(t, pool, 0)
	})
}

func assertOldestAge(t *testing.T, pool *pgxpool.Pool, want int64) {
	t.Helper()
	got, err := queuepostgres.New(pool).AdminQueue(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.OldestItemAgeSeconds < want || got.OldestItemAgeSeconds > want+1 {
		t.Fatalf("oldest age = %d; want %d..%d", got.OldestItemAgeSeconds, want, want+1)
	}
	if want == 0 && got.OldestItemAgeSeconds != 0 {
		t.Fatal("empty or future age must be exactly zero")
	}
}

func oldestExec(t *testing.T, pool *pgxpool.Pool, query string) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), query); err != nil {
		t.Fatal(err)
	}
}

func TestOldestActiveAgeIndexPlanIntegration(t *testing.T) {
	if os.Getenv("XFLICKR_INDEX_PLAN_TEST") != "1" {
		t.Skip("set XFLICKR_INDEX_PLAN_TEST=1 for the separate 400,000-row measurement")
	}
	pool := dbtest.Open(t, oldestConfig(t))
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tx.Rollback(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	_, err = tx.Exec(ctx, `INSERT INTO work_items(kind,subject_key,state,created_at)
 SELECT 'person',g::text,(ARRAY['pending','ready','running','waiting','done','failed'])[1+g%6],
 now()-g*interval '1 second' FROM generate_series(1,400000) g`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `ANALYZE work_items`); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := tx.QueryRow(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) `+oldestAgeSQL).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var plans []oldestExplain
	if err := json.Unmarshal(raw, &plans); err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected one plan, got %d", len(plans))
	}
	if !checkOldestPlan(t, plans[0].Plan) {
		t.Fatalf("missing %s: %s", oldestIndex, raw)
	}
	t.Logf("rows=400000 execution_ms=%.3f shared_hit=%d shared_read=%d plan=%s",
		plans[0].ExecutionTime, plans[0].Plan.SharedHit, plans[0].Plan.SharedRead, raw)
}

type oldestExplain struct {
	Plan          oldestPlanNode `json:"Plan"`
	ExecutionTime float64        `json:"Execution Time"`
}

type oldestPlanNode struct {
	NodeType   string           `json:"Node Type"`
	Relation   string           `json:"Relation Name"`
	Index      string           `json:"Index Name"`
	Rows       float64          `json:"Actual Rows"`
	Loops      float64          `json:"Actual Loops"`
	SharedHit  int              `json:"Shared Hit Blocks"`
	SharedRead int              `json:"Shared Read Blocks"`
	Plans      []oldestPlanNode `json:"Plans"`
}

func checkOldestPlan(t *testing.T, node oldestPlanNode) bool {
	t.Helper()
	if node.Relation == "work_items" && strings.Contains(node.NodeType, "Seq Scan") {
		t.Error("age query sequentially scans work_items")
	}
	found := node.Index == oldestIndex
	if found && node.Rows*node.Loops > 1 {
		t.Errorf("age index returned %v rows", node.Rows*node.Loops)
	}
	for _, child := range node.Plans {
		if checkOldestPlan(t, child) {
			found = true
		}
	}
	return found
}

func TestOldestActiveAgeIndexMigrationIntegration(t *testing.T) {
	cfg := oldestConfig(t)
	pool := dbtest.Open(t, cfg)
	var valid bool
	var definition, predicate string
	err := pool.QueryRow(t.Context(), `SELECT indisvalid,pg_get_indexdef(indexrelid),pg_get_expr(indpred,indrelid)
 FROM pg_index WHERE indexrelid=to_regclass($1)`, oldestIndex).Scan(&valid, &definition, &predicate)
	if err != nil {
		t.Fatal(err)
	}
	if !valid || !strings.Contains(definition, "USING btree (created_at)") {
		t.Fatalf("invalid index: %s", definition)
	}
	const wantPredicate = "(state = ANY (ARRAY['pending'::text, 'ready'::text, 'running'::text, 'waiting'::text]))"
	if predicate != wantPredicate {
		t.Fatalf("predicate = %s; want %s", predicate, wantPredicate)
	}
	oldestExec(t, pool, `CREATE INDEX oldest_migration_sentinel ON work_items(subject_key)`)
	oldestExec(t, pool, `INSERT INTO work_items(kind,subject_key) VALUES('person','preserved')`)
	content, err := migrations.Files.ReadFile(oldestMigration)
	if err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{oldestMigration: &fstest.MapFile{Data: content}}
	if err := db.MigrateFiles(t.Context(), cfg, db.MigrateDown, nil, files); err != nil {
		t.Fatal(err)
	}
	var removed, sentinel bool
	var subject string
	err = pool.QueryRow(t.Context(), `SELECT to_regclass($1) IS NULL,
 to_regclass('oldest_migration_sentinel') IS NOT NULL,subject_key FROM work_items`, oldestIndex).Scan(&removed, &sentinel, &subject)
	if err != nil {
		t.Fatal(err)
	}
	if !removed || !sentinel || subject != "preserved" {
		t.Fatal("down did not preserve unrelated index and data")
	}
	if err := db.MigrateFiles(t.Context(), cfg, db.MigrateUp, nil, files); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), `SELECT indisvalid FROM pg_index WHERE indexrelid=to_regclass($1)`, oldestIndex).Scan(&valid); err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Fatal("reapplied index is invalid")
	}
}
