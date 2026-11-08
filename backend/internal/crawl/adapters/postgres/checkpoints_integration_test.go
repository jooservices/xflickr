package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	catalogpg "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	crawlapp "github.com/jooservices/xflickr/internal/crawl/app"
	"github.com/jooservices/xflickr/internal/crawl/domain"
	"github.com/jooservices/xflickr/internal/crawl/facets"
)

type recordingFlickr struct {
	calls    []crawlapp.Request
	payloads map[string]json.RawMessage
}

func (f *recordingFlickr) Call(_ context.Context, request crawlapp.Request) (json.RawMessage, error) {
	f.calls = append(f.calls, request)
	key := request.Method + ":" + request.Params["page"]
	if payload, ok := f.payloads[key]; ok {
		return payload, nil
	}
	if payload, ok := f.payloads[request.Method]; ok {
		return payload, nil
	}
	return nil, fmt.Errorf("unexpected Flickr call %s %#v", request.Method, request.Params)
}

type forbiddenFlickr struct{ calls int }

func (f *forbiddenFlickr) Call(context.Context, crawlapp.Request) (json.RawMessage, error) {
	f.calls++
	return nil, fmt.Errorf("Flickr must not be called during checkpoint repair")
}

func TestInitialScanStoresCheckpointOnlyAfterAllPages(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("ckpt-initial-%d", time.Now().UnixNano())
	person := suffix + "@N01"
	owner := suffix + "-owner@N01"
	scanStart := time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE owner_nsid=ANY($1)`, []string{owner, person})
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=ANY($1)`, []string{owner, person})
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_interests WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1`, userID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES ($1,'p'),($2,'o')`, person, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via) VALUES ($1,$2,0,'test')`, userID, person); err != nil {
		t.Fatal(err)
	}
	cursor, err := json.Marshal(domain.Cursor{Page: 1, Method: "flickr.people.getPhotos", Params: map[string]string{
		"user_id": person, "per_page": "500", "extras": "url_m,date_upload,last_update,owner_name",
	}})
	if err != nil {
		t.Fatal(err)
	}
	var itemID int64
	if err := pool.QueryRow(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state,depth,cursor) VALUES ('photos',$1,$2,'ready',0,$3) RETURNING id`, person, userID, cursor).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	flickr := &recordingFlickr{payloads: map[string]json.RawMessage{
		"flickr.people.getPhotos:1": json.RawMessage(photoFavoritesPayload(2, 1, 1, 720000000000000001, owner, owner)),
		"flickr.people.getPhotos:2": json.RawMessage(photoFavoritesPayload(2, 2, 1, 720000000000000002, owner, owner)),
	}}
	engine := newCheckpointTestEngine(t, pool, flickr, scanStart)
	if err := engine.Handle(ctx, itemID); err != nil {
		t.Fatalf("page 1: %v", err)
	}
	var lastSeen *time.Time
	if err := pool.QueryRow(ctx, `SELECT last_seen_at FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2`, userID, person).Scan(&lastSeen); err != nil {
		t.Fatal(err)
	}
	if lastSeen != nil {
		t.Fatalf("partial scan stored checkpoint %v", lastSeen)
	}
	var storedCursor domain.Cursor
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT cursor FROM work_items WHERE id=$1`, itemID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &storedCursor); err != nil {
		t.Fatal(err)
	}
	if storedCursor.CheckpointAt != scanStart.Unix() {
		t.Fatalf("cursor checkpoint = %d, want scan start %d", storedCursor.CheckpointAt, scanStart.Unix())
	}
	if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready' WHERE id=$1`, itemID); err != nil {
		t.Fatal(err)
	}
	if err := engine.Handle(ctx, itemID); err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT last_seen_at FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2`, userID, person).Scan(&lastSeen); err != nil {
		t.Fatal(err)
	}
	if lastSeen == nil || !lastSeen.Equal(scanStart) {
		t.Fatalf("completed scan checkpoint = %v, want %v", lastSeen, scanStart)
	}
}

func TestFailedIncrementalPageKeepsCheckpoint(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("ckpt-fail-%d", time.Now().UnixNano())
	person := suffix + "@N01"
	checkpoint := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_interests WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, person)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES ($1,'p')`, person); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via,last_seen_at) VALUES ($1,$2,0,'test',$3)`, userID, person, checkpoint); err != nil {
		t.Fatal(err)
	}
	cursor, err := json.Marshal(domain.Cursor{
		Page: 1, Method: "flickr.people.getPhotos", CheckpointAt: time.Now().UTC().Unix(),
		Params: map[string]string{"user_id": person, "min_upload_date": fmt.Sprint(checkpoint.Unix()), "per_page": "500", "extras": "url_m"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var itemID int64
	if err := pool.QueryRow(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state,depth,cursor) VALUES ('photos',$1,$2,'ready',0,$3) RETURNING id`, person, userID, cursor).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	engine := newCheckpointTestEngine(t, pool, &forbiddenFlickr{}, time.Now().UTC())
	if err := engine.Handle(ctx, itemID); err != nil {
		t.Fatalf("failed page should defer without surfacing: %v", err)
	}
	var kept time.Time
	if err := pool.QueryRow(ctx, `SELECT last_seen_at FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2`, userID, person).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if !kept.Equal(checkpoint) {
		t.Fatalf("checkpoint moved to %v after failed page, want %v", kept, checkpoint)
	}
}

func TestRepairZeroCheckpointsIsRepeatableAndOfflineNoFlickrCall(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("ckpt-repair-%d", time.Now().UnixNano())
	personA := suffix + "-a@N01"
	personB := suffix + "-b@N01"
	photoSeen := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	upload := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	faveSeen := time.Date(2026, 10, 2, 11, 0, 0, 0, time.UTC)
	existing := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	photoID := int64(730000000000000001)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM favorites WHERE person_nsid=ANY($1)`, []string{personA, personB})
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id=$1`, photoID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_interests WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=ANY($1)`, []string{personA, personB})
	})
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES ($1,'a'),($2,'b')`, personA, personB); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO photos(id,owner_nsid,title,uploaded_at,last_update,last_seen_at,visibility,raw,content_hash)
		VALUES ($1,$2,'t',$3,$3,$4,'public','{}',decode(repeat('11',32),'hex'))`, photoID, personA, upload, photoSeen); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO favorites(person_nsid,photo_id,fave_date,seen_at) VALUES ($1,$2,$3,$4)`, personA, photoID, upload, faveSeen); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via,last_seen_at,last_fave_at) VALUES
		($1,$2,0,'test',NULL,NULL),
		($1,$3,0,'test',$4,$4)`, userID, personA, personB, existing); err != nil {
		t.Fatal(err)
	}
	// Repair has no Flickr port; a process that mistakenly called Flickr would
	// have to invent one. Keep a sentinel fake in scope for the AC wording.
	flickr := &forbiddenFlickr{}
	report, err := RepairZeroCheckpointsPool(ctx, pool, CheckpointRepairOptions{BatchSize: 500})
	if err != nil {
		t.Fatal(err)
	}
	if report.PhotosUpdated != 1 || report.FavoritesUpdated != 1 {
		t.Fatalf("report=%+v", report)
	}
	if flickr.calls != 0 {
		t.Fatalf("repair called Flickr %d times", flickr.calls)
	}
	var seen, fave time.Time
	if err := pool.QueryRow(ctx, `SELECT last_seen_at, last_fave_at FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2`, userID, personA).Scan(&seen, &fave); err != nil {
		t.Fatal(err)
	}
	if !seen.Equal(photoSeen) || !fave.Equal(faveSeen) {
		t.Fatalf("repaired checkpoints seen=%v fave=%v", seen, fave)
	}
	var otherSeen, otherFave time.Time
	if err := pool.QueryRow(ctx, `SELECT last_seen_at, last_fave_at FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2`, userID, personB).Scan(&otherSeen, &otherFave); err != nil {
		t.Fatal(err)
	}
	if !otherSeen.Equal(existing) || !otherFave.Equal(existing) {
		t.Fatalf("non-zero checkpoint changed: %v %v", otherSeen, otherFave)
	}
	second, err := RepairZeroCheckpointsPool(ctx, pool, CheckpointRepairOptions{BatchSize: 500})
	if err != nil {
		t.Fatal(err)
	}
	if second.PhotosUpdated != 0 || second.FavoritesUpdated != 0 {
		t.Fatalf("second run changed rows: %+v", second)
	}
}

func TestCycleAfterInitialScanIssuesOneFilteredPage(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("ckpt-cycle-%d", time.Now().UnixNano())
	person := suffix + "@N01"
	owner := suffix + "-owner@N01"
	scanStart := time.Date(2026, 11, 9, 7, 0, 0, 0, time.UTC)
	later := scanStart.Add(24 * time.Hour)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE owner_nsid=ANY($1)`, []string{owner, person})
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=ANY($1)`, []string{owner, person})
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_interests WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1`, userID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES ($1,'p'),($2,'o')`, person, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via) VALUES ($1,$2,0,'test')`, userID, person); err != nil {
		t.Fatal(err)
	}
	cursor, err := json.Marshal(domain.Cursor{Page: 1, Method: "flickr.people.getPhotos", Params: map[string]string{
		"user_id": person, "per_page": "500", "extras": "url_m,date_upload,last_update,owner_name",
	}})
	if err != nil {
		t.Fatal(err)
	}
	var photosID int64
	if err := pool.QueryRow(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state,depth,cursor) VALUES ('photos',$1,$2,'ready',0,$3) RETURNING id`, person, userID, cursor).Scan(&photosID); err != nil {
		t.Fatal(err)
	}
	flickr := &recordingFlickr{payloads: map[string]json.RawMessage{
		"flickr.people.getPhotos:1":  json.RawMessage(photoFavoritesPayload(1, 1, 1, 740000000000000001, owner, owner)),
		"flickr.favorites.getList:1": json.RawMessage(`{"photos":{"total":"0","pages":0,"photo":[]}}`),
	}}
	engine := newCheckpointTestEngine(t, pool, flickr, scanStart)
	if err := engine.Handle(ctx, photosID); err != nil {
		t.Fatal(err)
	}
	initialCalls := len(flickr.calls)
	checkCursor, err := json.Marshal(domain.Cursor{
		Page: 1, Method: "flickr.people.getPhotos", CheckpointAt: later.Unix(),
		Params: map[string]string{
			"user_id": person, "min_upload_date": fmt.Sprint(scanStart.Unix()), "min_fave_date": "0", "per_page": "1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var checkID int64
	if err := pool.QueryRow(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state,depth,cursor) VALUES ('check',$1,$2,'ready',0,$3) RETURNING id`, person, userID, checkCursor).Scan(&checkID); err != nil {
		t.Fatal(err)
	}
	registry := crawlapp.NewRegistry()
	for _, facet := range []domain.Facet{facets.Photos{}, facets.Favorites{}, crawlapp.ChangeCheck{}, facets.Albums{}, facets.Galleries{}, facets.Groups{}} {
		if err := registry.Register(facet); err != nil {
			t.Fatal(err)
		}
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
	checkEngine := &crawlapp.Engine{
		Registry: registry, Store: store, Units: store, Keys: intKeys{}, Governor: intGovernor{},
		Flickr: flickr, Settings: photosFavoritesSettings{maxDepth: 1}, Now: func() time.Time { return later },
	}
	flickr.payloads["flickr.people.getPhotos:1"] = json.RawMessage(`{"photos":{"total":"1","pages":1,"photo":[]}}`)
	if err := checkEngine.Handle(ctx, checkID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready' WHERE id=$1`, checkID); err != nil {
		t.Fatal(err)
	}
	if err := checkEngine.Handle(ctx, checkID); err != nil {
		t.Fatal(err)
	}
	var photoWorkID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM work_items WHERE user_id=$1 AND kind='photos' AND subject_key=$2 AND state='pending'`, userID, person).Scan(&photoWorkID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready' WHERE id=$1`, photoWorkID); err != nil {
		t.Fatal(err)
	}
	flickr.payloads["flickr.people.getPhotos:1"] = json.RawMessage(photoFavoritesPayload(1, 1, 1, 740000000000000002, owner, owner))
	beforeIncremental := len(flickr.calls)
	if err := checkEngine.Handle(ctx, photoWorkID); err != nil {
		t.Fatal(err)
	}
	incremental := flickr.calls[beforeIncremental:]
	if len(incremental) != 1 {
		t.Fatalf("incremental calls = %d (%#v), want 1 filtered page; initial=%d total=%d", len(incremental), incremental, initialCalls, len(flickr.calls))
	}
	if incremental[0].Params["min_upload_date"] != fmt.Sprint(scanStart.Unix()) || incremental[0].Params["per_page"] != "500" {
		t.Fatalf("incremental page params = %#v", incremental[0].Params)
	}
}

func newCheckpointTestEngine(t *testing.T, pool *pgxpool.Pool, flickr crawlapp.Flickr, now time.Time) *crawlapp.Engine {
	t.Helper()
	registry := crawlapp.NewRegistry()
	for _, facet := range []domain.Facet{facets.Photos{}, facets.Favorites{}} {
		if err := registry.Register(facet); err != nil {
			t.Fatal(err)
		}
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
	return &crawlapp.Engine{
		Registry: registry, Store: store, Units: store, Keys: intKeys{}, Governor: intGovernor{},
		Flickr: flickr, Settings: photosFavoritesSettings{maxDepth: 1}, Now: func() time.Time { return now },
	}
}
