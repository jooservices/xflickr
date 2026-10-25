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
