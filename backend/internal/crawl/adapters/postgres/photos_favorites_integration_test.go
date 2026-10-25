package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	catalogpg "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	crawlapp "github.com/jooservices/xflickr/internal/crawl/app"
	"github.com/jooservices/xflickr/internal/crawl/domain"
	"github.com/jooservices/xflickr/internal/crawl/facets"
	settingsdomain "github.com/jooservices/xflickr/internal/settings/domain"
)

type photosFavoritesFlickr struct {
	pages map[string]map[int]json.RawMessage
}

type catalogFacetsFlickr map[string]json.RawMessage

func (f catalogFacetsFlickr) Call(_ context.Context, request crawlapp.Request) (json.RawMessage, error) {
	if payload, ok := f[request.Method]; ok {
		return payload, nil
	}
	return nil, fmt.Errorf("unexpected catalog facet method %s", request.Method)
}

func (f photosFavoritesFlickr) Call(_ context.Context, request crawlapp.Request) (json.RawMessage, error) {
	page := 1
	fmt.Sscan(request.Params["page"], &page)
	key := request.Method + ":" + request.Params["user_id"]
	if payload, ok := f.pages[key][page]; ok {
		return payload, nil
	}
	return nil, fmt.Errorf("unexpected Flickr request %s page %d", key, page)
}

type checkIncrementalFlickr struct {
	subject string
	owner   string
	calls   []crawlapp.Request
	nextID  int64
}

type contactsPhotosFlagFlickr struct{}

func (contactsPhotosFlagFlickr) Call(_ context.Context, request crawlapp.Request) (json.RawMessage, error) {
	if request.Method != "flickr.photos.getContactsPhotos" {
		return nil, fmt.Errorf("unexpected Flickr request %s", request.Method)
	}
	return json.RawMessage(`{"photos":{"photo":[{"owner":"flagged-person"}]}}`), nil
}

func (f *checkIncrementalFlickr) Call(_ context.Context, request crawlapp.Request) (json.RawMessage, error) {
	f.calls = append(f.calls, request)
	if request.Params["per_page"] == "1" {
		if request.Method == "flickr.people.getPhotos" {
			return json.RawMessage(`{"photos":{"total":"1","pages":1,"photo":[]}}`), nil
		}
		if request.Method == "flickr.favorites.getList" {
			return json.RawMessage(`{"photos":{"total":"0","pages":0,"photo":[]}}`), nil
		}
	}
	if (request.Method == "flickr.people.getPhotos" || request.Method == "flickr.favorites.getList") && request.Params["page"] == "1" {
		id := f.nextID
		if id == 0 {
			id = 710000000000002000
		}
		f.nextID++
		payload := photoFavoritesPayload(1, 1, 1, id, f.owner, "New Photo Owner")
		if request.Params["extras"] == "" {
			payload = strings.ReplaceAll(payload, `,"lastupdate":"1700000100","dateupload":"1700000000"`, "")
		}
		return json.RawMessage(payload), nil
	}
	return nil, fmt.Errorf("unexpected Flickr request %s %#v", request.Method, request.Params)
}

func TestIncrementalCheckReopensCompletedPhotoWorkOnLaterCycle(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	subject := "check-repeat-" + suffix
	owner := "check-repeat-owner-" + suffix + "@N01"
	firstID, secondID := int64(710000000000002100), int64(710000000000002101)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id = ANY($1)`, []int64{firstID, secondID})
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, owner)
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2`, userID, subject)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via) VALUES($1,$2,0,'test')`, userID, subject); err != nil {
		t.Fatal(err)
	}
	checkpoint1 := time.Now().UTC().Truncate(time.Second)
	checkCursor := func(checkpoint time.Time) []byte {
		raw, marshalErr := json.Marshal(domain.Cursor{Page: 1, Method: "flickr.people.getPhotos", CheckpointAt: checkpoint.Unix(), Params: map[string]string{
			"user_id": subject, "min_upload_date": "1700000000", "min_fave_date": "1700000100", "per_page": "1",
		}})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return raw
	}
	var checkItemID int64
	if err := pool.QueryRow(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state,depth,cursor) VALUES('check',$1,$2,'ready',0,$3) RETURNING id`, subject, userID, checkCursor(checkpoint1)).Scan(&checkItemID); err != nil {
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
	for _, facet := range []domain.Facet{crawlapp.ChangeCheck{}, crawlapp.ContactsPhotos{}, facets.Photos{}, facets.Favorites{}, facets.Albums{}, facets.Galleries{}, facets.Groups{}} {
		if err := registry.Register(facet); err != nil {
			t.Fatal(err)
		}
	}
	flickrFake := &checkIncrementalFlickr{subject: subject, owner: owner, nextID: firstID}
	engine := &crawlapp.Engine{Registry: registry, Store: store, Units: store, Keys: intKeys{}, Governor: intGovernor{}, Flickr: flickrFake, Settings: photosFavoritesSettings{maxDepth: 1}}
	runCycle := func(checkpoint time.Time) {
		if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready', cursor=$2 WHERE id=$1`, checkItemID, checkCursor(checkpoint)); err != nil {
			t.Fatal(err)
		}
		if err := engine.Handle(ctx, checkItemID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready' WHERE id=$1`, checkItemID); err != nil {
			t.Fatal(err)
		}
		if err := engine.Handle(ctx, checkItemID); err != nil {
			t.Fatal(err)
		}
		var photoItemID int64
		if err := pool.QueryRow(ctx, `SELECT id FROM work_items WHERE user_id=$1 AND kind='photos' AND subject_key=$2`, userID, subject).Scan(&photoItemID); err != nil {
			t.Fatal(err)
		}
		var state string
		if err := pool.QueryRow(ctx, `SELECT state FROM work_items WHERE id=$1`, photoItemID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state != "pending" {
			t.Fatalf("incremental photo item state after check = %q, want pending", state)
		}
		if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready' WHERE id=$1`, photoItemID); err != nil {
			t.Fatal(err)
		}
		if err := engine.Handle(ctx, photoItemID); err != nil {
			t.Fatal(err)
		}
	}
	runCycle(checkpoint1)
	checkpoint2 := checkpoint1.Add(time.Hour)
	runCycle(checkpoint2)
	if calls := len(flickrFake.calls); calls != 6 {
		t.Fatalf("Flickr calls = %d, want two probes and one crawl page per cycle", calls)
	}
	if got := count(t, ctx, pool, `SELECT count(*) FROM photos WHERE id = ANY($1)`, []int64{firstID, secondID}); got != 2 {
		t.Fatalf("crawled photos across checks = %d, want 2", got)
	}
	var lastSeen time.Time
	if err := pool.QueryRow(ctx, `SELECT last_seen_at FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2`, userID, subject).Scan(&lastSeen); err != nil {
		t.Fatal(err)
	}
	if !lastSeen.Equal(checkpoint2) {
		t.Fatalf("last seen checkpoint after second cycle = %s, want %s", lastSeen, checkpoint2)
	}
	var cursor domain.Cursor
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT cursor FROM work_items WHERE user_id=$1 AND kind='photos' AND subject_key=$2`, userID, subject).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &cursor); err != nil {
		t.Fatal(err)
	}
	if cursor.CheckpointAt != checkpoint2.Unix() || cursor.Params["min_upload_date"] != "1700000000" {
		t.Fatalf("reopened incremental cursor = %#v", cursor)
	}
}

type photosFavoritesSettings struct{ maxDepth int }

func (s photosFavoritesSettings) Effective(context.Context, int64) (settingsdomain.Effective, error) {
	return settingsdomain.Effective{MaxDepth: s.maxDepth}, nil
}

func TestPhotosAndFavoritesFacetsCommitPagesAndGateOwnerDiscovery(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	owner0 := "facet-owner-" + suffix + "-0@N01"
	owner1 := "facet-owner-" + suffix + "-1@N01"
	if _, err := pool.Exec(ctx, `INSERT INTO people (nsid,username,realname,profile,icon,photos_count,last_seen_at,facet_state) VALUES ($1,'known','Known Person','{"bio":"keep"}','known-icon',42,now()-interval '1 day','{"contacts":"done"}'::jsonb)`, owner0); err != nil {
		t.Fatal(err)
	}
	photoSubject := "facet-photos-" + suffix
	favoritesSubject := "facet-favorites-" + suffix
	maxDepthFavoritesSubject := "facet-favorites-max-" + suffix
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,$1),($2,$2)`, favoritesSubject, maxDepthFavoritesSubject); err != nil {
		t.Fatal(err)
	}
	photoItem := insertPhotosFavoritesWork(t, ctx, pool, userID, "photos", photoSubject, 3, facets.Photos{})
	favoritesItem := insertPhotosFavoritesWork(t, ctx, pool, userID, "favorites", favoritesSubject, 2, facets.Favorites{})
	maxFavoritesItem := insertPhotosFavoritesWork(t, ctx, pool, userID, "favorites", maxDepthFavoritesSubject, 3, facets.Favorites{})
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id BETWEEN $1 AND $2`, int64(710000000000000000), int64(710000000000001999))
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid = ANY($1)`, []string{owner0, owner1, favoritesSubject, maxDepthFavoritesSubject})
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id = $1`, userID)
	})

	pages := map[string]map[int]json.RawMessage{
		"flickr.people.getPhotos:" + photoSubject: {
			1: json.RawMessage(photoFavoritesPayload(2, 1, 500, 710000000000000000, owner0, owner1)),
			2: json.RawMessage(photoFavoritesPayload(2, 2, 500, 710000000000000500, owner0, owner1)),
		},
		"flickr.favorites.getList:" + favoritesSubject: {
			1: json.RawMessage(photoFavoritesPayload(1, 1, 500, 710000000000001000, owner0, owner1)),
		},
		"flickr.favorites.getList:" + maxDepthFavoritesSubject: {
			1: json.RawMessage(photoFavoritesPayload(1, 1, 500, 710000000000001500, owner0, owner1)),
		},
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
	for _, facet := range []domain.Facet{facets.Photos{}, facets.Favorites{}} {
		if err := registry.Register(facet); err != nil {
			t.Fatal(err)
		}
	}
	engine := &crawlapp.Engine{
		Registry: registry, Store: store, Units: store, Keys: intKeys{}, Governor: intGovernor{},
		Flickr: photosFavoritesFlickr{pages: pages}, Settings: photosFavoritesSettings{maxDepth: 3},
	}

	for _, pageItem := range []struct {
		id    int64
		pages int
	}{{photoItem, 2}, {favoritesItem, 1}, {maxFavoritesItem, 1}} {
		for page := 1; page <= pageItem.pages; page++ {
			if page > 1 {
				if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready' WHERE id=$1`, pageItem.id); err != nil {
					t.Fatal(err)
				}
			}
			if err := engine.Handle(ctx, pageItem.id); err != nil {
				t.Fatalf("handle item %d page %d: %v", pageItem.id, page, err)
			}
		}
	}

	if got := count(t, ctx, pool, `SELECT count(*) FROM photos WHERE id BETWEEN $1 AND $2 AND sizes_status='provisional'`, int64(710000000000000000), int64(710000000000000999)); got != 1000 {
		t.Fatalf("photos facet stored %d provisional photos, want 1000", got)
	}
	if got := count(t, ctx, pool, `SELECT count(*) FROM photos WHERE id BETWEEN $1 AND $2 AND sizes_status='provisional'`, int64(710000000000001000), int64(710000000000001999)); got != 1000 {
		t.Fatalf("favorites facets stored %d provisional photos, want 1000", got)
	}
	if got := count(t, ctx, pool, `SELECT count(*) FROM favorites WHERE (person_nsid=$1 AND photo_id BETWEEN $3 AND $4) OR (person_nsid=$2 AND photo_id BETWEEN $5 AND $6)`, favoritesSubject, maxDepthFavoritesSubject, int64(710000000000001000), int64(710000000000001499), int64(710000000000001500), int64(710000000000001999)); got != 1000 {
		t.Fatalf("favorites facet persisted %d favorite records, want 1000", got)
	}
	if got := count(t, ctx, pool, `SELECT count(*) FROM crawl_interests WHERE user_id=$1 AND person_nsid LIKE $2 AND depth=3 AND via='favorite'`, userID, "facet-owner-"+suffix+"-%"); got != 2 {
		t.Fatalf("favorites at max_depth-1 recorded %d depth-3 interests, want 2", got)
	}
	if got := count(t, ctx, pool, `SELECT count(*) FROM work_items WHERE user_id=$1 AND subject_key LIKE $2 AND depth=3 AND kind IN ('photos','favorites')`, userID, "facet-owner-"+suffix+"-%"); got != 4 {
		t.Fatalf("favorites at max_depth-1 enqueued %d child facets, want 4", got)
	}
	if got := count(t, ctx, pool, `SELECT count(*) FROM crawl_interests WHERE user_id=$1 AND person_nsid LIKE $2 AND depth=4`, userID, "facet-owner-"+suffix+"-%"); got != 0 {
		t.Fatalf("favorites at max_depth recorded %d over-depth interests, want 0", got)
	}
	var username, realname, icon string
	var profile, state []byte
	var photosCount int64
	if err := pool.QueryRow(ctx, `SELECT username,realname,profile,icon,photos_count,facet_state FROM people WHERE nsid=$1`, owner0).Scan(&username, &realname, &profile, &icon, &photosCount, &state); err != nil {
		t.Fatal(err)
	}
	if username != "Known Person" || realname != "Known Person" || string(profile) != `{"bio": "keep"}` || icon != "known-icon" || photosCount != 42 || !strings.Contains(string(state), "contacts") {
		t.Fatalf("sparse owner upsert clobbered catalog fields: username=%q realname=%q profile=%s icon=%q photos=%d state=%s", username, realname, profile, icon, photosCount, state)
	}
}

func TestCatalogFacetsPersistReturnedRecordsAndEmptyResponses(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("catalog-facets-%d", time.Now().UnixNano())
	person := suffix + "@N01"
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM person_groups WHERE person_nsid=$1`, person)
		_, _ = pool.Exec(context.Background(), `DELETE FROM groups WHERE nsid=$1`, suffix+"-group")
		_, _ = pool.Exec(context.Background(), `DELETE FROM galleries WHERE id=$1`, suffix+"-gallery")
		_, _ = pool.Exec(context.Background(), `DELETE FROM albums WHERE id=$1`, suffix+"-album")
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, person)
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1`, userID)
	})
	facetsToRun := []struct {
		facet   domain.Facet
		payload string
	}{{facets.Albums{}, fmt.Sprintf(`{"photosets":{"pages":1,"photoset":[{"id":%q,"primary":"999999999999","photos":2,"title":{"_content":"Album"}}]}}`, suffix+"-album")}, {facets.Galleries{}, fmt.Sprintf(`{"galleries":{"pages":1,"gallery":[{"id":%q,"primary_photo_id":"999999999999","count_photos":1,"title":{"_content":"Gallery"}}]}}`, suffix+"-gallery")}, {facets.Groups{}, fmt.Sprintf(`{"groups":{"pages":1,"group":[{"nsid":%q,"name":"Group","members":3,"pool_count":4}]}}`, suffix+"-group")}}
	registry := crawlapp.NewRegistry()
	ids := make([]int64, 0, len(facetsToRun))
	responses := catalogFacetsFlickr{}
	for _, item := range facetsToRun {
		if err := registry.Register(item.facet); err != nil {
			t.Fatal(err)
		}
		spec := item.facet.Plan(ctx, domain.Person{NSID: person})[0]
		responses[spec.Method] = json.RawMessage(item.payload)
		ids = append(ids, insertPhotosFavoritesWork(t, ctx, pool, userID, item.facet.Name(), person, 0, item.facet))
	}
	store, err := NewEngine(pool, func(conn Conn) crawlapp.CatalogWriter {
		repo, e := catalogpg.New(conn)
		if e != nil {
			t.Fatal(e)
		}
		return repo
	})
	if err != nil {
		t.Fatal(err)
	}
	engine := &crawlapp.Engine{Registry: registry, Store: store, Units: store, Keys: intKeys{}, Governor: intGovernor{}, Flickr: responses, Settings: photosFavoritesSettings{maxDepth: 1}}
	for _, id := range ids {
		if err := engine.Handle(ctx, id); err != nil {
			t.Fatalf("handle catalog facet: %v", err)
		}
	}
	checks := []struct {
		name, query string
		args        []any
	}{{"albums", `SELECT count(*) FROM albums WHERE id=$1`, []any{suffix + "-album"}}, {"galleries", `SELECT count(*) FROM galleries WHERE id=$1`, []any{suffix + "-gallery"}}, {"groups", `SELECT count(*) FROM groups WHERE nsid=$1`, []any{suffix + "-group"}}, {"person_groups", `SELECT count(*) FROM person_groups WHERE person_nsid=$1 AND group_nsid=$2`, []any{person, suffix + "-group"}}}
	for _, check := range checks {
		var n int
		if err := pool.QueryRow(ctx, check.query, check.args...).Scan(&n); err != nil || n != 1 {
			t.Fatalf("%s count=%d err=%v", check.name, n, err)
		}
	}
}

func TestChangeCheckFindsPhotoAndCrawlsOneIncrementalPage(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	subject := "check-person-" + suffix
	owner := "check-owner-" + suffix + "@N01"
	checkpoint := time.Now().UTC().Truncate(time.Second)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id=$1`, int64(710000000000002000))
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, owner)
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2`, userID, subject)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via) VALUES($1,$2,0,'test')`, userID, subject); err != nil {
		t.Fatal(err)
	}

	flickrFake := &checkIncrementalFlickr{subject: subject, owner: owner}
	checkCursor := domain.Cursor{Page: 1, Method: "flickr.people.getPhotos", CheckpointAt: checkpoint.Unix(), Params: map[string]string{
		"user_id": subject, "min_upload_date": "1700000000", "min_fave_date": "1700000100", "per_page": "1",
	}}
	encodedCursor, err := json.Marshal(checkCursor)
	if err != nil {
		t.Fatal(err)
	}
	var checkItemID int64
	if err := pool.QueryRow(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state,depth,cursor) VALUES('check',$1,$2,'ready',0,$3) RETURNING id`, subject, userID, encodedCursor).Scan(&checkItemID); err != nil {
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
	for _, facet := range []domain.Facet{crawlapp.ChangeCheck{}, facets.Photos{}, facets.Favorites{}, facets.Albums{}, facets.Galleries{}, facets.Groups{}} {
		if err := registry.Register(facet); err != nil {
			t.Fatal(err)
		}
	}
	engine := &crawlapp.Engine{
		Registry: registry, Store: store, Units: store, Keys: intKeys{}, Governor: intGovernor{},
		Flickr: flickrFake, Settings: photosFavoritesSettings{maxDepth: 1},
	}
	if err := engine.Handle(ctx, checkItemID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready' WHERE id=$1`, checkItemID); err != nil {
		t.Fatal(err)
	}
	if err := engine.Handle(ctx, checkItemID); err != nil {
		t.Fatal(err)
	}
	var photoItemID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM work_items WHERE user_id=$1 AND kind='photos' AND subject_key=$2`, userID, subject).Scan(&photoItemID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready' WHERE id=$1`, photoItemID); err != nil {
		t.Fatal(err)
	}
	if err := engine.Handle(ctx, photoItemID); err != nil {
		t.Fatal(err)
	}
	if len(flickrFake.calls) != 3 {
		t.Fatalf("Flickr calls = %d, want two probes and one crawl page", len(flickrFake.calls))
	}
	if got := count(t, ctx, pool, `SELECT count(*) FROM photos WHERE id=$1`, int64(710000000000002000)); got != 1 {
		t.Fatalf("crawled photos = %d, want one", got)
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM work_items WHERE id=$1`, photoItemID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != domain.StateDone {
		t.Fatalf("work item state = %q, want done after one page", state)
	}
	var lastSeen time.Time
	if err := pool.QueryRow(ctx, `SELECT last_seen_at FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2`, userID, subject).Scan(&lastSeen); err != nil {
		t.Fatal(err)
	}
	if !lastSeen.Equal(checkpoint) {
		t.Fatalf("last seen checkpoint = %s, want %s", lastSeen, checkpoint)
	}
}

func TestContactsPhotosReopensCheckWithStoredCheckpoints(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	person := "flagged-person"
	account := "contacts-account-" + suffix
	lastSeen := time.Date(2025, 4, 5, 6, 7, 8, 0, time.UTC)
	lastFavorite := time.Date(2025, 5, 6, 7, 8, 9, 0, time.UTC)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2`, userID, person)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via,last_seen_at,last_fave_at) VALUES($1,$2,0,'test',$3,$4)`, userID, person, lastSeen, lastFavorite); err != nil {
		t.Fatal(err)
	}
	checkCursor, err := json.Marshal(domain.Cursor{Page: 1, Method: "flickr.people.getPhotos"})
	if err != nil {
		t.Fatal(err)
	}
	var checkItemID int64
	if err := pool.QueryRow(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state,depth,cursor) VALUES('check',$1,$2,'done',0,$3) RETURNING id`, person, userID, checkCursor).Scan(&checkItemID); err != nil {
		t.Fatal(err)
	}
	contactsCursor, err := json.Marshal(domain.Cursor{Page: 1, Method: "flickr.photos.getContactsPhotos", CheckpointAt: time.Now().Unix(), Params: map[string]string{"user_id": account, "count": "50"}})
	if err != nil {
		t.Fatal(err)
	}
	var contactsItemID int64
	if err := pool.QueryRow(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state,depth,cursor) VALUES('contacts_photos',$1,$2,'ready',0,$3) RETURNING id`, account, userID, contactsCursor).Scan(&contactsItemID); err != nil {
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
	for _, facet := range []domain.Facet{crawlapp.ChangeCheck{}, crawlapp.ContactsPhotos{}} {
		if err := registry.Register(facet); err != nil {
			t.Fatal(err)
		}
	}
	engine := &crawlapp.Engine{Registry: registry, Store: store, Units: store, Keys: intKeys{}, Governor: intGovernor{}, Flickr: contactsPhotosFlagFlickr{}, Settings: photosFavoritesSettings{maxDepth: 1}}
	if err := engine.Handle(ctx, contactsItemID); err != nil {
		t.Fatal(err)
	}
	var state string
	var rawCursor []byte
	if err := pool.QueryRow(ctx, `SELECT state,cursor FROM work_items WHERE id=$1`, checkItemID).Scan(&state, &rawCursor); err != nil {
		t.Fatal(err)
	}
	var gotCursor domain.Cursor
	if err := json.Unmarshal(rawCursor, &gotCursor); err != nil {
		t.Fatal(err)
	}
	if state != domain.StatePending {
		t.Fatalf("existing check state = %q, want pending", state)
	}
	if gotCursor.Params["min_upload_date"] != fmt.Sprint(lastSeen.Unix()) || gotCursor.Params["min_fave_date"] != fmt.Sprint(lastFavorite.Unix()) {
		t.Fatalf("reopened check cursor params = %#v, want stored checkpoints", gotCursor.Params)
	}
	var lastChecked time.Time
	if err := pool.QueryRow(ctx, `SELECT last_checked_at FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2`, userID, person).Scan(&lastChecked); err != nil {
		t.Fatal(err)
	}
	if lastChecked.IsZero() {
		t.Fatal("last_checked_at was not stamped when contacts photos flagged the person")
	}
}

func insertPhotosFavoritesWork(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID int64, kind, subject string, depth int, facet domain.Facet) int64 {
	t.Helper()
	spec := facet.Plan(ctx, domain.Person{NSID: subject})[0]
	cursor, err := json.Marshal(domain.NewCursor(spec))
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO work_items (kind,subject_key,user_id,state,depth,cursor) VALUES ($1,$2,$3,'ready',$4,$5) RETURNING id`, kind, subject, userID, depth, cursor).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func photoFavoritesPayload(pages, page, count int, firstID int64, owner0, owner1 string) string {
	items := make([]string, count)
	for i := range items {
		owner, username := owner0, "Known Person"
		if i%2 == 1 {
			owner, username = owner1, "Other Person"
		}
		items[i] = fmt.Sprintf(`{"id":"%d","owner":%q,"ownername":%q,"secret":"s","server":"1","lastupdate":"1700000100","dateupload":"1700000000","date_faved":"1700000200","ispublic":1,"url_m":"https://example.invalid/medium"}`, firstID+int64(i), owner, username)
	}
	return fmt.Sprintf(`{"photos":{"page":%d,"pages":%d,"perpage":500,"photo":[%s]}}`, page, pages, strings.Join(items, ","))
}
