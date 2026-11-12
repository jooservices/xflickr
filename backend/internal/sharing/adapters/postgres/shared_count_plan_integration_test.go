package postgres_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestSharedRecordCountQueryAvoidsPhotoScan checks the shipped count query on a
// large photos table. The user has few observations, so the plan must probe
// the photos primary key rather than hash or scan every photo.
func TestSharedRecordCountQueryAvoidsPhotoScan(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	pool := sharedCountPool(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	_, source, _, _ := runtime.Caller(0)
	queryFile := filepath.Join(filepath.Dir(source), "queries", "sharing.sql")
	sqlText, err := os.ReadFile(queryFile)
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(sqlText), "SELECT")
	if start < 0 {
		t.Fatal("queries/sharing.sql has no SELECT statement")
	}
	query := strings.TrimSpace(string(sqlText)[start:])

	prefix := fmt.Sprintf("count-plan-%d", time.Now().UnixNano())
	var owner, ownerAccount, other, otherAccount, keyID int64
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(tx.QueryRow(ctx, `INSERT INTO users (email,password_hash,role) VALUES ($1,'hash','user') RETURNING id`, prefix+"-owner@example.invalid").Scan(&owner))
	must(tx.QueryRow(ctx, `INSERT INTO users (email,password_hash,role) VALUES ($1,'hash','user') RETURNING id`, prefix+"-other@example.invalid").Scan(&other))
	must(tx.QueryRow(ctx, `INSERT INTO api_keys (user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES ($1,'test',$2,'{}',100,10,10) RETURNING id`, owner, prefix).Scan(&keyID))
	must(tx.QueryRow(ctx, `INSERT INTO flickr_accounts (user_id,key_id,nsid,username,token_sealed,secret_sealed) VALUES ($1,$2,$3,'test','{}','{}') RETURNING id`, owner, keyID, prefix+"-owner@flickr").Scan(&ownerAccount))
	must(tx.QueryRow(ctx, `INSERT INTO flickr_accounts (user_id,key_id,nsid,username,token_sealed,secret_sealed) VALUES ($1,$2,$3,'test','{}','{}') RETURNING id`, other, keyID, prefix+"-other@flickr").Scan(&otherAccount))
	_, err = tx.Exec(ctx, `INSERT INTO people (nsid, username) VALUES ($1,'plan')`, prefix+"@flickr")
	must(err)
	_, err = tx.Exec(ctx, `INSERT INTO photos (id, owner_nsid, visibility, last_update, raw, content_hash) SELECT 1000000000000+value, $1, 'public', now(), '{}'::jsonb, decode(repeat('00',32),'hex') FROM generate_series(1,300000) value`, prefix+"@flickr")
	must(err)
	_, err = tx.Exec(ctx, `INSERT INTO observations (user_id,flickr_account_id,record_kind,record_id) SELECT $1,$2,'photo',(1000000000000+value)::text FROM generate_series(1,300000) value`, other, otherAccount)
	must(err)
	_, err = tx.Exec(ctx, `INSERT INTO observations (user_id,flickr_account_id,record_kind,record_id) SELECT $1,$2,'photo',(1000000000000+value)::text FROM generate_series(1,3) value`, owner, ownerAccount)
	must(err)
	_, err = tx.Exec(ctx, `UPDATE sharing_settings SET enabled=true WHERE user_id=$1`, owner)
	must(err)
	_, err = tx.Exec(ctx, `ANALYZE photos`)
	must(err)
	_, err = tx.Exec(ctx, `ANALYZE observations`)
	must(err)

	rows, err := tx.Query(ctx, `EXPLAIN (ANALYZE, BUFFERS, TIMING OFF) `+query, owner)
	must(err)
	var plan strings.Builder
	for rows.Next() {
		var line string
		must(rows.Scan(&line))
		plan.WriteString(line + "\n")
	}
	must(rows.Err())
	rows.Close()
	t.Logf("shared record count plan:\n%s", plan.String())
	if strings.Contains(plan.String(), "Seq Scan on photos") {
		t.Fatalf("shared record count scans photos:\n%s", plan.String())
	}
	if strings.Contains(plan.String(), "Seq Scan on observations") {
		t.Fatalf("shared record count scans observations:\n%s", plan.String())
	}
}
