package observations

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestBackfillWorktreeBatchesAndGuards(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("integration gate not enabled")
	}
	_, source, _, _ := runtime.Caller(0)
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(filepath.Dir(source), "../../../../../..", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name == "xflickr_test" {
		t.Fatal("worktree database required")
	}
	pool := dbtest.Open(t, cfg)
	ctx := t.Context()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	// Temporary tables shadow the real schema on this connection only. No
	// existing users or catalog data are changed to exercise the singleton guard.
	for _, table := range []string{"users", "flickr_accounts", "people", "photos", "observations"} {
		_, err = conn.Exec(ctx, `CREATE TEMP TABLE `+table+` (LIKE public.`+table+` INCLUDING DEFAULTS INCLUDING CONSTRAINTS INCLUDING INDEXES)`)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Exec(context.Background(), `DROP TABLE IF EXISTS pg_temp.`+table)
	}
	for _, sql := range []string{
		`INSERT INTO users(id,email,password_hash,role) VALUES(1,'backfill@example.test','unused','user')`,
		`INSERT INTO flickr_accounts(id,user_id,key_id,nsid,username,token_sealed,secret_sealed) VALUES(2,1,3,'backfill','backfill','{}','{}')`,
		`INSERT INTO people(nsid,username,last_seen_at) VALUES('a','a','2020-01-01'),('b','b',NULL),('c','c','2021-01-01')`,
		`INSERT INTO photos(id,owner_nsid,title,last_seen_at,last_update,visibility,raw,content_hash) VALUES(10,'a','legacy','2020-02-01',now(),'public','{}',decode(repeat('00',32),'hex')),(20,'b','legacy','2021-02-01',now(),'private','{}',decode(repeat('00',32),'hex'))`,
	} {
		if _, err = conn.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	options := BackfillOptions{UserID: 1, AccountID: 2, BatchSize: 1}
	before := time.Now().UTC()
	report, err := Backfill(ctx, conn, options)
	if err != nil {
		t.Fatal(err)
	}
	if report.Inserted != 5 || report.Batches != 7 || report.Users != 1 || report.Accounts != 1 {
		t.Fatalf("report=%+v", report)
	}
	var seen time.Time
	if err = conn.QueryRow(ctx, `SELECT first_seen_at FROM observations WHERE record_kind='person' AND record_id='b'`).Scan(&seen); err != nil {
		t.Fatal(err)
	}
	if seen.Before(before) || !seen.Equal(report.RunAt.Truncate(time.Microsecond)) {
		t.Fatalf("null fallback=%s run=%s", seen, report.RunAt)
	}
	var exact bool
	if err = conn.QueryRow(ctx, `SELECT first_seen_at='2020-02-01'::timestamptz FROM observations WHERE record_kind='photo' AND record_id='10'`).Scan(&exact); err != nil || !exact {
		t.Fatalf("historical timestamp: %v %v", exact, err)
	}
	again, err := Backfill(ctx, conn, options)
	if err != nil || again.Inserted != 0 {
		t.Fatalf("repeat=%+v %v", again, err)
	}
	var unchanged time.Time
	_ = conn.QueryRow(ctx, `SELECT first_seen_at FROM observations WHERE record_kind='person' AND record_id='b'`).Scan(&unchanged)
	if !unchanged.Equal(seen) {
		t.Fatal("repeat changed first seen")
	}
	// Resume a partially populated catalog without changing existing timestamps.
	if _, err = conn.Exec(ctx, `DELETE FROM observations WHERE record_id='20'`); err != nil {
		t.Fatal(err)
	}
	resumed, err := Backfill(ctx, conn, options)
	if err != nil || resumed.Inserted != 1 {
		t.Fatalf("resume=%+v %v", resumed, err)
	}
	for _, bad := range []BackfillOptions{{UserID: 99, AccountID: 2, BatchSize: 1}, {UserID: 1, AccountID: 99, BatchSize: 1}} {
		got, err := Backfill(ctx, conn, bad)
		if err == nil || got.Inserted != 0 {
			t.Fatalf("bad attribution=%+v %v", got, err)
		}
	}
	if _, err = conn.Exec(ctx, `INSERT INTO users(id,email,password_hash,role) VALUES(9,'second@example.test','unused','user')`); err != nil {
		t.Fatal(err)
	}
	refused, err := Backfill(ctx, conn, options)
	if err == nil || refused.Inserted != 0 || refused.Users != 2 || refused.People != 3 || refused.Photos != 2 {
		t.Fatalf("multi-user=%+v %v", refused, err)
	}
	if _, err = conn.Exec(ctx, `DELETE FROM users WHERE id=9; INSERT INTO flickr_accounts(id,user_id,key_id,nsid,username,token_sealed,secret_sealed) VALUES(9,1,3,'second','second','{}','{}')`); err != nil {
		t.Fatal(err)
	}
	refused, err = Backfill(ctx, conn, options)
	if err == nil || refused.Inserted != 0 || refused.Accounts != 2 {
		t.Fatalf("multi-account=%+v %v", refused, err)
	}
}
