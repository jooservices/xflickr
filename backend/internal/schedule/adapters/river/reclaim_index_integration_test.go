package river

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestReclaimSelfHealUsesFailedUpdatedIndex(t *testing.T) {
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
	if _, err := tx.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,key_id,state,last_error,updated_at)
		SELECT 'sync_person',$1||'-'||g,$2,$3,
			CASE WHEN g <= 4 THEN 'failed' ELSE 'pending' END,
			CASE WHEN g <= 2 THEN 'lease expired' WHEN g <= 4 THEN 'other error' END,
			CASE WHEN g <= 2 THEN now()-interval '30 minutes' ELSE now() END
		FROM generate_series(1,400000) AS g`, suffix, userID, keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `ANALYZE work_items`); err != nil {
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
		name string
		sql  string
		args []any
	}{
		{
			name: "Reclaim expired lease selection",
			sql: `SELECT id, attempts, kind FROM work_items WHERE state IN ('ready','running') AND lease_expires_at <= $1
				AND NOT EXISTS (SELECT 1 FROM river_job j WHERE j.state IN ` + liveRiverStates + ` AND j.args->'args'->>'work_item_id' = work_items.id::text)
				ORDER BY lease_expires_at, id FOR UPDATE SKIP LOCKED`,
			args: []any{now},
		},
		{
			name: "pump inflight count",
			sql:  `SELECT count(*) FROM work_items WHERE state IN ('ready','running') AND ($1=0 OR key_id=$1) AND ($2='' OR (CASE WHEN kind LIKE 'ai%' THEN 'ai' WHEN kind LIKE 'media%' THEN 'media' ELSE 'sync' END)=$2)`,
			args: []any{int64(0), ""},
		},
		{
			name: "pump keyless pending count",
			sql:  `SELECT count(*) FROM work_items WHERE state='pending' AND key_id IS NULL AND not_before <= now()`,
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
			name: "pump key assignment",
			sql: `UPDATE work_items w SET key_id=owner.key_id, updated_at=now()
				FROM (SELECT DISTINCT ON (user_id) user_id, key_id FROM flickr_accounts WHERE status='ok' ORDER BY user_id, id) owner
				WHERE w.state='pending' AND w.key_id IS NULL AND w.user_id=owner.user_id`,
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
			name: "Reclaim self-heal update",
			sql: `UPDATE work_items SET state='pending', attempts=0, not_before=$1, lease_expires_at=NULL, last_error=NULL, updated_at=$1
				WHERE state='failed' AND last_error='lease expired' AND updated_at <= $2`,
			args: []any{now, now.Add(-SelfHealAfter)},
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
			if query.name == "Reclaim self-heal update" {
				if seqScan {
					t.Error("Reclaim self-heal performs a Seq Scan on work_items")
				}
				if !containsWorkItemsIndex(plan.Plan, "work_items_failed_updated_idx") {
					t.Error("Reclaim self-heal does not use work_items_failed_updated_idx")
				}
			}
		})
	}
}

func containsWorkItemsIndex(node explainNode, index string) bool {
	if node.RelationName == "work_items" && node.IndexName == index {
		return true
	}
	for _, child := range node.Plans {
		if containsWorkItemsIndex(child, index) {
			return true
		}
	}
	return false
}
