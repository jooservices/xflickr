package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	catalogpg "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
	crawlapp "github.com/jooservices/xflickr/internal/crawl/app"
	"github.com/jooservices/xflickr/internal/crawl/domain"
	"github.com/jooservices/xflickr/internal/crawl/facets"
)

type groupPoolFlickr struct {
	requests []crawlapp.Request
}

func (f *groupPoolFlickr) Call(_ context.Context, request crawlapp.Request) (json.RawMessage, error) {
	f.requests = append(f.requests, request)
	return json.RawMessage(`{"photos":{"pages":1,"photo":[{"id":"710000000000007001","owner":"group-owner@N01","ownername":"Group Owner","lastupdate":"1700000100","dateupload":"1700000000","ispublic":1,"url_t":"https://example.invalid/photo.jpg"}]}}`), nil
}

func TestGroupPoolPreferenceRunsAndStopsScheduledCrawl(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	person := "pool-contact-" + suffix + "@N01"
	groupNSID := "pool-group-" + suffix
	owner := "pool-owner-" + suffix + "@N01"
	photoID := int64(710000000000007001)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM group_pool_photos WHERE group_nsid=$1`, groupNSID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id=$1`, photoID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM person_groups WHERE group_nsid=$1`, groupNSID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM groups WHERE nsid=$1`, groupNSID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=ANY($1)`, []string{person, owner})
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2`, userID, person)
	})
	repository, err := catalogpg.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.UpsertPeople(ctx, []catalog.Person{{NSID: person, Username: person}, {NSID: owner, Username: owner}}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via) VALUES($1,$2,0,'integration')`, userID, person); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO groups(nsid,name) VALUES($1,'Pool group')`, groupNSID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO person_groups(person_nsid,group_nsid) VALUES($1,$2)`, person, groupNSID); err != nil {
		t.Fatal(err)
	}
	preference, found, err := repository.SetContactGroupPoolCrawl(ctx, userID, person, groupNSID, true)
	if err != nil || !found || !preference.Enabled {
		t.Fatalf("enable group pool preference=%+v found=%t err=%v", preference, found, err)
	}
	var itemID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM work_items WHERE user_id=$1 AND kind='group_pool' AND subject_key=$2`, userID, groupNSID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	store, err := NewEngine(pool, func(conn Conn) crawlapp.CatalogWriter {
		repo, repoErr := catalogpg.New(conn)
		if repoErr != nil {
			t.Fatal(repoErr)
		}
		return repo
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := crawlapp.NewRegistry()
	if err := registry.Register(facets.GroupPool{}); err != nil {
		t.Fatal(err)
	}
	flickr := &groupPoolFlickr{}
	engine := &crawlapp.Engine{Registry: registry, Store: store, Units: store, GroupPools: store, Keys: intKeys{}, Governor: intGovernor{}, Flickr: flickr, Settings: intSettings{}}
	if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready' WHERE id=$1`, itemID); err != nil {
		t.Fatal(err)
	}
	if err := engine.Handle(ctx, itemID); err != nil {
		t.Fatal(err)
	}
	if len(flickr.requests) != 1 || flickr.requests[0].Method != "flickr.groups.pools.getPhotos" || flickr.requests[0].Params["group_id"] != groupNSID || flickr.requests[0].Params["user_id"] != person {
		t.Fatalf("enabled preference requests=%+v", flickr.requests)
	}
	if got := count(t, ctx, pool, `SELECT count(*) FROM group_pool_photos WHERE group_nsid=$1 AND photo_id=$2`, groupNSID, photoID); got != 1 {
		t.Fatalf("stored group pool photos=%d, want one", got)
	}
	if _, found, err := repository.SetContactGroupPoolCrawl(ctx, userID, person, groupNSID, false); err != nil || !found {
		t.Fatalf("disable group pool preference found=%t err=%v", found, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready' WHERE id=$1`, itemID); err != nil {
		t.Fatal(err)
	}
	if err := engine.Handle(ctx, itemID); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM work_items WHERE id=$1`, itemID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != domain.StateDone || len(flickr.requests) != 1 {
		t.Fatalf("disabled preference state=%q Flickr calls=%d, want done and no extra call", state, len(flickr.requests))
	}
}
