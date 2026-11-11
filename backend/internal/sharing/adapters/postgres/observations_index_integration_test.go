package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestObservationP00LookupPlan(t *testing.T) {
	pool := observationIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	fixture, recordID, fixtureRows := insertObservationPlanFixture(t, ctx, tx)

	const lookupSQL = `SELECT user_id, flickr_account_id
FROM observations WHERE record_kind = 'photo' AND record_id = $1`
	rows, err := tx.Query(ctx, lookupSQL, recordID)
	if err != nil {
		t.Fatalf("lookup observation observers: %v", err)
	}
	var got []observationObserver
	for rows.Next() {
		var observer observationObserver
		if err := rows.Scan(&observer.userID, &observer.accountID); err != nil {
			rows.Close()
			t.Fatalf("scan observer: %v", err)
		}
		got = append(got, observer)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate observers: %v", err)
	}
	want := []observationObserver{{userID: fixture.userID, accountID: fixture.firstAccountID}, {userID: fixture.userID, accountID: fixture.secondAccountID}}
	if len(got) != len(want) {
		t.Fatalf("observer count = %d, want %d: %+v", len(got), len(want), got)
	}
	gotSet := make(map[observationObserver]bool, len(got))
	for _, observer := range got {
		gotSet[observer] = true
	}
	for _, observer := range want {
		if !gotSet[observer] {
			t.Fatalf("observers = %+v, missing %+v", got, observer)
		}
	}

	var planJSON []byte
	if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+lookupSQL, recordID).Scan(&planJSON); err != nil {
		t.Fatalf("explain observation lookup: %v", err)
	}
	var plan []struct {
		Plan observationPlanNode `json:"Plan"`
	}
	if err := json.Unmarshal(planJSON, &plan); err != nil {
		t.Fatalf("decode observation lookup plan: %v", err)
	}
	if len(plan) == 0 {
		t.Fatalf("empty observation lookup plan: %s", planJSON)
	}
	seqScan, indexScan := inspectObservationPlan(plan[0].Plan)
	if seqScan || !indexScan {
		t.Fatalf("observation lookup must use observations_p00_kind_record_idx without a Seq Scan: %s", planJSON)
	}
	t.Logf("EXPLAIN (ANALYZE, BUFFERS) p00 lookup on %d fixture rows: %s", fixtureRows, planJSON)
}

type observationFixture struct {
	userID          int64
	firstAccountID  int64
	secondAccountID int64
}

func insertObservationPlanFixture(t *testing.T, ctx context.Context, tx pgx.Tx) (observationFixture, string, int) {
	t.Helper()
	suffix := fmt.Sprintf("observation-index-%d", time.Now().UnixNano())
	var fixture observationFixture
	var keyID int64
	if err := tx.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'test','user') RETURNING id`, suffix+"@example.invalid").Scan(&fixture.userID); err != nil {
		t.Fatalf("insert fixture user: %v", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,'test',$2,'{}',100,10,10) RETURNING id`, fixture.userID, suffix).Scan(&keyID); err != nil {
		t.Fatalf("insert fixture API key: %v", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed) VALUES($1,$2,$3,'first','{}','{}') RETURNING id`, fixture.userID, keyID, suffix+"-first@flickr").Scan(&fixture.firstAccountID); err != nil {
		t.Fatalf("insert first fixture account: %v", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed) VALUES($1,$2,$3,'second','{}','{}') RETURNING id`, fixture.userID, keyID, suffix+"-second@flickr").Scan(&fixture.secondAccountID); err != nil {
		t.Fatalf("insert second fixture account: %v", err)
	}

	const fillerRows = 19999
	if tag, err := tx.Exec(ctx, `INSERT INTO observations(user_id,flickr_account_id,record_kind,record_id)
SELECT $1,$2,'photo',$3 || item::text FROM generate_series(1,$4) AS item`, fixture.userID, fixture.firstAccountID, suffix+"-record-", fillerRows); err != nil {
		t.Fatalf("insert observation plan fixture: %v", err)
	} else if tag.RowsAffected() != fillerRows {
		t.Fatalf("fixture rows = %d, want %d", tag.RowsAffected(), fillerRows)
	}

	var recordID string
	if err := tx.QueryRow(ctx, `SELECT record_id FROM observations
WHERE user_id=$1 AND flickr_account_id=$2 AND tableoid='public.observations_p00'::regclass
ORDER BY record_id LIMIT 1`, fixture.userID, fixture.firstAccountID).Scan(&recordID); err != nil {
		t.Fatalf("select record routed to observations_p00: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO observations(user_id,flickr_account_id,record_kind,record_id)
VALUES($1,$2,'photo',$3)`, fixture.userID, fixture.secondAccountID, recordID); err != nil {
		t.Fatalf("insert second observer: %v", err)
	}
	if _, err := tx.Exec(ctx, `ANALYZE observations_p00`); err != nil {
		t.Fatalf("analyze observations_p00 fixture: %v", err)
	}
	return fixture, recordID, fillerRows + 1
}

func TestObservationP00IndexRerun(t *testing.T) {
	pool := observationIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	if _, err := pool.Exec(ctx, `CREATE INDEX CONCURRENTLY IF NOT EXISTS observations_p00_kind_record_idx
ON observations_p00 (record_kind, record_id) INCLUDE (user_id, flickr_account_id)`); err != nil {
		t.Fatalf("rerun p00 index creation: %v", err)
	}

	var indexCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname='public' AND c.relname='observations_p00_kind_record_idx'
AND c.relkind='i'`).Scan(&indexCount); err != nil {
		t.Fatalf("count p00 index relation: %v", err)
	}
	if indexCount != 1 {
		t.Fatalf("p00 index relation count = %d, want 1", indexCount)
	}
	var valid, ready bool
	var definition string
	if err := pool.QueryRow(ctx, `SELECT i.indisvalid, i.indisready, pg_get_indexdef(c.oid)
FROM pg_class c JOIN pg_index i ON i.indexrelid=c.oid
WHERE c.oid=to_regclass('public.observations_p00_kind_record_idx')`).Scan(&valid, &ready, &definition); err != nil {
		t.Fatalf("inspect p00 index: %v", err)
	}
	if !valid || !ready {
		t.Fatalf("p00 index valid/ready = %v/%v, want true/true", valid, ready)
	}
	for _, fragment := range []string{"USING btree", "(record_kind, record_id)", "INCLUDE (user_id, flickr_account_id)"} {
		if !strings.Contains(definition, fragment) {
			t.Errorf("index definition %q does not contain %q", definition, fragment)
		}
	}
}

type observationPlanNode struct {
	NodeType  string                `json:"Node Type"`
	IndexName string                `json:"Index Name"`
	Plans     []observationPlanNode `json:"Plans"`
}

func inspectObservationPlan(node observationPlanNode) (seqScan, indexScan bool) {
	seqScan = node.NodeType == "Seq Scan" || node.NodeType == "Parallel Seq Scan"
	indexScan = node.IndexName == "observations_p00_kind_record_idx"
	for _, child := range node.Plans {
		childSeqScan, childIndexScan := inspectObservationPlan(child)
		seqScan = seqScan || childSeqScan
		indexScan = indexScan || childIndexScan
	}
	return seqScan, indexScan
}

func TestInspectObservationPlan(t *testing.T) {
	tests := []struct {
		name    string
		plan    observationPlanNode
		seqScan bool
		idxScan bool
	}{
		{
			name:    "nested expected index scan",
			plan:    observationPlanNode{NodeType: "Append", Plans: []observationPlanNode{{NodeType: "Index Only Scan", IndexName: "observations_p00_kind_record_idx"}}},
			idxScan: true,
		},
		{
			name:    "sequential scan",
			plan:    observationPlanNode{NodeType: "Seq Scan"},
			seqScan: true,
		},
		{
			name:    "parallel sequential scan",
			plan:    observationPlanNode{NodeType: "Parallel Seq Scan"},
			seqScan: true,
		},
		{
			name: "different index does not count",
			plan: observationPlanNode{NodeType: "Index Scan", IndexName: "observations_p00_pkey"},
		},
		{
			name:    "bitmap scan on expected index",
			plan:    observationPlanNode{NodeType: "Bitmap Index Scan", IndexName: "observations_p00_kind_record_idx"},
			idxScan: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			seqScan, indexScan := inspectObservationPlan(test.plan)
			if seqScan != test.seqScan || indexScan != test.idxScan {
				t.Fatalf("inspectObservationPlan() = %v/%v, want %v/%v", seqScan, indexScan, test.seqScan, test.idxScan)
			}
		})
	}
}

type observationObserver struct {
	userID    int64
	accountID int64
}

func observationIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return dbtest.Open(t, cfg)
}
