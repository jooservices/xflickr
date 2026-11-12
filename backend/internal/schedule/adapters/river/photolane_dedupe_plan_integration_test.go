package river

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestPhotoDedupeCheckUsesTheExpressionIndex proves the "is this photo job already queued?" check that
// runs before every photo enqueue is served by river_job_photo_dedupe_key_idx instead of a river_job scan.
// The plan is not forced: the planner picks it on a realistic table of 100,000 active jobs.
func TestPhotoDedupeCheckUsesTheExpressionIndex(t *testing.T) {
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

	if _, err := tx.Exec(ctx, `INSERT INTO river_job(state,args,kind,queue)
		SELECT 'available',jsonb_build_object('args',jsonb_build_object('dedupe_key','other-'||g)),'photo.sizes','sync'
		FROM generate_series(1,100000) AS g`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO river_job(state,args,kind,queue)
		VALUES ('available',jsonb_build_object('args',jsonb_build_object('dedupe_key','wanted-key')),'photo.sizes','sync')`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `ANALYZE river_job`); err != nil {
		t.Fatal(err)
	}

	const dedupeSQL = `SELECT EXISTS (SELECT 1 FROM river_job WHERE state IN ('available','pending','scheduled','retryable','running') AND args->'args'->>'dedupe_key' = $1)`
	rows, err := tx.Query(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) `+strings.Replace(dedupeSQL, "$1", "'wanted-key'", 1))
	if err != nil {
		t.Fatal(err)
	}
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		plan = append(plan, line)
	}
	rows.Close()
	joined := strings.Join(plan, "\n")
	if !strings.Contains(joined, "river_job_photo_dedupe_key_idx") {
		t.Fatalf("dedupe check does not use river_job_photo_dedupe_key_idx:\n%s", joined)
	}
	if strings.Contains(joined, "Seq Scan on river_job") {
		t.Fatalf("dedupe check scans river_job sequentially:\n%s", joined)
	}

	var queued bool
	if err := tx.QueryRow(ctx, dedupeSQL, "wanted-key").Scan(&queued); err != nil || !queued {
		t.Fatalf("queued = %t, error = %v; want true", queued, err)
	}
	if err := tx.QueryRow(ctx, dedupeSQL, "absent-key").Scan(&queued); err != nil || queued {
		t.Fatalf("queued = %t, error = %v; want false", queued, err)
	}
}
