package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func assertHomePlans(t *testing.T, ctx context.Context, pool *pgxpool.Pool, statement string, userID int64, batchSize int) {
	t.Helper()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("begin Home query plan transaction: %v", err)
	}
	defer conn.Release()
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
	requestPlan := explainHomePlan(t, ctx, tx, statement, userID)
	assertCandidatePlan(t, requestPlan, 12)
	assertWorkItemPlan(t, requestPlan)
	refreshPlan := explainHomePlan(t, ctx, tx, refreshPhotoPageSQL, userID, "", batchSize)
	assertCandidatePlan(t, refreshPlan, batchSize)
	t.Log("request and refresh EXPLAIN ANALYZE passed with the default planner and 400,000 candidates")
}

func createPlanFixtures(t *testing.T, ctx context.Context, tx pgx.Tx, userID int64) {
	t.Helper()
	statements := []struct {
		query string
		name  string
	}{
		{`CREATE TEMP TABLE user_home_photo_candidates (user_id BIGINT NOT NULL, photo_id BIGINT NOT NULL, uploaded_at TIMESTAMPTZ)`, "own candidate table"},
		{`CREATE TEMP TABLE user_home_shared_photo_candidates (photo_id BIGINT NOT NULL, uploaded_at TIMESTAMPTZ)`, "shared candidate table"},
		{`CREATE INDEX ON user_home_photo_candidates (user_id, uploaded_at DESC NULLS LAST, photo_id DESC)`, "own candidate index"},
		{`CREATE INDEX ON user_home_shared_photo_candidates (uploaded_at DESC NULLS LAST, photo_id DESC)`, "shared candidate index"},
		{`INSERT INTO user_home_photo_candidates
SELECT $1, candidate_id::bigint, statement_timestamp() - candidate_id * interval '1 second'
FROM generate_series(1, 200000) AS candidate(candidate_id)`, "200,000 own candidates"},
		{`INSERT INTO user_home_shared_photo_candidates
SELECT candidate_id::bigint, statement_timestamp() - candidate_id * interval '1 second'
FROM generate_series(1, 200000) AS candidate(candidate_id)`, "200,000 shared candidates"},
		{`INSERT INTO user_home_photo_candidates
SELECT $1, candidate.photo_id, statement_timestamp() + interval '1 year'
FROM public.user_home_photo_candidates candidate
WHERE candidate.user_id=$1 ORDER BY candidate.photo_id LIMIT 12`, "visible own candidates"},
		{`INSERT INTO user_home_shared_photo_candidates
SELECT candidate.photo_id, statement_timestamp() + interval '1 year'
FROM public.user_home_shared_photo_candidates candidate
ORDER BY candidate.photo_id LIMIT 12`, "visible shared candidates"},
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
SELECT candidate_id, 'photos', $2, $1,
       CASE candidate_id % 3 WHEN 0 THEN 'failed' WHEN 1 THEN 'running' ELSE 'pending' END,
       statement_timestamp() - candidate_id * interval '1 second'
FROM generate_series(1, 5000) AS candidate(candidate_id)`, userID, contactNSID); err != nil {
		t.Fatalf("seed 5,000 active work items: %v", err)
	}
	for _, table := range []string{"user_home_photo_candidates", "user_home_shared_photo_candidates", "work_items"} {
		if _, err := tx.Exec(ctx, `ANALYZE `+table); err != nil {
			t.Fatalf("analyze synthetic %s: %v", table, err)
		}
	}
}

func explainHomePlan(t *testing.T, ctx context.Context, tx pgx.Tx, statement string, args ...any) explainNode {
	t.Helper()
	var raw []byte
	if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+statement, args...).Scan(&raw); err != nil {
		t.Fatalf("explain Home query: %v", err)
	}
	var document explainDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode Home query plan: %v", err)
	}
	if len(document) != 1 {
		t.Fatalf("Home query plan has %d roots", len(document))
	}
	return document[0].Plan
}

func assertCandidatePlan(t *testing.T, root explainNode, limit int) {
	t.Helper()
	seenOwn, seenShared := false, false
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
			if node.ActualRows*node.ActualLoops > float64(limit) {
				t.Errorf("Home plan reads %.0f rows from %s, over limit %d", node.ActualRows*node.ActualLoops, relation, limit)
			}
		}
		for _, child := range node.Plans {
			visit(child)
		}
	}
	visit(root)
	if !seenOwn || !seenShared {
		t.Fatalf("Home plan candidate sources own=%t shared=%t", seenOwn, seenShared)
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
