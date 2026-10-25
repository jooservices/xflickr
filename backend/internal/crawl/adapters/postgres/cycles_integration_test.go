package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	catalogpg "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	"github.com/jooservices/xflickr/internal/crawl/app"
	"github.com/jooservices/xflickr/internal/crawl/domain"
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

// seedOwnerKey creates an API key for the user and, when accountStatus is not
// empty, a Flickr account with that status bound to the key.
func seedOwnerKey(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID int64, accountStatus string) int64 {
	t.Helper()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var keyID int64
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,'owner key',$2,'{}',3600,4,5) RETURNING id`, userID, "ok-"+suffix).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_cycles WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_interests WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM flickr_accounts WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM api_keys WHERE id=$1`, keyID)
	})
	if accountStatus != "" {
		if _, err := pool.Exec(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,'owner','{}','{}',$4)`, userID, keyID, "owner-"+suffix, accountStatus); err != nil {
			t.Fatal(err)
		}
	}
	return keyID
}

func newPoolEngine(t *testing.T, pool *pgxpool.Pool) *Engine {
	t.Helper()
	store, err := NewEngine(pool, func(conn Conn) app.CatalogWriter {
		repo, repoErr := catalogpg.New(conn)
		if repoErr != nil {
			t.Fatal(repoErr)
		}
		return repo
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func workItemKey(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID int64, subject string) (*int64, string) {
	t.Helper()
	var key *int64
	var state string
	if err := pool.QueryRow(ctx, `SELECT key_id, state FROM work_items WHERE user_id=$1 AND subject_key=$2`, userID, subject).Scan(&key, &state); err != nil {
		t.Fatal(err)
	}
	return key, state
}

func TestEnqueueAssignsKeyOnlyFromOkAccount(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	okUser := insertInterestTestUser(t, ctx, pool)
	okKey := seedOwnerKey(t, ctx, pool, okUser, "ok")
	brokenUser := insertInterestTestUser(t, ctx, pool)
	seedOwnerKey(t, ctx, pool, brokenUser, "reconnect_required")
	store := newPoolEngine(t, pool)
	enqueue := func(userID int64, subject string, reopen bool) {
		t.Helper()
		if err := store.Do(ctx, func(w app.Writers) error {
			return w.Work.Enqueue(ctx, app.NewWork{Kind: "photos", Subject: subject, UserID: userID, Reopen: reopen, Cursor: domain.Cursor{Page: 1, Method: "flickr.people.getPhotos"}})
		}); err != nil {
			t.Fatal(err)
		}
	}

	enqueue(okUser, "new-"+suffix, false)
	if key, state := workItemKey(t, ctx, pool, okUser, "new-"+suffix); key == nil || *key != okKey || state != "pending" {
		t.Fatalf("new item key=%v state=%s, want key %d pending", key, state, okKey)
	}

	enqueue(brokenUser, "broken-"+suffix, false)
	if key, _ := workItemKey(t, ctx, pool, brokenUser, "broken-"+suffix); key != nil {
		t.Fatalf("item of a user without an ok account got key %d, want NULL", *key)
	}

	// A finished item that never had a key is reopened with the owner's key.
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state,attempts,last_error) VALUES('photos',$1,$2,'done',2,'old')`, "reopen-"+suffix, okUser); err != nil {
		t.Fatal(err)
	}
	enqueue(okUser, "reopen-"+suffix, true)
	if key, state := workItemKey(t, ctx, pool, okUser, "reopen-"+suffix); key == nil || *key != okKey || state != "pending" {
		t.Fatalf("reopened item key=%v state=%s, want key %d pending", key, state, okKey)
	}

	// Reopening for a user without an ok account must not erase an existing key.
	other := insertInterestTestUser(t, ctx, pool)
	otherKey := seedOwnerKey(t, ctx, pool, other, "revoked")
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,key_id,state) VALUES('photos',$1,$2,$3,'failed')`, "keep-"+suffix, other, otherKey); err != nil {
		t.Fatal(err)
	}
	enqueue(other, "keep-"+suffix, true)
	if key, state := workItemKey(t, ctx, pool, other, "keep-"+suffix); key == nil || *key != otherKey || state != "pending" {
		t.Fatalf("reopened item key=%v state=%s, want kept key %d pending", key, state, otherKey)
	}
}

func TestCycleContactsPhotosAssignsKeyAndSkipsNonOkAccount(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	repository, err := NewCycleRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		status  string
		wantKey bool
	}{{"ok", true}, {"reconnect_required", false}} {
		t.Run(tc.status, func(t *testing.T) {
			userID := insertInterestTestUser(t, ctx, pool)
			keyID := seedOwnerKey(t, ctx, pool, userID, tc.status)
			person := fmt.Sprintf("cp-%s-%d", tc.status, time.Now().UnixNano())
			if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via) VALUES($1,$2,0,'test')`, userID, person); err != nil {
				t.Fatal(err)
			}
			// Cursor past the only person with an old cycle start: the next visit wraps.
			if _, err := pool.Exec(ctx, `INSERT INTO crawl_cycles(user_id,cursor_person_nsid,started_at) VALUES($1,'zzzz',now()-interval '48 hours') ON CONFLICT (user_id) DO UPDATE SET cursor_person_nsid='zzzz', started_at=now()-interval '48 hours'`, userID); err != nil {
				t.Fatal(err)
			}
			if queued, err := repository.ScheduleNext(ctx, userID, time.Now().UTC(), 24*time.Hour); err != nil || !queued {
				t.Fatalf("ScheduleNext() = %v, %v; want queued", queued, err)
			}
			var key *int64
			err := pool.QueryRow(ctx, `SELECT key_id FROM work_items WHERE user_id=$1 AND kind='contacts_photos'`, userID).Scan(&key)
			if !tc.wantKey {
				if !errors.Is(err, pgx.ErrNoRows) {
					t.Fatalf("contacts_photos for non-ok account: key=%v err=%v, want no row", key, err)
				}
				return
			}
			if err != nil || key == nil || *key != keyID {
				t.Fatalf("contacts_photos key=%v err=%v, want %d", key, err, keyID)
			}
			// Done -> pending reopen keeps the owner key.
			if _, err := pool.Exec(ctx, `UPDATE work_items SET state='done', key_id=NULL WHERE user_id=$1 AND kind='contacts_photos'`, userID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE crawl_cycles SET cursor_person_nsid='zzzz', started_at=now()-interval '48 hours' WHERE user_id=$1`, userID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE work_items SET state='done' WHERE user_id=$1 AND kind='check'`, userID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE crawl_interests SET last_checked_at=NULL WHERE user_id=$1`, userID); err != nil {
				t.Fatal(err)
			}
			if queued, err := repository.ScheduleNext(ctx, userID, time.Now().UTC(), 24*time.Hour); err != nil || !queued {
				t.Fatalf("second ScheduleNext() = %v, %v; want queued", queued, err)
			}
			var state string
			if err := pool.QueryRow(ctx, `SELECT key_id, state FROM work_items WHERE user_id=$1 AND kind='contacts_photos'`, userID).Scan(&key, &state); err != nil || key == nil || *key != keyID || state != "pending" {
				t.Fatalf("reopened contacts_photos key=%v state=%s err=%v, want %d pending", key, state, err, keyID)
			}
		})
	}
}
