package queuepostgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/schedule/adapters/queuepostgres"
)

func TestQueueSnapshotsAndEventIntegration(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	pool := dbtest.Open(t, cfg)
	store := queuepostgres.New(pool)
	suffix := fmt.Sprintf("queue-%d", time.Now().UnixNano())
	var userID, keyID int64
	var eventCursor int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(max(id),0) FROM outbox_events`).Scan(&eventCursor); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'test','user') RETURNING id`, suffix+"@example.invalid").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE args->>'test_subject' LIKE $1`, suffix+"%")
		_, _ = pool.Exec(context.Background(), `DELETE FROM outbox_events WHERE topic='queue.state' AND key='global' AND id>$1`, eventCursor)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst,status) VALUES($1,'integration',$2,'{}',1000,1,1,'valid') RETURNING id`, userID, suffix).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,'integration','{}','{}','ok')`, userID, keyID, suffix); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ kind, subject string }{{"person", suffix + "-person-1"}, {"person", suffix + "-person-2"}, {"photo_sizes", suffix + "-sizes"}} {
		if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,key_id,state,created_at) VALUES($1,$2,$3,$4,'pending',now()-interval '90 seconds')`, item.kind, item.subject, userID, keyID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO river_job(kind,args,queue,state) VALUES('queue.integration',$1,'sync','available')`, json.RawMessage(fmt.Sprintf(`{"test_subject":%q}`, suffix+"-river"))); err != nil {
		t.Fatal(err)
	}
	lanes, err := store.UserQueue(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	var reserve, daily int64
	var personShare float64
	if err := pool.QueryRow(ctx, `SELECT (reserve.value #>> '{}')::bigint,(daily.value #>> '{}')::bigint,(share.value #>> '{}')::float8 FROM app_settings reserve CROSS JOIN app_settings daily CROSS JOIN app_settings share WHERE reserve.key='quota_reserve' AND daily.key='daily_call_limit' AND share.key='person_lane_share'`).Scan(&reserve, &daily, &personShare); err != nil {
		t.Fatal(err)
	}
	base := int64(1000)
	if daily > 0 && daily < base {
		base = daily
	}
	personCapacity := math.Max(0, float64(base-reserve))
	sizesCapacity := math.Min(math.Floor(float64(base)*(1-personShare)), personCapacity)
	if len(lanes) != 2 || lanes[0].Lane != "person" || lanes[0].BacklogCalls != 2 || lanes[0].EffectiveCallsPerHour != personCapacity || lanes[1].Lane != "sizes" || lanes[1].BacklogCalls != 1 || lanes[1].EffectiveCallsPerHour != sizesCapacity {
		t.Fatalf("user queue=%+v", lanes)
	}
	admin, err := store.AdminQueue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if admin.OldestItemAgeSeconds < 89 || len(admin.ReadyQueueSizes) == 0 {
		t.Fatalf("admin queue=%+v", admin)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state) VALUES('person',$1,$2,'pending')`, suffix+"-keyless", userID); err != nil {
		t.Fatal(err)
	}
	if err := store.PublishQueueState(ctx); err != nil {
		t.Fatal(err)
	}
	var eventCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE topic='queue.state' AND key='global' AND id>$1 AND payload ? 'backlog_by_lane' AND payload ? 'ready_jobs' AND (payload->>'pending_without_key')::bigint >= 1`, eventCursor).Scan(&eventCount); err != nil || eventCount != 1 {
		t.Fatalf("queue.state events=%d err=%v", eventCount, err)
	}
}
