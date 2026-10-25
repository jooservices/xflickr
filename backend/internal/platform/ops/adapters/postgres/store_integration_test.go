package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/platform/ops"
	opspostgres "github.com/jooservices/xflickr/internal/platform/ops/adapters/postgres"
)

func TestSnapshotAndHealthLineIntegration(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../.."))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	pool := dbtest.Open(t, cfg)
	suffix := fmt.Sprintf("ops%d", time.Now().UnixNano())
	var userID, keyID int64
	photoQueued := time.Now().UnixNano()
	photoRunning := photoQueued + 1
	photoPending := photoQueued + 2
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'test','user') RETURNING id`, suffix+"@example.invalid").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID) })
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE args->'args'->>'photo_id' = ANY($1)`, []string{fmt.Sprint(photoQueued), fmt.Sprint(photoRunning)})
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id = ANY($1)`, []int64{photoQueued, photoRunning, photoPending})
	})
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst,status) VALUES($1,'ops',$2,'{}',10,1,1,'valid') RETURNING id`, userID, suffix).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,'ops','{}','{}','revoked')`, userID, keyID, suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO api_call_log(key_id,class,called_at) SELECT $1,'photo',now() - interval '2 minutes' FROM generate_series(1,4)`, keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO api_call_log(key_id,class,called_at) VALUES($1,'photo',now() - interval '30 minutes')`, keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,key_id,state) VALUES('person.photos',$1,$2,NULL,'pending'),('person.photos',$3,$2,$4,'pending')`, suffix+"a", userID, suffix+"b", keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,$1) ON CONFLICT DO NOTHING`, suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO photos(id,owner_nsid,last_update,visibility,sizes_status,sizes,raw,content_hash)
VALUES ($1,$2,now(),'public','provisional','[]','{}',decode(repeat('cd',32),'hex')),
       ($3,$2,now(),'public','provisional','[]','{}',decode(repeat('ef',32),'hex'))`, photoQueued, suffix, photoPending); err != nil {
		t.Fatal(err)
	}
	args := func(id int64) string {
		return fmt.Sprintf(`{"kind":"photo.sizes","args":{"photo_id":%d,"key_id":%d,"dedupe_key":"photo:%d:sizes"}}`, id, keyID, id)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO river_job(kind,args,queue,state,attempt,max_attempts) VALUES('xflickr',$1,'sync','available',0,3)`, args(photoQueued)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO river_job(kind,args,queue,state,attempt,max_attempts) VALUES('xflickr',$1,'sync','running',0,3)`, args(photoRunning)); err != nil {
		t.Fatal(err)
	}

	store := opspostgres.Store{Pool: pool}
	snapshot, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Counts.People < 1 || snapshot.WorkItems["pending"] < 2 || snapshot.Pending.Total < 2 || snapshot.Pending.WithoutKey < 1 || snapshot.Pending.Oldest.IsZero() {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if snapshot.Tokens["revoked"] < 1 || snapshot.CallsLast10Min < 4 {
		t.Fatalf("tokens=%v calls10=%d", snapshot.Tokens, snapshot.CallsLast10Min)
	}
	if snapshot.PhotoSizesJobs.Pending < 1 || snapshot.PhotoSizesJobs.Ready < 1 || snapshot.PhotoSizesJobs.Running < 1 {
		t.Fatalf("photo sizes lane counts = %+v", snapshot.PhotoSizesJobs)
	}
	var found bool
	for _, key := range snapshot.Keys {
		if key.KeyID == keyID {
			found = true
			if key.CallsLastHour != 5 || key.Limit != 10 || key.Remaining() != 5 || key.Label != "ops" {
				t.Fatalf("key=%+v", key)
			}
		}
	}
	if !found {
		t.Fatalf("key %d missing from %+v", keyID, snapshot.Keys)
	}

	logs := &bytes.Buffer{}
	monitor := &ops.Monitor{Service: ops.Service{Store: store}, Logger: slog.New(slog.NewTextHandler(logs, nil))}
	if err := monitor.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"msg=\"crawl health\"", "people=", "photos=", "photo_sizes=", "photo_sizes_pending=", "photo_sizes_ready=", "photo_sizes_running=", "favorites=", "albums=", "galleries=", "groups=", "work_items.pending=", "ready_jobs=", "river_jobs", "calls_last_hour=", "calls_last_10min=", "oldest_pending_age=", "token_status=revoked", "failed_work_items=", "dead_jobs="} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log line misses %q: %s", want, logs.String())
		}
	}
}

// failingPool fails the Nth query and delegates the rest to the real pool.
type failingPool struct {
	opspostgres.Querier
	failAt int
	calls  int
}

func (f *failingPool) next() bool {
	f.calls++
	return f.calls == f.failAt
}

func (f *failingPool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.next() {
		return nil, errors.New("injected query failure")
	}
	return f.Querier.Query(ctx, sql, args...)
}

func (f *failingPool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.next() {
		return failingRow{}
	}
	return f.Querier.QueryRow(ctx, sql, args...)
}

type failingRow struct{}

func (failingRow) Scan(...any) error { return errors.New("injected scan failure") }

func TestSnapshotReportsEveryQueryFailure(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../.."))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	pool := dbtest.Open(t, cfg)
	if _, err := (opspostgres.Store{}).Snapshot(t.Context()); err == nil {
		t.Fatal("expected error without pool")
	}
	for failAt := 1; failAt <= 40; failAt++ {
		failing := &failingPool{Querier: pool, failAt: failAt}
		_, err := opspostgres.Store{Pool: failing}.Snapshot(t.Context())
		if failing.calls < failAt {
			if err != nil {
				t.Fatalf("snapshot failed without injection: %v", err)
			}
			return
		}
		if err == nil {
			t.Fatalf("query %d failed silently", failAt)
		}
	}
	t.Fatal("snapshot issued more queries than expected")
}
