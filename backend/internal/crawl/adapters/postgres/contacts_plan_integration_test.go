package postgres

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestContactsReconcilePlanUsesKeysetIndex(t *testing.T) {
	if os.Getenv("XFLICKR_CONTACTS_PLAN_TEST") != "1" {
		t.Skip("set XFLICKR_CONTACTS_PLAN_TEST=1 to build and explain the 400k-row fixture")
	}
	pool := openEngineTestPool(t)
	conn, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	tx, err := conn.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(t.Context()) }()

	if _, err := tx.Exec(t.Context(), `CREATE TEMP TABLE contacts (
		person_nsid TEXT NOT NULL,
		contact_nsid TEXT NOT NULL,
		seen_at TIMESTAMPTZ NOT NULL,
		PRIMARY KEY (person_nsid, contact_nsid)
	) ON COMMIT DROP`); err != nil {
		t.Fatal(err)
	}
	scannedAt := time.Now().UTC().Truncate(time.Second)
	if _, err := tx.Exec(t.Context(), `
		INSERT INTO contacts (person_nsid, contact_nsid, seen_at)
		SELECT 'fixture-account', lpad(n::text, 8, '0'),
		       CASE WHEN n <= 320000 THEN $1::timestamptz ELSE $2::timestamptz END
		FROM generate_series(1, 400000) AS n`, scannedAt.Add(-24*time.Hour), scannedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(t.Context(), `ANALYZE contacts`); err != nil {
		t.Fatal(err)
	}

	rows, err := tx.Query(t.Context(), "EXPLAIN (ANALYZE, BUFFERS) "+deleteStaleContactsBatchSQL,
		"fixture-account", "", scannedAt, contactsReconcileBatchSize)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	text := plan.String()
	t.Logf("400k contacts reconcile plan:\n%s", text)
	if strings.Contains(text, "Seq Scan on contacts") {
		t.Fatal("contacts reconciliation plan uses a sequential scan")
	}
	if !strings.Contains(text, "Index Scan using contacts_pkey") {
		t.Fatal("contacts reconciliation plan does not use contacts_pkey")
	}
}
