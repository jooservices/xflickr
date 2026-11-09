package river

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReclaimSelfHealUsesEligibleFailureIndex(t *testing.T) {
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

	suffix := fmt.Sprintf("reclaim-index-%d", time.Now().UnixNano())
	var freePlanID, userID, keyID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM plans WHERE name='free'`).Scan(&freePlanID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO users(email,password_hash,role,plan_id) VALUES($1,'unused','user',$2) RETURNING id`, suffix+"@example.test", freePlanID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,'reclaim index test',$2,'{}',3600,4,5) RETURNING id`, userID, suffix).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,key_id,state,last_error,updated_at,lease_expires_at)
		SELECT 'sync_person',$1||'-'||g,$2,$3,
			CASE WHEN g <= 4 OR g BETWEEN 1000 AND 100999 THEN 'failed'
				WHEN g BETWEEN 5 AND 8 OR g BETWEEN 200000 AND 210010 THEN 'ready'
				ELSE 'pending' END,
			CASE WHEN g <= 2 THEN 'lease expired' WHEN g <= 4 THEN 'other error'
				WHEN g BETWEEN 1000 AND 100999 THEN 'historical failure' END,
			CASE WHEN g <= 2 OR g BETWEEN 1000 AND 100999 THEN now()-interval '30 minutes' ELSE now() END,
			CASE WHEN g BETWEEN 5 AND 8 OR g BETWEEN 200000 AND 210010 THEN now()-interval '30 minutes' END
		FROM generate_series(1,400000) AS g`, suffix, userID, keyID); err != nil {
		t.Fatal(err)
	}
	var matchedWorkItemID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM work_items WHERE subject_key=$1`, suffix+"-5").Scan(&matchedWorkItemID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO river_job(state,args,kind,queue)
		SELECT 'available',jsonb_build_object('args',jsonb_build_object('work_item_id','unrelated-'||g)),
			'sync_person','sync' FROM generate_series(1,100000) AS g`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO river_job(state,args,kind,queue)
		VALUES ('available',jsonb_build_object('args',jsonb_build_object('work_item_id',$1::text)),'sync_person','sync')`, fmt.Sprint(matchedWorkItemID)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO river_job(state,args,kind,queue)
		SELECT 'available',jsonb_build_object('args',jsonb_build_object('work_item_id',w.id::text)),
			'sync_person','sync'
		FROM work_items w
		WHERE w.subject_key BETWEEN $1 AND $2`, suffix+"-200000", suffix+"-210000"); err != nil {
		t.Fatal(err)
	}
	var keylessCursor int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(id),0) FROM work_items`).Scan(&keylessCursor); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state,not_before)
		SELECT 'sync_person',$1||'-future-keyless-'||g,$2,'pending',now()+interval '1 day'
		FROM generate_series(1,20000) AS g`, suffix, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state) VALUES ('sync_person',$1,$2,'pending')`, suffix+"-keyless", userID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `ANALYZE work_items`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `ANALYZE river_job`); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	rescoreArgs := []any{10, 100, 20, 4, 5, 0.1, 500}
	rescoreSQL := func(selection string) string {
		return `UPDATE work_items AS w SET score = batch.score
FROM (
	SELECT w2.id, ` + staticScoreExpr + ` AS score
	FROM work_items w2
	LEFT JOIN users u ON u.id=w2.user_id
	LEFT JOIN plans p ON p.id=u.plan_id
	LEFT JOIN crawl_interests ci ON ci.user_id=w2.user_id AND ci.person_nsid=w2.subject_key
	` + selection + `
) AS batch
WHERE w.id = batch.id`
	}
	queries := []struct {
		name          string
		sql           string
		args          []any
		indexRelation string
		indexName     string
		indexNames    []string
		maxRows       int
		maxBuffers    int
	}{
		{
			name:          "Reclaim expired lease selection",
			sql:           reclaimExpiredLeasesSQL,
			args:          []any{now, nil, nil, 100},
			indexRelation: "work_items",
			indexNames:    []string{"work_items_ready_lease_idx", "work_items_expired_lease_cursor_idx"},
			maxRows:       100,
			maxBuffers:    2000,
		},
		{
			name:          "pump node inflight count",
			sql:           inflightNodeCountSQL,
			args:          []any{500},
			indexRelation: "work_items",
			indexNames:    []string{"work_items_active_worker_count_idx", "work_items_active_node_count_idx"},
			maxRows:       500,
			maxBuffers:    500,
		},
		{
			name: "pump key inflight count",
			sql:  inflightKeyCountSQL,
			args: []any{keyID, 100},
		},
		{
			name: "pump worker inflight count",
			sql:  inflightWorkerCountSQL,
			args: []any{"sync", 100},
		},
		{
			name:          "pump keyless pending probe",
			sql:           keylessPendingExistsSQL,
			indexRelation: "work_items",
			indexName:     "work_items_pending_keyless_due_idx",
		},
		{
			name:          "pump key assignment candidates",
			sql:           keylessOwnerCandidatesSQL,
			args:          []any{keylessCursor, 100},
			indexRelation: "work_items",
			indexName:     "work_items_pending_keyless_idx",
			maxRows:       100,
			maxBuffers:    500,
		},
		{
			name: "pump plan backlog count",
			sql:  planBacklogSQL,
			args: []any{keyID, "sync", 100},
		},
		{
			name: "pump pending claim",
			sql: `SELECT w.id, w.kind, w.subject_key, w.cursor FROM work_items w LEFT JOIN users u ON u.id=w.user_id
				WHERE w.state='pending' AND w.key_id=$1 AND w.worker_type=$2 AND w.not_before<=now()
				AND COALESCE(u.plan_id,(SELECT id FROM plans WHERE name='free'))=$3
				AND NOT EXISTS (SELECT 1 FROM crawl_interests ci WHERE ci.user_id=w.user_id AND ci.person_nsid=w.subject_key AND ci.paused_at IS NOT NULL)
				ORDER BY w.score DESC,w.not_before,w.id LIMIT $4 FOR UPDATE OF w SKIP LOCKED`,
			args: []any{keyID, "sync", freePlanID, 100},
		},
		{
			name: "maintenance pending rescore",
			sql:  rescoreSQL(`WHERE w2.state='pending' AND w2.score IS DISTINCT FROM (` + staticScoreExpr + `) ORDER BY w2.id LIMIT $7`),
			args: rescoreArgs,
		},
		{
			name: "maintenance expired selection rescore",
			sql: rescoreSQL(`WHERE w2.state='pending' AND w2.direct_action AND ci.selected_until IS NOT NULL
				AND ci.selected_until <= now() AND w2.score IS DISTINCT FROM (` + staticScoreExpr + `)
				ORDER BY ci.selected_until, w2.id LIMIT $7`),
			args: rescoreArgs,
		},
		{
			name:          "Reclaim self-heal update",
			sql:           reclaimSelfHealSQL,
			args:          []any{now, now.Add(-SelfHealAfter), 100},
			indexRelation: "work_items",
			indexName:     "work_items_failed_lease_expired_idx",
			maxRows:       100,
			maxBuffers:    500,
		},
	}

	for _, query := range queries {
		t.Run(query.name, func(t *testing.T) {
			plan, err := explainStatement(ctx, tx, query.sql, query.args...)
			if err != nil {
				t.Fatal(err)
			}
			seqScan := containsWorkItemsSeqScan(plan.Plan)
			t.Logf("work_items_seq_scan=%t", seqScan)
			if seqScan {
				t.Error("query performs a Seq Scan on work_items")
			}
			indexNames := query.indexNames
			if len(indexNames) == 0 && query.indexName != "" {
				indexNames = []string{query.indexName}
			}
			var actualRows float64
			indexFound := len(indexNames) == 0
			for _, indexName := range indexNames {
				if containsIndexOnRelation(plan.Plan, query.indexRelation, indexName) {
					indexFound = true
				}
				if rows := maxActualRowsOnIndex(plan.Plan, query.indexRelation, indexName); rows > actualRows {
					actualRows = rows
				}
			}
			if !indexFound {
				t.Errorf("query does not use any of %v; plan=%+v", indexNames, plan.Plan)
			}
			if query.maxRows > 0 {
				if actualRows > float64(query.maxRows) {
					t.Errorf("%s read %.0f indexed rows, want at most %d", query.name, actualRows, query.maxRows)
				}
			}
			if query.maxBuffers > 0 {
				buffers := plan.Plan.SharedHitBlocks + plan.Plan.SharedReadBlocks
				if buffers > query.maxBuffers {
					t.Errorf("%s touched %d shared buffers, want at most %d", query.name, buffers, query.maxBuffers)
				}
			}
			if query.name == "Reclaim expired lease selection" {
				if loops := maxActualLoopsOnRelation(plan.Plan, "river_job"); loops > 100 {
					t.Errorf("Reclaim inspected River jobs %.0f times, want at most 100", loops)
				}
				if !containsIndexOnRelation(plan.Plan, "river_job", "river_job_active_work_item_idx") {
					t.Error("Reclaim lease selector does not use river_job_active_work_item_idx")
				}
				if containsSeqScanOnRelation(plan.Plan, "river_job") {
					t.Error("Reclaim lease selector performs a Seq Scan on river_job")
				}
			}
		})
	}
}

func containsSeqScanOnRelation(node explainNode, relation string) bool {
	if strings.Contains(node.NodeType, "Seq Scan") && node.RelationName == relation {
		return true
	}
	for _, child := range node.Plans {
		if containsSeqScanOnRelation(child, relation) {
			return true
		}
	}
	return false
}

func containsIndexOnRelation(node explainNode, relation, index string) bool {
	return containsIndexOnRelationBelow(node, relation, index, "")
}

func containsIndexOnRelationBelow(node explainNode, relation, index, inheritedRelation string) bool {
	if node.RelationName != "" {
		inheritedRelation = node.RelationName
	}
	if inheritedRelation == relation && node.IndexName == index {
		return true
	}
	for _, child := range node.Plans {
		if containsIndexOnRelationBelow(child, relation, index, inheritedRelation) {
			return true
		}
	}
	return false
}

func TestContainsIndexOnBitmapChild(t *testing.T) {
	plan := explainNode{
		NodeType:     "Bitmap Heap Scan",
		RelationName: "work_items",
		Plans: []explainNode{{
			NodeType:  "Bitmap Index Scan",
			IndexName: "work_items_failed_lease_expired_idx",
		}},
	}
	if !containsIndexOnRelation(plan, "work_items", "work_items_failed_lease_expired_idx") {
		t.Fatal("did not recognize index on a Bitmap Index Scan child")
	}
}

func TestAssignOwnerKeysProcessesOrderedBatches(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run with XFLICKR_INTEGRATION_TEST=1 to use the worktree database")
	}
	pool := internalIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	suffix := fmt.Sprintf("keyless-batch-%d", time.Now().UnixNano())
	const batchSize = 100
	const rowsPerOwner = 150
	keyID := seedReclaimKey(t, ctx, pool, suffix)
	var ownerID, orphanID int64
	if err := pool.QueryRow(ctx, `SELECT user_id FROM api_keys WHERE id=$1`, keyID).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'unused','user') RETURNING id`, suffix+"-orphan@example.test").Scan(&orphanID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE subject_key LIKE $1`, suffix+"-%")
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE email LIKE $1`, suffix+"%")
	})
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	var lastWorkItemID int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(id),0) FROM work_items`).Scan(&lastWorkItemID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schedule_meta(key,value) VALUES ('keyless_owner_cursor',$1)
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, fmt.Sprint(lastWorkItemID)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state)
		SELECT 'sync_person',$1||'-orphan-'||g,$2,'pending' FROM generate_series(1,$3) AS g`, suffix, orphanID, rowsPerOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state)
		SELECT 'sync_person',$1||'-owned-'||g,$2,'pending' FROM generate_series(1,$3) AS g`, suffix, ownerID, rowsPerOwner); err != nil {
		t.Fatal(err)
	}
	assigned := func() int {
		t.Helper()
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM work_items WHERE subject_key LIKE $1 AND key_id IS NOT NULL`, suffix+"-owned-%").Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	if err := assignOwnerKeys(ctx, tx, batchSize); err != nil {
		t.Fatal(err)
	}
	firstBatch := assigned()
	if firstBatch > batchSize {
		t.Fatalf("first pump assigned %d owned keyless rows, want at most %d", firstBatch, batchSize)
	}
	if err := assignOwnerKeys(ctx, tx, batchSize); err != nil {
		t.Fatal(err)
	}
	if secondBatch := assigned(); secondBatch <= firstBatch {
		t.Fatalf("second pump assigned %d owned keyless rows, want progress beyond %d after skipping ownerless rows", secondBatch, firstBatch)
	}
	for assigned() < rowsPerOwner {
		if err := assignOwnerKeys(ctx, tx, batchSize); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReclaimProcessesLargeBacklogsInBatches(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run with XFLICKR_INTEGRATION_TEST=1 to use the worktree database")
	}
	pool := internalIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	suffix := fmt.Sprintf("reclaim-batch-%d", time.Now().UnixNano())
	const batchSize = 100
	const backlog = 250
	var userID, keyID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'unused','user') RETURNING id`, suffix+"@example.test").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE subject_key LIKE $1`, suffix+"-%")
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst)
		VALUES($1,'reclaim batch test',$2,'{}',3600,4,5) RETURNING id`, userID, suffix).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,key_id,state,lease_expires_at)
		SELECT 'sync_person',$1||'-active-'||g,$2,$3,'ready',$4::timestamptz-interval '1 minute'
		FROM generate_series(1,$5) AS g`, suffix, userID, keyID, now, backlog); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,key_id,state,last_error,updated_at)
		SELECT 'sync_person',$1||'-failed-'||g,$2,$3,'failed','lease expired',$4::timestamptz-interval '30 minutes'
		FROM generate_series(1,$5) AS g`, suffix, userID, keyID, now, backlog); err != nil {
		t.Fatal(err)
	}
	store := Store{Pool: pool}
	for remaining, wantBatch := backlog, batchSize; remaining > 0; remaining -= wantBatch {
		want := min(batchSize, remaining)
		reclaimed, err := store.Reclaim(ctx, 5, time.Second, batchSize, now)
		if err != nil {
			t.Fatal(err)
		}
		if reclaimed != want {
			t.Fatalf("Reclaim() = %d, want bounded batch %d", reclaimed, want)
		}
		var ready, failed int
		if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE state='ready'), count(*) FILTER (WHERE state='failed')
			FROM work_items WHERE subject_key LIKE $1`, suffix+"-%").Scan(&ready, &failed); err != nil {
			t.Fatal(err)
		}
		wantRemaining := remaining - want
		if ready != wantRemaining || failed != wantRemaining {
			t.Fatalf("remaining ready=%d failed=%d, want %d each", ready, failed, wantRemaining)
		}
		wantBatch = batchSize
	}
}

func TestReclaimCursorAdvancesPastLiveRiverJobs(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run with XFLICKR_INTEGRATION_TEST=1 to use the worktree database")
	}
	pool := internalIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	suffix := fmt.Sprintf("reclaim-cursor-%d", time.Now().UnixNano())
	const batchSize = 100
	const liveBacklog = 150
	const reclaimableBacklog = 10
	keyID := seedReclaimKey(t, ctx, pool, suffix)
	var userID int64
	if err := pool.QueryRow(ctx, `SELECT user_id FROM api_keys WHERE id=$1`, keyID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE args->'args'->>'work_item_id' IN
			(SELECT id::text FROM work_items WHERE subject_key LIKE $1)`, suffix+"-%")
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE subject_key LIKE $1`, suffix+"-%")
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE email=$1`, suffix+"@example.test")
	})
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,key_id,state,lease_expires_at)
		SELECT 'sync_person',$1||'-live-'||g,$2,$3,'ready','2000-01-01'::timestamptz
		FROM generate_series(1,$4) AS g`, suffix, userID, keyID, liveBacklog); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,key_id,state,lease_expires_at)
		SELECT 'sync_person',$1||'-free-'||g,$2,$3,'ready','2000-01-01'::timestamptz
		FROM generate_series(1,$4) AS g`, suffix, userID, keyID, reclaimableBacklog); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO river_job(state,args,kind,queue)
		SELECT 'available',jsonb_build_object('args',jsonb_build_object('work_item_id',w.id::text)),
			'sync_person','sync' FROM work_items w WHERE w.subject_key LIKE $1`, suffix+"-live-%"); err != nil {
		t.Fatal(err)
	}
	store := Store{Pool: pool}
	if got, err := store.Reclaim(ctx, 5, time.Second, batchSize, time.Now()); err != nil || got != 0 {
		t.Fatalf("first Reclaim() = %d, %v; want 0 while the first 100 candidates have live River jobs", got, err)
	}
	if got, err := store.Reclaim(ctx, 5, time.Second, batchSize, time.Now()); err != nil || got != reclaimableBacklog {
		t.Fatalf("second Reclaim() = %d, %v; want %d after the cursor advances", got, err, reclaimableBacklog)
	}
}

func seedReclaimKey(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string) int64 {
	t.Helper()
	var userID, keyID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'unused','user') RETURNING id`, suffix+"@example.test").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst)
		VALUES($1,'reclaim integration test',$2,'{}',3600,4,5) RETURNING id`, userID, suffix).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status)
		VALUES($1,$2,$3,'owner','{}','{}','ok')`, userID, keyID, suffix+"@N00"); err != nil {
		t.Fatal(err)
	}
	return keyID
}
