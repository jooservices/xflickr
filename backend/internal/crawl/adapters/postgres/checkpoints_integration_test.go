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

type observerCheckFlickr struct {
	totals map[int64]string
	calls  []crawlapp.Request
}

func (f *observerCheckFlickr) Call(_ context.Context, request crawlapp.Request) (json.RawMessage, error) {
	f.calls = append(f.calls, request)
	total, ok := f.totals[request.UserID]
	if !ok || request.Method != "flickr.people.getPhotos" || request.Params["per_page"] != "1" {
		return nil, fmt.Errorf("unexpected observer check request: %+v", request)
	}
	return json.RawMessage(fmt.Sprintf(`{"photos":{"total":%q}}`, total)), nil
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

func TestLegacyPhotoCursorRestartsFromFirstPageAndStoresCheckpoint(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("ckpt-legacy-%d", time.Now().UnixNano())
	person := suffix + "@N01"
	scanStart := time.Date(2026, 11, 9, 6, 30, 0, 0, time.UTC)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_interests WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, person)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES ($1,'p')`, person); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via) VALUES ($1,$2,0,'test')`, userID, person); err != nil {
		t.Fatal(err)
	}
	cursor, err := json.Marshal(domain.Cursor{Page: 3, Continuation: "legacy-cursor", Method: "flickr.people.getPhotos", Params: map[string]string{
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
		"flickr.people.getPhotos:1": json.RawMessage(photoFavoritesPayload(1, 1, 0, 0, person, person)),
	}}
	engine := newCheckpointTestEngine(t, pool, flickr, scanStart)
	if err := engine.Handle(ctx, itemID); err != nil {
		t.Fatal(err)
	}
	if len(flickr.calls) != 1 || flickr.calls[0].Params["page"] != "1" || flickr.calls[0].Params["continuation"] != "" {
		t.Fatalf("legacy cursor was not restarted at page one: %+v", flickr.calls)
	}
	var raw []byte
	var state string
	if err := pool.QueryRow(ctx, `SELECT state,cursor FROM work_items WHERE id=$1`, itemID).Scan(&state, &raw); err != nil {
		t.Fatal(err)
	}
	stored, err := domain.ParseCursor(raw)
	if err != nil || state != domain.StateDone || stored.Page != 1 || stored.CheckpointAt != scanStart.Unix() {
		t.Fatalf("completed legacy work item state=%q cursor=%+v err=%v", state, stored, err)
	}
	var checkpoint time.Time
	if err := pool.QueryRow(ctx, `SELECT last_seen_at FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2`, userID, person).Scan(&checkpoint); err != nil {
		t.Fatal(err)
	}
	if !checkpoint.Equal(scanStart) {
		t.Fatalf("checkpoint=%v, want scan start %v", checkpoint, scanStart)
	}
}

func TestChangeCheckUsesObserverSpecificAccessAndCheckpoints(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	userA := insertInterestTestUser(t, ctx, pool)
	userB := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("ckpt-observer-%d", time.Now().UnixNano())
	person := suffix + "@N01"
	photoA := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	photoB := photoA.Add(time.Hour)
	faveA := photoA.Add(2 * time.Hour)
	faveB := photoA.Add(3 * time.Hour)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=ANY($1)`, []int64{userA, userB})
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_interests WHERE user_id=ANY($1)`, []int64{userA, userB})
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, person)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES ($1,'p')`, person); err != nil {
		t.Fatal(err)
	}
	for _, observer := range []struct {
		userID int64
		photo  time.Time
		fave   time.Time
		photos string
	}{
		{userID: userA, photo: photoA, fave: faveA, photos: "1"},
		{userID: userB, photo: photoB, fave: faveB, photos: "0"},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via,last_seen_at,last_fave_at) VALUES ($1,$2,0,'test',$3,$4)`, observer.userID, person, observer.photo, observer.fave); err != nil {
			t.Fatal(err)
		}
		cursor, marshalErr := json.Marshal(domain.Cursor{Page: 1, Method: "flickr.people.getPhotos", Params: map[string]string{
			"user_id": person, domain.ParamMinUploadDate: fmt.Sprint(observer.photo.Unix()),
			domain.ParamMinFaveDate: fmt.Sprint(observer.fave.Unix()), "per_page": "1",
		}})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state,depth,cursor) VALUES ('check',$1,$2,'ready',0,$3)`, person, observer.userID, cursor); err != nil {
			t.Fatal(err)
		}
	}
	flickr := &observerCheckFlickr{totals: map[int64]string{userA: "1", userB: "0"}}
	engine := newCheckpointTestEngine(t, pool, flickr, time.Date(2026, 11, 9, 7, 0, 0, 0, time.UTC))
	for _, userID := range []int64{userA, userB} {
		var itemID int64
		if err := pool.QueryRow(ctx, `SELECT id FROM work_items WHERE user_id=$1 AND subject_key=$2 AND kind='check'`, userID, person).Scan(&itemID); err != nil {
			t.Fatal(err)
		}
		if err := engine.Handle(ctx, itemID); err != nil {
			t.Fatal(err)
		}
	}
	if len(flickr.calls) != 2 || flickr.calls[0].UserID != userA || flickr.calls[1].UserID != userB {
		t.Fatalf("check calls lost observer identity: %+v", flickr.calls)
	}
	if flickr.calls[0].Params[domain.ParamMinUploadDate] != fmt.Sprint(photoA.Unix()) ||
		flickr.calls[1].Params[domain.ParamMinUploadDate] != fmt.Sprint(photoB.Unix()) {
		t.Fatalf("photo checks used another observer's bound: %+v", flickr.calls)
	}
	var raw []byte
	err := pool.QueryRow(ctx, `SELECT cursor FROM work_items WHERE user_id=$1 AND subject_key=$2 AND kind='photos'`, userA, person).Scan(&raw)
	if err != nil {
		t.Fatalf("observer A's changed list was not queued: %v", err)
	}
	photoCursor, err := domain.ParseCursor(raw)
	if err != nil || photoCursor.Params[domain.ParamMinUploadDate] != fmt.Sprint(photoA.Unix()) {
		t.Fatalf("observer A's incremental cursor=%+v err=%v", photoCursor, err)
	}
	if count(t, ctx, pool, `SELECT count(*) FROM work_items WHERE user_id=$1 AND subject_key=$2 AND kind='photos'`, userB, person) != 0 {
		t.Fatal("observer B received a photo page unavailable in their view")
	}
}

func TestRepairZeroCheckpointsIsRepeatableAndOfflineNoFlickrCall(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	otherUserID := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("ckpt-repair-%d", time.Now().UnixNano())
	personA := suffix + "-a@N01"
	personB := suffix + "-b@N01"
	photoSeen := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	upload := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	faveSeen := time.Date(2026, 10, 2, 11, 0, 0, 0, time.UTC)
	photoCheckpoint := time.Date(2026, 9, 20, 8, 30, 0, 0, time.UTC)
	faveCheckpoint := time.Date(2026, 9, 21, 9, 30, 0, 0, time.UTC)
	existing := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	photoID := int64(730000000000000001)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM favorites WHERE person_nsid=ANY($1)`, []string{personA, personB})
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id=$1`, photoID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=ANY($1)`, []int64{userID, otherUserID})
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_interests WHERE user_id=ANY($1)`, []int64{userID, otherUserID})
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
		($1,$3,0,'test',$4,$4),
		($5,$2,0,'test',NULL,NULL)`, userID, personA, personB, existing, otherUserID); err != nil {
		t.Fatal(err)
	}
	for _, scan := range []struct {
		kind       string
		method     string
		checkpoint time.Time
	}{
		{kind: "photos", method: "flickr.people.getPhotos", checkpoint: photoCheckpoint},
		{kind: "favorites", method: "flickr.favorites.getList", checkpoint: faveCheckpoint},
	} {
		cursor, marshalErr := json.Marshal(domain.Cursor{Page: 1, Method: scan.method, CheckpointAt: scan.checkpoint.Unix()})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state,depth,cursor) VALUES ($1,$2,$3,'done',0,$4)`, scan.kind, personA, userID, cursor); err != nil {
			t.Fatal(err)
		}
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
	if !seen.Equal(photoCheckpoint) || !fave.Equal(faveCheckpoint) {
		t.Fatalf("repaired checkpoints seen=%v fave=%v", seen, fave)
	}
	var unscopedSeen, unscopedFave *time.Time
	if err := pool.QueryRow(ctx, `SELECT last_seen_at,last_fave_at FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2`, otherUserID, personA).Scan(&unscopedSeen, &unscopedFave); err != nil {
		t.Fatal(err)
	}
	if unscopedSeen != nil || unscopedFave != nil {
		t.Fatalf("other observer without completed scans received shared checkpoints: %v %v", unscopedSeen, unscopedFave)
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

func TestRepairCheckpointKeysetPassesBatchesWithoutEvidence(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("ckpt-keyset-%d", time.Now().UnixNano())
	people := []string{
		suffix + "-a@N01", suffix + "-b@N01", suffix + "-c@N01",
		suffix + "-d@N01", suffix + "-e@N01", suffix + "-f@N01",
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1 AND subject_key=ANY($2)`, userID, people)
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1 AND kind=$2`, userID, suffix+"-plan")
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_interests WHERE user_id=$1 AND person_nsid=ANY($2)`, userID, people)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=ANY($1)`, people)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) SELECT nsid,'repair' FROM unnest($1::text[]) AS nsid`, people); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via) SELECT $1,nsid,0,'test' FROM unnest($2::text[]) AS nsid`, userID, people); err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{2, 5} {
		for _, scan := range []struct{ kind, method string }{
			{kind: "photos", method: "flickr.people.getPhotos"},
			{kind: "favorites", method: "flickr.favorites.getList"},
		} {
			checkpoint := time.Date(2026, 11, 1+index, 10, 0, 0, 0, time.UTC)
			cursor, marshalErr := json.Marshal(domain.Cursor{Page: 1, Method: scan.method, CheckpointAt: checkpoint.Unix()})
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state,depth,cursor) VALUES ($1,$2,$3,'done',0,$4)`, scan.kind, people[index], userID, cursor); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state,depth)
		SELECT $1,$2 || '-' || number::text,$3,'done',0 FROM generate_series(1,1500) AS number`, suffix+"-plan", suffix, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE work_items`); err != nil {
		t.Fatal(err)
	}
	planRows, err := pool.Query(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
		SELECT (w.cursor->>'checkpoint_at')::bigint
		FROM work_items w
		WHERE w.kind='photos' AND w.subject_key=$1 AND w.user_id=$2 AND w.state='done'
		  AND COALESCE((w.cursor->>'checkpoint_at')::bigint,0)>0
		LIMIT 1`, people[2], userID)
	if err != nil {
		t.Fatal(err)
	}
	var plan strings.Builder
	for planRows.Next() {
		var line string
		if err := planRows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := planRows.Err(); err != nil {
		t.Fatal(err)
	}
	planRows.Close()
	if !strings.Contains(plan.String(), "work_items_dedupe_key") {
		t.Fatalf("completed scan lookup did not use the bounded unique index:\n%s", plan.String())
	}
	t.Logf("completed scan lookup plan:\n%s", plan.String())

	report, err := RepairZeroCheckpointsPool(ctx, pool, CheckpointRepairOptions{BatchSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if report.PhotosUpdated != 2 || report.FavoritesUpdated != 2 {
		t.Fatalf("keyset repair stopped before later eligible rows: %+v", report)
	}
	for _, index := range []int{0, 1, 3, 4} {
		var seen, fave *time.Time
		if err := pool.QueryRow(ctx, `SELECT last_seen_at,last_fave_at FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2`, userID, people[index]).Scan(&seen, &fave); err != nil {
			t.Fatal(err)
		}
		if seen != nil || fave != nil {
			t.Errorf("person %s without a completed scan got checkpoints: %v %v", people[index], seen, fave)
		}
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
	for _, facet := range []domain.Facet{facets.Photos{}, facets.Favorites{}, crawlapp.ChangeCheck{}} {
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
