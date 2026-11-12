package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	crawlapp "github.com/jooservices/xflickr/internal/crawl/app"
	"github.com/jooservices/xflickr/internal/crawl/domain"
	"github.com/jooservices/xflickr/internal/crawl/facets"
)

// The SQL in this file is hand-written on purpose: it seeds and asserts fixtures on the isolated worktree database, and sqlc
// generates production queries only.
type publicGroupsRefreshCase struct {
	name, body            string
	membership, failed    bool
	existing              bool
	wantDescription       string
	wantMembers, wantPool int64
}

func TestPublicGroupsRefreshPersistsMembership(t *testing.T) {
	for _, tc := range []publicGroupsRefreshCase{
		{name: "populated", body: `{"groups":{"pages":1,"group":[{"nsid":"%s","name":"Public group"}]}}`, membership: true},
		{name: "existing metadata omitted", body: `{"groups":{"pages":1,"group":[{"nsid":"%s","name":"Public group"}]}}`, membership: true, existing: true, wantDescription: "Saved description", wantMembers: 42, wantPool: 99},
		{name: "explicit empty metadata", body: `{"groups":{"pages":1,"group":[{"nsid":"%s","name":"Public group","description":"","members":"0","pool_count":0}]}}`, membership: true, existing: true},
		{name: "partial metadata", body: `{"groups":{"pages":1,"group":[{"nsid":"%s","name":"Public group","members":"7"}]}}`, membership: true, existing: true, wantDescription: "Saved description", wantMembers: 7, wantPool: 99},
		{name: "empty", body: `{"groups":{"pages":1,"group":[]}}`},
		{name: "malformed", body: `{`, failed: true},
		{name: "invalid member rolls back whole page", body: `{"groups":{"group":[{"nsid":"%s","name":"Public group"},{"nsid":"invalid"}]}}`, failed: true},
		{name: "transport error", failed: true},
	} {
		t.Run(tc.name, func(t *testing.T) { runPublicGroupsRefresh(t, tc) })
	}
}

func runPublicGroupsRefresh(t *testing.T, tc publicGroupsRefreshCase) {
	t.Helper()
	pool := openEngineTestPool(t)
	user := insertInterestTestUser(t, t.Context(), pool)
	person := fmt.Sprintf("public-refresh-%d@N01", user)
	group := person + "-group"
	check := seedPublicGroupsCheck(t, pool, user, person)
	if tc.existing {
		if _, err := pool.Exec(t.Context(), `INSERT INTO groups(nsid,name,description,members_count,pool_count) VALUES($1,'Old name','Saved description',42,99)`, group); err != nil {
			t.Fatal(err)
		}
	}
	fake := &recordingFlickr{payloads: map[string]json.RawMessage{"flickr.favorites.getList": json.RawMessage(`{"photos":{"total":0}}`)}}
	if tc.body != "" {
		fake.payloads["flickr.people.getPublicGroups"] = json.RawMessage(strings.ReplaceAll(tc.body, "%s", group))
	}
	engine := newPublicGroupsEngine(t, pool, fake)
	if err := engine.Handle(t.Context(), check); err != nil {
		t.Fatal(err)
	}
	var item int64
	if err := pool.QueryRow(t.Context(), `UPDATE work_items SET state='ready' WHERE user_id=$1 AND kind='groups' AND subject_key=$2 RETURNING id`, user, person).Scan(&item); err != nil {
		t.Fatal(err)
	}
	if err := engine.Handle(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	assertPublicGroupsRequest(t, fake, person)
	assertPublicGroupsRows(t, pool, person, tc.membership)
	assertPublicGroupsCompletion(t, pool, item, tc.failed)
	if tc.membership {
		assertPublicGroupMetadata(t, pool, group, tc)
	}
}

func newPublicGroupsEngine(t *testing.T, pool *pgxpool.Pool, fake crawlapp.Flickr) *crawlapp.Engine {
	t.Helper()
	engine := newCheckpointTestEngine(t, pool, fake, time.Now().UTC())
	for _, facet := range []domain.Facet{facets.Albums{}, facets.Galleries{}, facets.Groups{}} {
		if err := engine.Registry.Register(facet); err != nil {
			t.Fatal(err)
		}
	}
	return engine
}

func seedPublicGroupsCheck(t *testing.T, pool *pgxpool.Pool, user int64, person string) int64 {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		for _, cleanup := range []struct {
			query string
			arg   any
		}{
			{`DELETE FROM work_items WHERE user_id=$1`, user},
			{`DELETE FROM person_groups WHERE person_nsid=$1`, person},
			{`DELETE FROM groups WHERE nsid=$1`, person + "-group"},
			{`DELETE FROM people WHERE nsid=$1`, person},
		} {
			if _, err := pool.Exec(ctx, cleanup.query, cleanup.arg); err != nil {
				t.Errorf("cleanup public refresh: %v", err)
			}
		}
	})
	if _, err := pool.Exec(t.Context(), `INSERT INTO people(nsid,username) VALUES($1,'Contact')`, person); err != nil {
		t.Fatal(err)
	}
	cursor, err := json.Marshal(domain.Cursor{Page: 1, Method: "flickr.favorites.getList", Params: map[string]string{"user_id": person, "per_page": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := pool.QueryRow(t.Context(), `INSERT INTO work_items(kind,subject_key,user_id,state,cursor) VALUES('check',$1,$2,'ready',$3) RETURNING id`, person, user, cursor).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func assertPublicGroupsRequest(t *testing.T, fake *recordingFlickr, person string) {
	t.Helper()
	if len(fake.calls) != 2 {
		t.Fatalf("calls = %+v, want favorites probe and group refresh only", fake.calls)
	}
	request := fake.calls[1]
	if request.Method != "flickr.people.getPublicGroups" || request.Params["user_id"] != person || request.Params["per_page"] != "500" || request.Params["page"] != "1" {
		t.Fatalf("public groups request = %+v", request)
	}
}

func assertPublicGroupsRows(t *testing.T, pool *pgxpool.Pool, person string, want bool) {
	t.Helper()
	var membership, group bool
	err := pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM person_groups WHERE person_nsid=$1), EXISTS(SELECT 1 FROM groups WHERE nsid=$1 || '-group' AND name='Public group')`, person).Scan(&membership, &group)
	if err != nil {
		t.Fatal(err)
	}
	if membership != want || group != want {
		t.Fatalf("membership=%t group=%t, want %t", membership, group, want)
	}
	if want {
		var linked bool
		if err := pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM person_groups WHERE person_nsid=$1 AND group_nsid=$1 || '-group')`, person).Scan(&linked); err != nil {
			t.Fatal(err)
		}
		if !linked {
			t.Fatal("group not linked to the refreshed contact")
		}
	}
}

func assertPublicGroupsCompletion(t *testing.T, pool *pgxpool.Pool, item int64, failed bool) {
	t.Helper()
	var state string
	var page, attempts int
	var lastError bool
	if err := pool.QueryRow(t.Context(), `SELECT state, (cursor->>'page')::int, attempts, last_error IS NOT NULL FROM work_items WHERE id=$1`, item).Scan(&state, &page, &attempts, &lastError); err != nil {
		t.Fatal(err)
	}
	if failed {
		if state != domain.StatePending || page != 1 || attempts != 1 || !lastError {
			t.Fatalf("failed refresh: state=%s page=%d attempts=%d error=%t", state, page, attempts, lastError)
		}
	} else if state != domain.StateDone || attempts != 0 || lastError {
		t.Fatalf("completed refresh: state=%s attempts=%d error=%t", state, attempts, lastError)
	}
}

func assertPublicGroupMetadata(t *testing.T, pool *pgxpool.Pool, group string, tc publicGroupsRefreshCase) {
	t.Helper()
	var description string
	var members, photos int64
	if err := pool.QueryRow(t.Context(), `SELECT description,members_count,pool_count FROM groups WHERE nsid=$1`, group).Scan(&description, &members, &photos); err != nil {
		t.Fatal(err)
	}
	if description != tc.wantDescription || members != tc.wantMembers || photos != tc.wantPool {
		t.Fatalf("metadata = (%q,%d,%d), want (%q,%d,%d)", description, members, photos, tc.wantDescription, tc.wantMembers, tc.wantPool)
	}
}
