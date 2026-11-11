package postgres_test

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
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
