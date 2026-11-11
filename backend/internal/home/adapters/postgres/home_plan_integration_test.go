package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestHomeQueryPlansUseBoundedIndexedSources(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's isolated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	pool := dbtest.Open(t, homeDatabase(t))
	suffix := fmt.Sprintf("home-plan-%d", time.Now().UnixNano())
	var userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration','user') RETURNING id`, suffix+"@example.invalid").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	personNSID := suffix + "@flickr"
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,'plan-contact')`, personNSID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via,starred) VALUES($1,$2,1,'home-plan',TRUE)`, userID, personNSID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_, _ = pool.Exec(cleanup, `DELETE FROM users WHERE id=$1`, userID)
		_, _ = pool.Exec(cleanup, `DELETE FROM people WHERE nsid=$1`, personNSID)
	})
	assertHomePlans(t, ctx, pool, homeSnapshotSQL, userID, 3)
}

func assertHomePlans(t *testing.T, ctx context.Context, pool *pgxpool.Pool, statement string, userID int64, batchSize int) {
	t.Helper()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("begin Home query plan transaction: %v", err)
	}
	defer func() {
		if err := conn.Conn().Close(context.Background()); err != nil {
			t.Errorf("close Home plan fixture connection: %v", err)
		}
		conn.Release()
	}()
	setup, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin Home plan fixture transaction: %v", err)
	}
	createPlanFixtures(t, ctx, setup, userID)
	if err := setup.Commit(ctx); err != nil {
		t.Fatalf("commit Home plan fixtures: %v", err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin Home query plan transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var seqScanSetting string
	if err := tx.QueryRow(ctx, `SHOW enable_seqscan`).Scan(&seqScanSetting); err != nil {
		t.Fatalf("read planner sequential scan setting: %v", err)
	}
	if seqScanSetting != "on" {
		t.Fatalf("query plan fixture requires the default sequential scan setting, got %q", seqScanSetting)
	}
	requestPlan := explainHomePlan(t, ctx, tx, "Home request snapshot", statement, userID)
	assertRequestCandidatePlan(t, requestPlan, 24)
	assertWorkItemPlan(t, requestPlan)
	refreshPlan := explainHomePlan(t, ctx, tx, "Home refresh photo page", refreshPhotoPageSQL, userID, "", batchSize)
	assertCandidatePlan(t, refreshPlan, batchSize, batchSize)
	if !t.Failed() {
		t.Log("request and refresh EXPLAIN ANALYZE passed with the default planner and 400,000 candidates")
	}
}

func createPlanFixtures(t *testing.T, ctx context.Context, tx pgx.Tx, userID int64) {
	t.Helper()
	statements := []struct {
		query string
		name  string
	}{
		{`CREATE TEMP TABLE user_home_photo_candidates (user_id BIGINT NOT NULL, photo_id BIGINT NOT NULL, uploaded_at TIMESTAMPTZ)`, "own candidate table"},
		{`CREATE TEMP TABLE user_home_shared_photo_candidates (photo_id BIGINT NOT NULL, uploaded_at TIMESTAMPTZ)`, "shared candidate table"},
		{`CREATE TEMP TABLE sharing_settings (user_id BIGINT NOT NULL, enabled BOOLEAN NOT NULL)`, "sharing settings"},
		{`CREATE TEMP TABLE observations (user_id BIGINT NOT NULL, record_kind TEXT NOT NULL, record_id TEXT NOT NULL)`, "photo observations"},
		{`CREATE TEMP TABLE photos (
    id BIGINT PRIMARY KEY,
    title TEXT NOT NULL,
    owner_nsid TEXT NOT NULL,
    taken_at TIMESTAMPTZ,
    uploaded_at TIMESTAMPTZ,
    sizes JSONB NOT NULL,
    last_update TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp(),
    sizes_status TEXT NOT NULL DEFAULT 'none',
    sizes_checked_at TIMESTAMPTZ,
    visibility TEXT NOT NULL,
    tombstoned_at TIMESTAMPTZ
)`, "candidate photos"},
		{`INSERT INTO user_home_photo_candidates
SELECT $1, candidate_id::bigint, statement_timestamp() - candidate_id * interval '1 second'
FROM generate_series(1, 200000) AS candidate(candidate_id)`, "200,000 own candidates"},
		{`INSERT INTO user_home_photo_candidates
SELECT $1 + 2, candidate_id::bigint, statement_timestamp() + interval '2 years' - candidate_id * interval '1 second'
FROM generate_series(1, 200000) AS candidate(candidate_id)`, "200,000 disabled sharer candidates"},
		{`INSERT INTO user_home_shared_photo_candidates
SELECT candidate_id::bigint, statement_timestamp() - candidate_id * interval '1 second'
FROM generate_series(1, 200000) AS candidate(candidate_id)`, "200,000 shared candidates"},
		{`INSERT INTO user_home_photo_candidates (user_id, photo_id, uploaded_at)
SELECT $1, 400000 + candidate_id, statement_timestamp() + interval '1 year'
FROM generate_series(1, 12) AS candidate(candidate_id)`, "12 visible own candidates"},
		{`INSERT INTO user_home_photo_candidates (user_id, photo_id, uploaded_at)
SELECT $1 + 1, 500000 + candidate_id, statement_timestamp() + interval '1 year' + candidate_id * interval '1 second'
FROM generate_series(1, 12) AS candidate(candidate_id)`, "12 visible shared candidates"},
		{`INSERT INTO user_home_shared_photo_candidates (photo_id, uploaded_at)
SELECT 400000 + candidate_id, statement_timestamp() + interval '1 year'
FROM generate_series(1, 12) AS candidate(candidate_id)`, "12 revoked shared candidates"},
		{`INSERT INTO sharing_settings (user_id, enabled) VALUES ($1 + 1, TRUE), ($1 + 2, FALSE)`, "enabled and disabled sharers"},
		{`INSERT INTO photos (id, title, owner_nsid, taken_at, uploaded_at, sizes, visibility)
SELECT 400000 + candidate_id, 'plan photo', 'plan-owner', statement_timestamp(), statement_timestamp(), '[]'::jsonb, 'public'
FROM generate_series(1, 12) AS candidate(candidate_id)`, "candidate photos"},
		{`INSERT INTO photos (id, title, owner_nsid, taken_at, uploaded_at, sizes, visibility)
SELECT 500000 + candidate_id, 'shared plan photo', 'shared-plan-owner', statement_timestamp(), statement_timestamp(), '[]'::jsonb, 'public'
FROM generate_series(1, 12) AS candidate(candidate_id)`, "shared candidate photos"},
		{`INSERT INTO observations (user_id, record_kind, record_id)
SELECT $1, 'photo', (400000 + candidate_id)::text FROM generate_series(1, 12) AS candidate(candidate_id)
UNION ALL
SELECT $1 + 1, 'photo', (500000 + candidate_id)::text FROM generate_series(1, 12) AS candidate(candidate_id)`, "visible photo observations"},
		{`CREATE UNIQUE INDEX ON user_home_photo_candidates (user_id, photo_id)`, "own candidate primary key"},
		{`CREATE INDEX ON user_home_photo_candidates (user_id, uploaded_at DESC NULLS LAST, photo_id DESC)`, "own candidate recent index"},
		{`CREATE UNIQUE INDEX ON user_home_shared_photo_candidates (photo_id)`, "shared candidate primary key"},
		{`CREATE INDEX ON user_home_shared_photo_candidates (uploaded_at DESC NULLS LAST, photo_id DESC)`, "shared candidate recent index"},
		{`CREATE INDEX ON observations (user_id, record_kind, record_id)`, "photo observation lookup"},
		{`CREATE INDEX ON sharing_settings (user_id) WHERE enabled`, "enabled sharing lookup"},
		{`CREATE TEMP TABLE work_items (id BIGINT, kind TEXT, subject_key TEXT, user_id BIGINT, state TEXT, updated_at TIMESTAMPTZ)`, "work item table"},
		{`CREATE INDEX ON work_items (user_id, subject_key, state)
WHERE state IN ('pending', 'ready', 'running', 'waiting', 'failed')`, "contact state index"},
		{`CREATE INDEX ON work_items (user_id, updated_at DESC, id DESC)
WHERE user_id IS NOT NULL AND kind IN ('photos', 'sizes')
  AND state IN ('ready', 'running', 'waiting', 'failed', 'done')`, "activity index"},
	}
	for _, statement := range statements {
		var args []any
		if strings.Contains(statement.query, "$1") {
			args = append(args, userID)
		}
		if _, err := tx.Exec(ctx, statement.query, args...); err != nil {
			t.Fatalf("create Home plan %s: %v", statement.name, err)
		}
	}
	var contactNSID string
	if err := tx.QueryRow(ctx, `SELECT person_nsid FROM public.crawl_interests WHERE user_id=$1 AND starred ORDER BY person_nsid LIMIT 1`, userID).Scan(&contactNSID); err != nil {
		t.Fatalf("select starred contact for work item fixture: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO work_items
SELECT candidate_id, 'photos',
       CASE WHEN candidate_id <= 3 THEN $2 ELSE 'other-' || candidate_id::text END,
       CASE WHEN candidate_id <= 3 THEN $1 ELSE $1 + candidate_id END,
       CASE candidate_id % 3 WHEN 0 THEN 'failed' WHEN 1 THEN 'running' ELSE 'pending' END,
       statement_timestamp() - candidate_id * interval '1 second'
FROM generate_series(1, 5000) AS candidate(candidate_id)`, userID, contactNSID); err != nil {
		t.Fatalf("seed 5,000 active work items: %v", err)
	}
	for _, table := range []string{"user_home_photo_candidates", "user_home_shared_photo_candidates", "sharing_settings", "observations", "work_items"} {
		if _, err := tx.Exec(ctx, `ANALYZE `+table); err != nil {
			t.Fatalf("analyze synthetic %s: %v", table, err)
		}
	}
}

func assertRequestCandidatePlan(t *testing.T, root explainNode, candidateLimit int) {
	t.Helper()
	var seen bool
	var rowsRead float64
	var visit func(explainNode)
	visit = func(node explainNode) {
		if node.RelationName == "user_home_photo_candidates" {
			seen = true
			if node.NodeType == "Seq Scan" {
				t.Error("Home request plan sequentially scans per-user photo candidates")
			}
			rowsRead += node.ActualRows * node.ActualLoops
		}
		for _, child := range node.Plans {
			visit(child)
		}
	}
	visit(root)
	if !seen {
		t.Fatal("Home request plan does not use per-user photo candidates")
	}
	if rowsRead > float64(candidateLimit) {
		t.Errorf("Home request reads %.0f per-user candidates, over bound %d", rowsRead, candidateLimit)
	}
}

func explainHomePlan(t *testing.T, ctx context.Context, tx pgx.Tx, label, statement string, args ...any) explainNode {
	t.Helper()
	var raw []byte
	if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+statement, args...).Scan(&raw); err != nil {
		t.Fatalf("explain Home query: %v", err)
	}
	t.Logf("EXPLAIN %s (ANALYZE, BUFFERS, FORMAT JSON): %s", label, raw)
	var document explainDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode Home query plan: %v", err)
	}
	if len(document) != 1 {
		t.Fatalf("Home query plan has %d roots", len(document))
	}
	return document[0].Plan
}

func assertCandidatePlan(t *testing.T, root explainNode, ownLimit, sharedLimit int) {
	t.Helper()
	seenOwn, seenShared := false, false
	rowsRead := map[string]float64{}
	var visit func(explainNode)
	visit = func(node explainNode) {
		for _, relation := range []string{"user_home_photo_candidates", "user_home_shared_photo_candidates"} {
			if node.RelationName != relation {
				continue
			}
			if relation == "user_home_photo_candidates" {
				seenOwn = true
			} else {
				seenShared = true
			}
			if node.NodeType == "Seq Scan" {
				t.Errorf("Home plan sequentially scans %s", relation)
			}
			rowsRead[relation] += node.ActualRows * node.ActualLoops
		}
		for _, child := range node.Plans {
			visit(child)
		}
	}
	visit(root)
	if !seenOwn || !seenShared {
		t.Fatalf("Home plan candidate sources own=%t shared=%t", seenOwn, seenShared)
	}
	if rowsRead["user_home_photo_candidates"] > float64(ownLimit) {
		t.Errorf("Home plan reads %.0f own candidates, over limit %d", rowsRead["user_home_photo_candidates"], ownLimit)
	}
	if rowsRead["user_home_shared_photo_candidates"] > float64(sharedLimit) {
		t.Errorf("Home plan reads %.0f shared candidates, over limit %d", rowsRead["user_home_shared_photo_candidates"], sharedLimit)
	}
}

func assertWorkItemPlan(t *testing.T, root explainNode) {
	t.Helper()
	var maxRows float64
	var visit func(explainNode)
	visit = func(node explainNode) {
		if node.RelationName == "work_items" {
			rows := node.ActualRows * node.ActualLoops
			if rows > maxRows {
				maxRows = rows
			}
			if node.NodeType == "Seq Scan" {
				t.Error("Home plan sequentially scans active work items")
			}
		}
		for _, child := range node.Plans {
			visit(child)
		}
	}
	visit(root)
	if maxRows > 5 {
		t.Errorf("Home plan reads %.0f work items for bounded state/activity lookups", maxRows)
	}
}
