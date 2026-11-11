package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	catalogapp "github.com/jooservices/xflickr/internal/catalog/app"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestPhotoLaneSelectsOnlyStarredOrActiveSelectedContacts(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := dbtest.Open(t, loadCatalogDatabase(t))
	base := time.Now().UnixNano() % 1_000_000_000 * 100
	suffix := fmt.Sprintf("lane-%d", time.Now().UnixNano())
	var userA, userB int64
	for i, dest := range []*int64{&userA, &userB} {
		if err := pool.QueryRow(ctx, `INSERT INTO users (email,password_hash,role) VALUES ($1,'integration-hash','user') RETURNING id`, fmt.Sprintf("%s-%d@example.test", suffix, i)).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	people := map[string]string{}
	for _, name := range []string{"plain", "shallow", "starred", "selected", "expired"} {
		people[name] = suffix + "-" + name
	}
	t.Cleanup(func() {
		bg := context.Background()
		for _, nsid := range people {
			_, _ = pool.Exec(bg, `DELETE FROM photos WHERE owner_nsid=$1`, nsid)
			_, _ = pool.Exec(bg, `DELETE FROM people WHERE nsid=$1`, nsid)
		}
		_, _ = pool.Exec(bg, `DELETE FROM users WHERE id = ANY($1)`, []int64{userA, userB})
	})
	for _, nsid := range people {
		if _, err := pool.Exec(ctx, `INSERT INTO people (nsid, username) VALUES ($1,$1)`, nsid); err != nil {
			t.Fatal(err)
		}
	}
	interest := func(user int64, name string, depth int, starred bool, selected string) {
		if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests (user_id, person_nsid, depth, via, starred, selected_until) VALUES ($1,$2,$3,'test',$4, CASE WHEN $5 = '' THEN NULL ELSE now() + $5::interval END)`, user, people[name], depth, starred, selected); err != nil {
			t.Fatal(err)
		}
	}
	interest(userA, "plain", 3, false, "")
	interest(userA, "shallow", 1, false, "")
	interest(userB, "shallow", 4, false, "")
	interest(userA, "starred", 2, true, "")
	interest(userB, "selected", 5, false, "1 hour")
	interest(userA, "expired", 0, false, "-1 hour")

	photo := func(id int64, name, status string, uploadedAgo time.Duration, tombstoned bool) {
		if _, err := pool.Exec(ctx, `INSERT INTO photos (id, owner_nsid, last_update, visibility, sizes_status, sizes_checked_at, uploaded_at, tombstoned_at, raw, content_hash)
			VALUES ($1,$2,now(),'public',$3, CASE WHEN $3='confirmed' THEN now() END, now() - $4::interval, CASE WHEN $5 THEN now() END, '{}', decode(repeat('ab',32),'hex'))`, id, people[name], status, fmt.Sprintf("%d seconds", int(uploadedAgo.Seconds())), tombstoned); err != nil {
			t.Fatal(err)
		}
	}
	// Per person: newest provisional first; confirmed and tombstoned photos never appear.
	photo(base+1, "selected", "none", 2*time.Hour, false)
	photo(base+2, "selected", "provisional", time.Hour, false)
	photo(base+3, "selected", "confirmed", time.Minute, false)
	photo(base+4, "starred", "none", time.Hour, false)
	photo(base+5, "starred", "none", time.Hour, true)
	photo(base+6, "expired", "none", time.Hour, false)
	photo(base+7, "shallow", "none", time.Hour, false)
	photo(base+8, "plain", "none", time.Hour, false)

	lane := postgres.PhotoLane{Pool: pool}
	mine := func(limit, skip int) []int64 {
		refs, err := lane.Select(ctx, limit, skip, nil)
		if err != nil {
			t.Fatal(err)
		}
		var ids []int64
		for _, ref := range refs {
			if ref.ID >= base && ref.ID < base+100 {
				ids = append(ids, ref.ID)
			}
		}
		return ids
	}
	want := []int64{base + 2, base + 1, base + 4}
	if got := mine(1000, 0); !reflect.DeepEqual(got, want) {
		t.Fatalf("Select() ids = %v, want %v", got, want)
	}
	// Paging is checked against the full ordered list, so other eligible rows in
	// the shared test database cannot change the expectation.
	all, err := lane.Select(ctx, 1000, 0, nil)
	if err != nil || len(all) < 3 {
		t.Fatalf("Select(1000, 0) = %v, %v; want at least the fixture rows", all, err)
	}
	first, err := lane.Select(ctx, 2, 0, nil)
	if err != nil || !reflect.DeepEqual(first, all[:2]) {
		t.Fatalf("Select(2, 0) = %v, %v; want %v", first, err, all[:2])
	}
	second, err := lane.Select(ctx, 2, 2, nil)
	if err != nil || !reflect.DeepEqual(second, all[2:min(4, len(all))]) {
		t.Fatalf("Select(2, 2) = %v, %v; want %v", second, err, all[2:min(4, len(all))])
	}
	withExtras, err := lane.Select(ctx, 1000, 0, []string{"info"})
	if err != nil {
		t.Fatalf("Select() with optional facets: %v", err)
	}
	selectedInfoIndex := -1
	for index, ref := range withExtras {
		if ref.ID == base+3 && ref.Facet == "info" {
			selectedInfoIndex = index
		}
		if ref.ID == base+8 && ref.Facet != "sizes" {
			t.Fatalf("unstarred and unselected owner received optional facet: %+v", ref)
		}
	}
	if selectedInfoIndex < 0 {
		t.Fatalf("selected contact photo missing optional info work: %+v", withExtras)
	}
	for index, ref := range withExtras {
		if ref.Facet == "sizes" && index > selectedInfoIndex {
			t.Fatalf("optional facet at %d preceded size job at %d", selectedInfoIndex, index)
		}
	}

	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests (user_id,person_nsid,depth,via)
		VALUES($1,$2,0,'connected_account')`, userA, people["selected"]); err != nil {
		t.Fatal(err)
	}
	var keyID, accountID int64
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys
		(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst,status)
		VALUES($1,'lane',$2,'{}',3600,4,5,'valid') RETURNING id`, userA, "lane-"+suffix).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts
		(user_id,key_id,nsid,username,token_sealed,secret_sealed,status)
		VALUES($1,$2,$3,'lane','{}','{}','ok') RETURNING id`, userA, keyID, people["selected"]).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM flickr_accounts WHERE id=$1`, accountID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM api_keys WHERE id=$1`, keyID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO photo_account_sizes
		(photo_id,account_id,photo_updated_at,photo_visibility,sizes,sizes_status,sizes_checked_at)
		SELECT id,$2,last_update,visibility,'[]'::jsonb,'confirmed',now()
		FROM photos WHERE id=$1`, base+2, accountID); err != nil {
		t.Fatal(err)
	}
	for cycle := range 3 {
		ids := mine(1000, 0)
		for _, id := range ids {
			if id == base+2 {
				t.Fatalf("photo lane selected account-resolved photo %d on cycle %d", id, cycle+1)
			}
		}
		if len(ids) < 2 {
			t.Fatalf("photo lane stopped progressing on cycle %d: ids=%v", cycle+1, ids)
		}
	}
}

func TestPhotoLaneHandlesLegacyAndAccountScopedSizes(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := dbtest.Open(t, loadCatalogDatabase(t))
	suffix := fmt.Sprintf("lane-sizes-%d", time.Now().UnixNano())
	base := time.Now().UnixNano() % 1_000_000_000 * 100
	owner := suffix + "-owner"
	var ownerUser, otherUser, ownerKey, otherKey, ownerAccount, otherAccount int64
	for _, entry := range []struct {
		email string
		id    *int64
	}{{suffix + "-owner@example.test", &ownerUser}, {suffix + "-other@example.test", &otherUser}} {
		if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration-hash','user') RETURNING id`, entry.email).Scan(entry.id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username)
		SELECT $1 || '-owner-' || n, $1 || '-owner-' || n FROM generate_series(1,3999) AS n`, suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,$1)`, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via,starred,selected_until)
		VALUES($1,$2,0,'test',true,now()+interval '1 hour')`, ownerUser, owner); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst,status)
		VALUES($1,'owner',$2,'{}',3600,4,5,'valid') RETURNING id`, ownerUser, suffix+"-owner-key").Scan(&ownerKey); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status)
		VALUES($1,$2,$3,'owner','{}','{}','ok') RETURNING id`, ownerUser, ownerKey, owner).Scan(&ownerAccount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst,status)
		VALUES($1,'other',$2,'{}',3600,4,5,'valid') RETURNING id`, otherUser, suffix+"-other-key").Scan(&otherKey); err != nil {
		t.Fatal(err)
	}
	// A second XFlickr user can connect the same Flickr identity, but their
	// account-scoped result must not close the photo owner's lane.
	if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status)
		VALUES($1,$2,$3,'other','{}','{}','ok') RETURNING id`, otherUser, otherKey, owner).Scan(&otherAccount); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM entity_facet_restrictions WHERE entity_id = ANY($1)`, []string{fmt.Sprint(base + 1), fmt.Sprint(base + 2), fmt.Sprint(base + 3), fmt.Sprint(base + 4), fmt.Sprint(base + 5)})
		_, _ = pool.Exec(bg, `DELETE FROM photos WHERE id >= $1 AND id <= $2`, base+1, base+5)
		_, _ = pool.Exec(bg, `DELETE FROM people WHERE nsid=$1`, owner)
		_, _ = pool.Exec(bg, `DELETE FROM users WHERE id = ANY($1)`, []int64{ownerUser, otherUser})
	})
	for _, photo := range []struct {
		id     int64
		server string
		status string
	}{
		{base + 1, "1234", "unavailable"},
		{base + 2, "65535", "confirmed"},
		{base + 3, "1234", "confirmed"},
		{base + 4, "1234", "provisional"},
		{base + 5, "1234", "provisional"},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO photos(id,owner_nsid,server,last_update,visibility,sizes_status,sizes_checked_at,uploaded_at,raw,content_hash)
			VALUES($1,$2,$3,now(),'public',$4,now(),now(),'{}',decode(repeat('ab',32),'hex'))`, photo.id, owner, photo.server, photo.status); err != nil {
			t.Fatal(err)
		}
	}
	insertAccountResult := func(photoID, accountID int64, status string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO photo_account_sizes(photo_id,account_id,photo_updated_at,photo_visibility,sizes,sizes_status,sizes_checked_at)
			SELECT id,$2,last_update,visibility,'[]'::jsonb,$3,now() FROM photos WHERE id=$1`, photoID, accountID, status); err != nil {
			t.Fatal(err)
		}
	}
	insertAccountResult(base+1, otherAccount, "unavailable")
	insertAccountResult(base+4, ownerAccount, "unavailable")
	insertAccountResult(base+5, ownerAccount, "confirmed")
	lane := postgres.PhotoLane{Pool: pool}
	selectRefs := func() map[int64][]string {
		t.Helper()
		refs, err := lane.Select(ctx, 1000, 0, []string{"info"})
		if err != nil {
			t.Fatal(err)
		}
		got := make(map[int64][]string)
		for _, ref := range refs {
			if ref.ID >= base+1 && ref.ID <= base+5 {
				got[ref.ID] = append(got[ref.ID], ref.Facet)
			}
		}
		return got
	}
	got := selectRefs()
	for id, want := range map[int64][]string{
		base + 1: {"sizes"},
		base + 2: {"sizes"},
		base + 3: {"info"},
		base + 4: nil,
		base + 5: {"info"},
	} {
		if !reflect.DeepEqual(got[id], want) {
			t.Errorf("photo lane facets for %d = %v, want %v", id, got[id], want)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE flickr_accounts SET status='reconnect_required' WHERE id=$1`, ownerAccount); err != nil {
		t.Fatal(err)
	}
	got = selectRefs()
	for _, id := range []int64{base + 4, base + 5} {
		if !reflect.DeepEqual(got[id], []string{"sizes"}) {
			t.Errorf("photo lane facets for inactive account result %d = %v, want sizes", id, got[id])
		}
	}
}

func TestPhotoLaneLimitsPendingPhotosPerOwner(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := dbtest.Open(t, loadCatalogDatabase(t))
	suffix := fmt.Sprintf("lane-cap-%d", time.Now().UnixNano())
	base := time.Now().UnixNano() % 1_000_000_000 * 100
	owner := suffix + "-owner"
	userEmail := suffix + "@example.test"
	var userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration-hash','user') RETURNING id`, userEmail).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,$1)`, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via,starred,selected_until)
		VALUES($1,$2,0,'test',true,now()+interval '1 hour')`, userID, owner); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM photos WHERE owner_nsid=$1`, owner)
		_, _ = pool.Exec(bg, `DELETE FROM people WHERE nsid=$1`, owner)
		_, _ = pool.Exec(bg, `DELETE FROM users WHERE id=$1`, userID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO photos(id,owner_nsid,server,last_update,visibility,sizes,sizes_status,sizes_checked_at,uploaded_at,raw,content_hash)
		SELECT $1::bigint+n::bigint,$2,'1234',now(),'public','[]'::jsonb,'provisional',NULL,now()-n*interval '1 second','{}',decode(repeat('ab',32),'hex')
		FROM generate_series(1,75) AS n`, base, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO photos(id,owner_nsid,server,last_update,visibility,sizes,sizes_status,sizes_checked_at,uploaded_at,raw,content_hash)
		SELECT $1::bigint+n::bigint,$2,'1234',now(),'public','[]'::jsonb,'confirmed',now(),now()-n*interval '1 second','{}',decode(repeat('ab',32),'hex')
		FROM generate_series(101,175) AS n`, base, owner); err != nil {
		t.Fatal(err)
	}
	refs, err := (postgres.PhotoLane{Pool: pool}).Select(ctx, 150, 0, []string{"info"})
	if err != nil {
		t.Fatal(err)
	}
	var found int
	for _, ref := range refs {
		if ref.OwnerNSID == owner {
			found++
			if ref.Facet != "sizes" {
				t.Fatalf("per-owner photo cap admitted optional work before pending sizes: %+v", ref)
			}
		}
	}
	if found != 50 {
		t.Fatalf("selected %d photos for one owner, want the per-owner cap of 50", found)
	}
}

func TestPhotoLaneContinuesPastResolvedPhotoBatches(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := dbtest.Open(t, loadCatalogDatabase(t))
	suffix := fmt.Sprintf("lane-cursor-%d", time.Now().UnixNano())
	base := time.Now().UnixNano() % 1_000_000_000 * 100
	owners := []string{suffix + "-sizes", suffix + "-facets"}
	userEmail := suffix + "@example.test"
	var userID, keyID, accountID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration-hash','user') RETURNING id`, userEmail).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys
		(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst,status)
		VALUES($1,'lane-cursor',$2,'{}',3600,4,5,'valid') RETURNING id`, userID, "lane-"+suffix).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts
		(user_id,key_id,nsid,username,token_sealed,secret_sealed,status)
		VALUES($1,$2,$3,'lane','{}','{}','ok') RETURNING id`, userID, keyID, owners[0]).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	for _, owner := range owners {
		if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,$1)`, owner); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via,starred,selected_until)
			VALUES($1,$2,0,'test',true,now()+interval '1 hour')`, userID, owner); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM schedule_meta WHERE key = ANY($1)`, []string{
			"photo_lane_cursor:" + owners[0], "photo_lane_cursor:" + owners[1],
		})
		_, _ = pool.Exec(bg, `DELETE FROM photos WHERE owner_nsid = ANY($1)`, owners)
		_, _ = pool.Exec(bg, `DELETE FROM people WHERE nsid = ANY($1)`, owners)
		_, _ = pool.Exec(bg, `DELETE FROM flickr_accounts WHERE id=$1`, accountID)
		_, _ = pool.Exec(bg, `DELETE FROM api_keys WHERE id=$1`, keyID)
		_, _ = pool.Exec(bg, `DELETE FROM users WHERE id=$1`, userID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO photos
		(id,owner_nsid,server,last_update,visibility,sizes,sizes_status,sizes_checked_at,uploaded_at,raw,content_hash)
		SELECT $1::bigint+n::bigint,$2,'1234',now(),'public','[]'::jsonb,'provisional',NULL,
			now()-n*interval '1 second','{}',decode(repeat('ab',32),'hex')
		FROM generate_series(1,75) AS n`, base, owners[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO photo_account_sizes
		(photo_id,account_id,photo_updated_at,photo_visibility,sizes,sizes_status,sizes_checked_at)
		SELECT id,$2,last_update,visibility,'[]'::jsonb,'confirmed',last_update
		FROM photos WHERE owner_nsid=$1 AND id<>$3`, owners[0], accountID, base+56); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO photos
		(id,owner_nsid,server,last_update,visibility,sizes,sizes_status,sizes_checked_at,uploaded_at,raw,content_hash)
		SELECT $1::bigint+n::bigint,$2,'1234',now(),'public','[]'::jsonb,'confirmed',now(),
			now()-n*interval '1 second','{}',decode(repeat('cd',32),'hex')
		FROM generate_series(101,175) AS n`, base, owners[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO photo_facet_status(photo_id,facet,photo_updated_at)
		SELECT id,'info',last_update FROM photos WHERE owner_nsid=$1 AND id<>$2`, owners[1], base+156); err != nil {
		t.Fatal(err)
	}
	lane := postgres.PhotoLane{Pool: pool}
	selectOwners := func() map[string][]catalogapp.PhotoRef {
		t.Helper()
		refs, err := lane.Select(ctx, 1000, 0, []string{"info"})
		if err != nil {
			t.Fatal(err)
		}
		found := make(map[string][]catalogapp.PhotoRef, len(owners))
		for _, ref := range refs {
			if ref.OwnerNSID == owners[0] || ref.OwnerNSID == owners[1] {
				found[ref.OwnerNSID] = append(found[ref.OwnerNSID], ref)
			}
		}
		return found
	}
	if first := selectOwners(); len(first[owners[0]]) != 0 || len(first[owners[1]]) != 0 {
		t.Fatalf("first bounded photo batch = %v; want both owners to advance past 50 resolved photos", first)
	}
	second := selectOwners()
	want := map[string]string{owners[0]: "sizes", owners[1]: "info"}
	for index, owner := range owners {
		photoID := base + 56
		if index == 1 {
			photoID = base + 156
		}
		found := false
		for _, ref := range second[owner] {
			if ref.ID == photoID && ref.Facet == want[owner] {
				found = true
			}
		}
		if !found {
			t.Errorf("second photo batch for %s = %v; want photo %d facet %s", owner, second[owner], photoID, want[owner])
		}
	}
}

func TestPhotoLanePlanBoundsPhotoReads(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool := dbtest.Open(t, loadCatalogDatabase(t))
	suffix := fmt.Sprintf("lane-plan-%d", time.Now().UnixNano())
	base := time.Now().UnixNano() % 1_000_000_000 * 1000
	owner := suffix + "-owner"
	userEmail := suffix + "@example.test"
	var userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration-hash','user') RETURNING id`, userEmail).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username)
		SELECT $1 || '-owner-' || n, $1 || '-owner-' || n FROM generate_series(1,3999) AS n`, suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,$1)`, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via,starred,selected_until)
		VALUES($1,$2,0,'test',true,now()+interval '1 hour')`, userID, owner); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM photos WHERE owner_nsid LIKE $1 || '-owner%'`, suffix)
		_, _ = pool.Exec(bg, `DELETE FROM people WHERE nsid LIKE $1 || '-owner%'`, suffix)
		_, _ = pool.Exec(bg, `DELETE FROM users WHERE id=$1`, userID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO photos(id,owner_nsid,last_update,visibility,sizes,sizes_status,uploaded_at,raw,content_hash)
		SELECT $1::bigint+n::bigint,
			CASE WHEN n <= 1000 THEN $2 ELSE $3 || '-owner-' || ((n % 3999)+1)::text END,
			now(),'public','[]'::jsonb,'provisional',now()-n*interval '1 second','{}',decode(repeat('ab',32),'hex')
		FROM generate_series(1,400000) AS n`, base, owner, suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE photos`); err != nil {
		t.Fatal(err)
	}
	queryPool := &photoLanePlanPool{pool: pool}
	if _, err := (postgres.PhotoLane{Pool: queryPool}).Select(ctx, 50, 0, nil); err != nil {
		t.Fatal(err)
	}
	var encoded []byte
	if err := pool.QueryRow(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) `+queryPool.query, queryPool.args...).Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	var result []map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil || len(result) != 1 {
		t.Fatalf("decode photo lane plan: %v; plan=%s", err, encoded)
	}
	root, ok := result[0]["Plan"].(map[string]any)
	if !ok {
		t.Fatalf("photo lane plan has no root node: %s", encoded)
	}
	if relation := photoSeqScan(root); relation != "" {
		t.Fatalf("photo lane plan uses a sequential scan on %s: %s", relation, photoScanNodes(root))
	}
	buffers := planBuffers(root)
	if buffers > 2000 {
		t.Fatalf("photo lane plan touched %d buffers, want at most 2000: %s", buffers, photoScanNodes(root))
	}
	t.Logf("EXPLAIN (ANALYZE, BUFFERS): fixture=400000 photos, execution_ms=%.3f, shared_buffers=%d, photo_access=%s",
		result[0]["Execution Time"], buffers, photoIndexNodes(root))
}

type photoLanePlanPool struct {
	pool interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	}
	query string
	args  []any
}

func (p *photoLanePlanPool) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	p.query = query
	p.args = append([]any(nil), args...)
	return p.pool.Query(ctx, query, args...)
}

func photoSeqScan(node map[string]any) string {
	if node["Node Type"] == "Seq Scan" || node["Node Type"] == "Parallel Seq Scan" {
		if relation, ok := node["Relation Name"].(string); ok && strings.HasPrefix(relation, "photos") {
			return relation
		}
	}
	children, _ := node["Plans"].([]any)
	for _, child := range children {
		if plan, ok := child.(map[string]any); ok {
			if relation := photoSeqScan(plan); relation != "" {
				return relation
			}
		}
	}
	return ""
}

func planBuffers(node map[string]any) int {
	var total int
	for _, key := range []string{"Shared Hit Blocks", "Shared Read Blocks", "Shared Dirtied Blocks", "Shared Written Blocks"} {
		if count, ok := node[key].(float64); ok {
			total += int(count)
		}
	}
	return total
}

func photoIndexNodes(node map[string]any) string {
	var found []string
	if name, ok := node["Index Name"].(string); ok && strings.Contains(name, "photos_") {
		found = append(found, name)
	}
	children, _ := node["Plans"].([]any)
	for _, child := range children {
		if plan, ok := child.(map[string]any); ok {
			if names := photoIndexNodes(plan); names != "" {
				found = append(found, strings.Split(names, ",")...)
			}
		}
	}
	sort.Strings(found)
	unique := found[:0]
	for _, name := range found {
		if len(unique) == 0 || unique[len(unique)-1] != name {
			unique = append(unique, name)
		}
	}
	return strings.Join(unique, ",")
}

func photoScanNodes(node map[string]any) string {
	var found []string
	if relation, ok := node["Relation Name"].(string); ok && strings.HasPrefix(relation, "photos") {
		found = append(found, fmt.Sprintf("%s %s index=%v rows=%v loops=%v filter=%v",
			node["Node Type"], relation, node["Index Name"], node["Actual Rows"], node["Actual Loops"], node["Filter"]))
	}
	children, _ := node["Plans"].([]any)
	for _, child := range children {
		if plan, ok := child.(map[string]any); ok {
			found = append(found, photoScanNodes(plan))
		}
	}
	return strings.Join(found, "; ")
}
