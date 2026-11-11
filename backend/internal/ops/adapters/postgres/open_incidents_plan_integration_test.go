package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestOpenIncidentsPlan(t *testing.T) {
	if os.Getenv("XFLICKR_INCIDENT_PLAN_TEST") != "1" {
		t.Skip("run docker/scripts/incident-open-plan.sh to measure the open incident query")
	}
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if err := tx.Rollback(cleanupCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("roll back incident plan fixtures: %v", err)
		}
	}()

	kind := fmt.Sprintf("plan.open-list.%d", time.Now().UnixNano())
	_, err = tx.Exec(ctx, `
INSERT INTO incidents(kind, scope, severity, title, state, first_seen, last_seen, count)
SELECT $1, 'plan:' || fixture_id::text, 'info', 'plan fixture', 'open',
       statement_timestamp(), statement_timestamp(), 1
FROM generate_series(1, 400000) AS fixture(fixture_id)`, kind)
	if err != nil {
		t.Fatalf("insert plan fixtures: %v", err)
	}
	if _, err := tx.Exec(ctx, "ANALYZE incidents"); err != nil {
		t.Fatalf("analyze plan fixtures: %v", err)
	}

	rows, err := tx.Query(ctx, `EXPLAIN (ANALYZE, BUFFERS)
SELECT id, severity, first_seen, last_seen, count
FROM incidents
WHERE state = 'open'
ORDER BY last_seen DESC, id DESC
LIMIT 51`)
	if err != nil {
		t.Fatalf("explain open incident page query: %v", err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("read query plan: %v", err)
		}
		fmt.Fprintln(&plan, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate query plan: %v", err)
	}
	result := plan.String()
	t.Logf("EXPLAIN (ANALYZE, BUFFERS) on 400000 transaction-local incident rows:\n%s", result)
	if strings.Contains(result, "Seq Scan") {
		t.Fatalf("open incident page query used a sequential scan:\n%s", result)
	}
	if !strings.Contains(result, "incidents_open_last_seen_id_idx") {
		t.Fatalf("open incident page query did not use incidents_open_last_seen_id_idx:\n%s", result)
	}
}
