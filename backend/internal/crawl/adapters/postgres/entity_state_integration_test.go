package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/crawl/app"
)

func TestEntityStatesPersistFirstOutcomeAndIsolateFacets(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx := t.Context()
	s := EntityStates{DB: pool}
	now := time.Now().UTC().Truncate(time.Second)
	id := fmt.Sprintf("entity-%d", now.UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM entity_states WHERE entity_id=$1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM entity_facet_restrictions WHERE entity_id=$1`, id)
	})
	for _, kind := range []string{"person", "album", "gallery", "group"} {
		t.Run(kind, func(t *testing.T) {
			a := app.EntityAccess{EntityRef: app.EntityRef{Kind: kind, ID: id}, Facet: "list", UserID: 10, KeyID: 20}
			if err := s.RecordFailure(ctx, a, "restricted", "permission_denied", now, now.Add(30*24*time.Hour)); err != nil {
				t.Fatal(err)
			}
			blocked := func(a app.EntityAccess, at time.Time, want bool) {
				t.Helper()
				got, err := s.Suppressed(ctx, a, at)
				if err != nil || got != want {
					t.Fatalf("blocked %v want %v err %v", got, want, err)
				}
			}
			blocked(a, now, true)
			other := a
			other.Facet = "other"
			blocked(other, now, false)
			other = a
			other.UserID++
			blocked(other, now, false)
			other = a
			other.KeyID++
			blocked(other, now, false)
			blocked(a, now.Add(30*24*time.Hour), false)
			later := now.Add(31 * 24 * time.Hour)
			if err := s.RecordFailure(ctx, a, "restricted", "changed", later, later.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			var restrictedFirst time.Time
			var restrictedReason string
			if err := pool.QueryRow(ctx, `SELECT first_seen_at,reason FROM entity_facet_restrictions WHERE entity_type=$1 AND entity_id=$2`, kind, id).Scan(&restrictedFirst, &restrictedReason); err != nil || !restrictedFirst.Equal(now) || restrictedReason != "permission_denied" {
				t.Fatal("restriction history changed", err)
			}
			blocked(a, later, true)

			if err := s.RecordFailure(ctx, a, "gone", "not_found", now, now); err != nil {
				t.Fatal(err)
			}
			if err := s.RecordFailure(ctx, a, "gone", "changed", now.Add(time.Hour), now); err != nil {
				t.Fatal(err)
			}
			blocked(other, now.Add(365*24*time.Hour), true)
			var first time.Time
			var reason string
			if err := pool.QueryRow(ctx, `SELECT first_seen_at,reason FROM entity_states WHERE entity_type=$1 AND entity_id=$2`, kind, id).Scan(&first, &reason); err != nil || !first.Equal(now) || reason != "not_found" {
				t.Fatalf("%s %s %v", first, reason, err)
			}
		})
	}
	a := app.EntityAccess{EntityRef: app.EntityRef{Kind: "photo", ID: id}, Facet: "exif", KeyID: 20, Version: now}
	for _, facet := range []string{"sizes", "info", "exif", "contexts", "comments", "photo_favorites"} {
		a.Facet = facet
		if err := s.RecordFailure(ctx, a, "restricted", "not_found", now, now); err != nil {
			t.Fatal(err)
		}
		if got, err := s.Suppressed(ctx, a, now.Add(365*24*time.Hour)); err != nil || !got {
			t.Fatalf("photo interval reopened %v %v", got, err)
		}
		independent := a
		independent.Facet = "unrestricted"
		if got, err := s.Suppressed(ctx, independent, now); err != nil || got {
			t.Fatal("restricted photo blocked another facet", err)
		}
		other := a
		other.Version = now.Add(time.Second)
		if got, err := s.Suppressed(ctx, other, now); err != nil || got {
			t.Fatalf("new version suppressed %v %v", got, err)
		}
		other = a
		other.KeyID++
		if got, err := s.Suppressed(ctx, other, now); err != nil || got {
			t.Fatalf("other caller suppressed %v %v", got, err)
		}
	}
}

func TestPhotoRestrictionReopensOnVisibilityAndSkipsTombstones(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx := t.Context()
	s := EntityStates{DB: pool}
	now := time.Now().UTC().Truncate(time.Second)
	id := now.UnixNano()%1000000000 + 900000000000
	owner := fmt.Sprintf("state-photo-%d", id)
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,$1)`, owner); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM entity_facet_restrictions WHERE entity_type='photo' AND entity_id=$1`, fmt.Sprint(id))
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id=$1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, owner)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO photos(id,owner_nsid,last_update,visibility,raw,content_hash) VALUES($1,$2,$3,'public','{}',decode(repeat('ab',32),'hex'))`, id, owner, now); err != nil {
		t.Fatal(err)
	}
	a := app.EntityAccess{EntityRef: app.EntityRef{Kind: "photo", ID: fmt.Sprint(id)}, Facet: "sizes", KeyID: 20, Version: now}
	if err := s.RecordFailure(ctx, a, "restricted", "permission_denied", now, now); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.PhotoFacetAllowed(ctx, id, 20, "sizes"); err != nil || ok {
		t.Fatalf("restricted scheduled %v %v", ok, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE photos SET visibility='private' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.PhotoFacetAllowed(ctx, id, 20, "sizes"); err != nil || !ok {
		t.Fatalf("visibility did not reopen %v %v", ok, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE photos SET tombstoned_at=now() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.PhotoFacetAllowed(ctx, id, 20, "sizes"); err != nil || ok {
		t.Fatalf("tombstone scheduled %v %v", ok, err)
	}
}

func TestPhotoFacetEligibilityUsesTheSelectedKeyOwner(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx := t.Context()
	userID := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("state-photo-key-%d", time.Now().UnixNano())
	owner := suffix + "-owner"
	var keyID int64
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys
		(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst,status)
		VALUES($1,'restriction',$2,'{}',3600,4,5,'valid') RETURNING id`, userID, "key-"+suffix).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	photoID := now.UnixNano()%1_000_000_000 + 900_000_000_000
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,$1)`, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO photos(id,owner_nsid,last_update,visibility,raw,content_hash)
		VALUES($1,$2,$3,'public','{}',decode(repeat('ab',32),'hex'))`, photoID, owner, now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM entity_facet_restrictions WHERE entity_type='photo' AND entity_id=$1`, fmt.Sprint(photoID))
		_, _ = pool.Exec(bg, `DELETE FROM photos WHERE id=$1`, photoID)
		_, _ = pool.Exec(bg, `DELETE FROM people WHERE nsid=$1`, owner)
		_, _ = pool.Exec(bg, `DELETE FROM api_keys WHERE id=$1`, keyID)
		_, _ = pool.Exec(bg, `DELETE FROM users WHERE id=$1`, userID)
	})
	access := app.EntityAccess{
		EntityRef: app.EntityRef{Kind: "photo", ID: fmt.Sprint(photoID)},
		Facet:     "sizes",
		UserID:    userID,
		KeyID:     keyID,
		Version:   now,
	}
	states := EntityStates{DB: pool}
	if err := states.RecordFailure(ctx, access, "restricted", "permission_denied", now, now); err != nil {
		t.Fatal(err)
	}
	if allowed, err := states.PhotoFacetAllowed(ctx, photoID, keyID, "sizes"); err != nil || allowed {
		t.Fatalf("PhotoFacetAllowed() = %v, %v; want the selected key's restriction to block scheduling", allowed, err)
	}
}

func TestPhotoSizeRestrictionDistinguishesAccountsSharingAKey(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx := t.Context()
	userID := insertInterestTestUser(t, ctx, pool)
	var keyID, firstAccount, secondAccount int64
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst,status)
		VALUES($1,'shared','key-shared','{}',100,2,4,'valid') RETURNING id`, userID).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	for _, account := range []struct {
		nsid string
		id   *int64
	}{{"account-one", &firstAccount}, {"account-two", &secondAccount}} {
		if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status)
			VALUES($1,$2,$3,$3,'{}','{}','ok') RETURNING id`, userID, keyID, account.nsid).Scan(account.id); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	access := app.EntityAccess{EntityRef: app.EntityRef{Kind: "photo", ID: "account-shared-key-photo"}, Facet: "sizes", UserID: userID, KeyID: keyID, AccountID: firstAccount, Version: now}
	states := EntityStates{DB: pool}
	if err := states.RecordFailure(ctx, access, "restricted", "permission_denied", now, now); err != nil {
		t.Fatal(err)
	}
	blocked, err := states.Suppressed(ctx, access, now)
	if err != nil || !blocked {
		t.Fatalf("failed account restriction = %v, %v", blocked, err)
	}
	access.AccountID = secondAccount
	blocked, err = states.Suppressed(ctx, access, now)
	if err != nil || blocked {
		t.Fatalf("same-key second account restriction = %v, %v", blocked, err)
	}
}

func TestCycleSkipsGonePersonForChecksAndReconciliation(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx := t.Context()
	user := insertInterestTestUser(t, ctx, pool)
	person := fmt.Sprintf("gone-cycle-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1`, user)
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_cycles WHERE user_id=$1`, user)
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_interests WHERE user_id=$1`, user)
		_, _ = pool.Exec(context.Background(), `DELETE FROM entity_states WHERE entity_type='person' AND entity_id=$1`, person)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via) VALUES($1,$2,0,'test')`, user, person); err != nil {
		t.Fatal(err)
	}
	states := EntityStates{DB: pool}
	now := time.Now()
	if err := states.RecordFailure(ctx, app.EntityAccess{EntityRef: app.EntityRef{Kind: "person", ID: person}}, "gone", "not_found", now, now); err != nil {
		t.Fatal(err)
	}
	repo, err := NewCycleRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	if queued, err := repo.ScheduleNext(ctx, user, now, time.Hour, time.Hour); err != nil || queued {
		t.Fatal(queued, err)
	}
	if n := count(t, ctx, pool, `SELECT count(*) FROM work_items WHERE user_id=$1 AND subject_key=$2`, user, person); n != 0 {
		t.Fatalf("queued %d jobs for gone person", n)
	}
}
