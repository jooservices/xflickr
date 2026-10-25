package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestCycleSchedulesPeopleInOrderBeforeWrapping(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	people := []string{"cycle-a-" + suffix, "cycle-b-" + suffix, "cycle-c-" + suffix}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_cycles WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_interests WHERE user_id=$1`, userID)
	})
	for _, person := range people {
		if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via) VALUES($1,$2,0,'test')`, userID, person); err != nil {
			t.Fatal(err)
		}
	}
	repository, err := NewCycleRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := pool.Exec(ctx, `UPDATE crawl_interests SET last_checked_at=$3 WHERE user_id=$1 AND person_nsid=$2`, userID, people[1], now); err != nil {
		t.Fatal(err)
	}
	for index, want := range people {
		queued, err := repository.ScheduleNext(ctx, userID, now, 24*time.Hour)
		if index == 1 {
			if err != nil || queued {
				t.Fatalf("ScheduleNext() for not-yet-due %q = %v, %v; want cursor visit only", want, queued, err)
			}
			var cursor string
			if err := pool.QueryRow(ctx, `SELECT cursor_person_nsid FROM crawl_cycles WHERE user_id=$1`, userID).Scan(&cursor); err != nil {
				t.Fatal(err)
			}
			if cursor != want {
				t.Fatalf("cursor after not-yet-due visit = %q, want %q", cursor, want)
			}
			continue
		}
		if err != nil || !queued {
			t.Fatalf("ScheduleNext() = %v, %v; want queued", queued, err)
		}
		var got string
		if err := pool.QueryRow(ctx, `SELECT subject_key FROM work_items WHERE user_id=$1 AND kind='check' AND state='pending' ORDER BY id DESC LIMIT 1`, userID).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("scheduled %q, want %q", got, want)
		}
		if _, err := pool.Exec(ctx, `UPDATE work_items SET state='done' WHERE user_id=$1 AND kind='check' AND subject_key=$2`, userID, want); err != nil {
			t.Fatal(err)
		}
	}
	queued, err := repository.ScheduleNext(ctx, userID, now.Add(24*time.Hour), 24*time.Hour)
	if err != nil || !queued {
		t.Fatalf("wrapped ScheduleNext() = %v, %v; want queued", queued, err)
	}
	var wrapped string
	if err := pool.QueryRow(ctx, `SELECT subject_key FROM work_items WHERE user_id=$1 AND kind='check' AND state='pending' ORDER BY id DESC LIMIT 1`, userID).Scan(&wrapped); err != nil {
		t.Fatal(err)
	}
	if wrapped != people[0] {
		t.Fatalf("cycle wrapped to %q, want %q", wrapped, people[0])
	}
}

func TestCycleAssignsOwnerAccountKeyToWorkItems(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	person := "cycle-key-" + suffix
	var keyID int64
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,'cycle key',$2,'{}',3600,4,5) RETURNING id`, userID, "ck-"+suffix).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_cycles WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_interests WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM flickr_accounts WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM api_keys WHERE id=$1`, keyID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed) VALUES($1,$2,$3,'owner','{}','{}')`, userID, keyID, "owner-"+suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via) VALUES($1,$2,0,'test')`, userID, person); err != nil {
		t.Fatal(err)
	}
	repository, err := NewCycleRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	if queued, err := repository.ScheduleNext(ctx, userID, time.Now().UTC(), 24*time.Hour); err != nil || !queued {
		t.Fatalf("ScheduleNext() = %v, %v; want queued", queued, err)
	}
	var missing int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM work_items WHERE user_id=$1 AND key_id IS DISTINCT FROM $2`, userID, keyID).Scan(&missing); err != nil {
		t.Fatal(err)
	}
	var total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM work_items WHERE user_id=$1`, userID).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total == 0 || missing != 0 {
		t.Fatalf("work items total=%d without owner key=%d", total, missing)
	}
}
