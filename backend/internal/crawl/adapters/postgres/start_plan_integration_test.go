//go:build workitemsplan

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

type startPlanDocument struct {
	Plan startPlanNode `json:"Plan"`
}

type startPlanNode struct {
	NodeType     string          `json:"Node Type"`
	RelationName string          `json:"Relation Name"`
	IndexName    string          `json:"Index Name"`
	Plans        []startPlanNode `json:"Plans"`
}

func TestStartPlanUsesPrimaryKey(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	var databaseName string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(databaseName, "xflickr_wt") {
		t.Fatalf("plan fixture requires this worktree database, got %q", databaseName)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	suffix := fmt.Sprintf("start-plan-%d", time.Now().UnixNano())
	if _, err := tx.Exec(ctx, `INSERT INTO work_items(kind, subject_key, state)
		SELECT 'start_plan', $1 || '-' || g::text,
			CASE WHEN g <= 350000 THEN 'pending'
				WHEN g <= 375000 THEN 'done'
				ELSE 'failed' END
		FROM generate_series(1, 399997) AS g`, suffix); err != nil {
		t.Fatalf("insert 399997 baseline work items: %v", err)
	}
	for _, state := range []string{"ready", "running", "waiting"} {
		if _, err := tx.Exec(ctx, `INSERT INTO work_items(kind, subject_key, state)
			VALUES ('start_plan', $1, $2)`, suffix+"-"+state, state); err != nil {
			t.Fatalf("insert %s plan target: %v", state, err)
		}
	}
	if _, err := tx.Exec(ctx, `ANALYZE work_items`); err != nil {
		t.Fatalf("analyze work_items fixture: %v", err)
	}

	for _, state := range []string{"ready", "running"} {
		t.Run(state, func(t *testing.T) {
			var id int64
			if err := tx.QueryRow(ctx, `SELECT id FROM work_items
				WHERE kind = 'start_plan' AND subject_key = $1`, suffix+"-"+state).Scan(&id); err != nil {
				t.Fatal(err)
			}
			var raw []byte
			if err := tx.QueryRow(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) `+startSQL, id).Scan(&raw); err != nil {
				t.Fatalf("explain Start for %s: %v", state, err)
			}
			t.Logf("EXPLAIN (ANALYZE, BUFFERS) on 400000 work_items (%s): %s", state, raw)
			var documents []startPlanDocument
			if err := json.Unmarshal(raw, &documents); err != nil || len(documents) != 1 {
				t.Fatalf("decode Start plan: %v; documents=%d", err, len(documents))
			}
			usesPrimaryKey, hasWorkItemsSeqScan := inspectStartPlan(documents[0].Plan)
			if hasWorkItemsSeqScan {
				t.Fatal("Start plan contains a Seq Scan on work_items")
			}
			if !usesPrimaryKey {
				t.Fatalf("Start plan does not use work_items_pkey: %s", raw)
			}
		})
	}
}

func inspectStartPlan(node startPlanNode) (usesPrimaryKey, hasWorkItemsSeqScan bool) {
	usesPrimaryKey = node.IndexName == "work_items_pkey"
	hasWorkItemsSeqScan = strings.Contains(node.NodeType, "Seq Scan") && node.RelationName == "work_items"
	for _, child := range node.Plans {
		childUsesPrimaryKey, childHasSeqScan := inspectStartPlan(child)
		usesPrimaryKey = usesPrimaryKey || childUsesPrimaryKey
		hasWorkItemsSeqScan = hasWorkItemsSeqScan || childHasSeqScan
	}
	return usesPrimaryKey, hasWorkItemsSeqScan
}
