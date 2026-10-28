package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	catalogpg "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/crawl/app"
	"github.com/jooservices/xflickr/internal/crawl/domain"
	"github.com/jooservices/xflickr/internal/crawl/facets"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	settingsdomain "github.com/jooservices/xflickr/internal/settings/domain"
	"github.com/jooservices/xflickr/pkg/flickr"
)

var errSimulatedCrash = errors.New("simulated crash before commit")

type intFacet struct{ name string }

func (f intFacet) Name() string      { return f.name }
func (f intFacet) Lane() domain.Lane { return domain.LanePerson }
func (f intFacet) Plan(_ context.Context, p domain.Person) []domain.WorkSpec {
	return []domain.WorkSpec{{Subject: p.NSID, Method: "flickr.people.getPhotos"}}
}

func (f intFacet) Handle(_ context.Context, page domain.Page) (domain.Result, error) {
	return domain.Result{
		People:       []catalog.Person{{NSID: page.Item.Subject, Username: "u-" + page.Item.Subject}},
		Observations: []domain.Observation{{Kind: "person.seen", Subject: page.Item.Subject}},
		Discoveries:  []domain.Discovery{{NSID: page.Item.Subject + "-child", Via: "contact"}},
		HasMore:      page.Number < 2,
	}, nil
}

type intKeys struct{}

func (intKeys) Choose(context.Context, string, domain.Lane) (int64, error) { return 1, nil }

type intPermit struct{}

func (intPermit) Done(error) error { return nil }

type intGovernor struct{}

func (intGovernor) Acquire(context.Context, int64, domain.Lane) (app.Permit, time.Duration, error) {
	return intPermit{}, 0, nil
}

type intFlickr struct {
	calls int
	err   error
}

func (f *intFlickr) Call(context.Context, app.Request) (json.RawMessage, error) {
	f.calls++
	return json.RawMessage(`{}`), f.err
}

type reconcileIntFlickr struct{ responses map[string]json.RawMessage }

func (f reconcileIntFlickr) Call(_ context.Context, request app.Request) (json.RawMessage, error) {
	key := request.Method + ":" + request.Params["page"]
	response, ok := f.responses[key]
	if !ok {
		return nil, fmt.Errorf("unexpected reconciliation request %s", key)
	}
	return response, nil
}

type intSettings struct{}

func (intSettings) Effective(context.Context, int64) (settingsdomain.Effective, error) {
	return settingsdomain.Effective{MaxDepth: 3}, nil
}

// crashOnce runs the whole commit callback and then fails, so the transaction
// rolls back after every write — a crash between the call and the commit.
type crashOnce struct {
	inner   app.UnitOfWork
	crashes int
}

func (c *crashOnce) Do(ctx context.Context, fn func(app.Writers) error) error {
	if c.crashes > 0 {
		c.crashes--
		return c.inner.Do(ctx, func(w app.Writers) error {
			if err := fn(w); err != nil {
				return err
			}
			return errSimulatedCrash
		})
	}
	return c.inner.Do(ctx, fn)
}

func openEngineTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../"))
	dbConfig, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatalf("load worktree database configuration: %v", err)
	}
	if err := dbtest.ValidateDatabaseName(dbConfig.Name); err != nil {
		t.Fatal(err)
	}
	return dbtest.Open(t, dbConfig)
}

func count(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestEngineCrashBetweenCallAndCommitRerunsSafely(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	subject := "engine-" + suffix
	kind := "engine_" + suffix
	cursor, _ := json.Marshal(domain.Cursor{Page: 1, Method: "flickr.people.getPhotos"})
	var itemID int64
	if err := pool.QueryRow(ctx, `INSERT INTO work_items (kind, subject_key, user_id, state, depth, cursor) VALUES ($1, $2, $3, 'ready', 0, $4) RETURNING id`, kind, subject, userID, cursor).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id = $1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid LIKE $1`, subject+"%")
	})

	store, err := NewEngine(pool, func(c Conn) app.CatalogWriter {
		repo, repoErr := catalogpg.New(c)
		if repoErr != nil {
			t.Fatal(repoErr)
		}
		return repo
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := app.NewRegistry()
	if err := registry.Register(intFacet{name: kind}); err != nil {
		t.Fatal(err)
	}
	flickrCalls := &intFlickr{}
	crash := &crashOnce{inner: store, crashes: 1}
	engine := &app.Engine{Registry: registry, Store: store, Units: crash, Keys: intKeys{}, Governor: intGovernor{}, Flickr: flickrCalls, Settings: intSettings{}}

	if err := engine.Handle(ctx, itemID); !errors.Is(err, errSimulatedCrash) {
		t.Fatalf("first run err = %v, want the simulated crash", err)
	}
	if n := count(t, ctx, pool, `SELECT count(*) FROM people WHERE nsid LIKE $1`, subject+"%"); n != 0 {
		t.Fatalf("rolled back run left %d people", n)
	}
	if n := count(t, ctx, pool, `SELECT count(*) FROM crawl_observations WHERE work_item_id = $1`, itemID); n != 0 {
		t.Fatalf("rolled back run left %d observations", n)
	}
	var state string
	var page int
	readItem := func() {
		if err := pool.QueryRow(ctx, `SELECT state, (cursor->>'page')::int FROM work_items WHERE id = $1`, itemID).Scan(&state, &page); err != nil {
			t.Fatal(err)
		}
	}
	if readItem(); state != "running" || page != 1 {
		t.Fatalf("after crash state=%s page=%d, want running page 1", state, page)
	}

	if err := engine.Handle(ctx, itemID); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if readItem(); state != "pending" || page != 2 {
		t.Fatalf("after page 1 state=%s page=%d, want pending page 2", state, page)
	}
	if n := count(t, ctx, pool, `SELECT count(*) FROM crawl_interests WHERE user_id = $1 AND person_nsid = $2 AND depth = 1`, userID, subject+"-child"); n != 1 {
		t.Fatalf("child interest count = %d", n)
	}
	if n := count(t, ctx, pool, `SELECT count(*) FROM work_items WHERE user_id = $1 AND subject_key = $2 AND depth = 1`, userID, subject+"-child"); n != 1 {
		t.Fatalf("child work count = %d", n)
	}

	// The scheduler dispatches page 2, then a duplicate job for the finished item is a no-op.
	if _, err := pool.Exec(ctx, `UPDATE work_items SET state = 'ready' WHERE id = $1`, itemID); err != nil {
		t.Fatal(err)
	}
	if err := engine.Handle(ctx, itemID); err != nil {
		t.Fatal(err)
	}
	if n := count(t, ctx, pool, `SELECT count(*) FROM crawl_observations WHERE work_item_id = $1`, itemID); n != 2 {
		t.Fatalf("observations = %d, want page 1 and page 2 once each", n)
	}
	if readItem(); state != "done" {
		t.Fatalf("after last page state = %s, want done", state)
	}
	before := count(t, ctx, pool, `SELECT count(*) FROM work_items WHERE user_id = $1`, userID)
	callsBefore := flickrCalls.calls
	if err := engine.Handle(ctx, itemID); err != nil {
		t.Fatal(err)
	}
	if after := count(t, ctx, pool, `SELECT count(*) FROM work_items WHERE user_id = $1`, userID); after != before || flickrCalls.calls != callsBefore {
		t.Fatalf("a finished item changed state: items %d -> %d, calls %d -> %d", before, after, callsBefore, flickrCalls.calls)
	}
}

func TestRecoverableFlickrFailureKeepsWorkItemPending(t *testing.T) {
	cases := []struct {
		name      string
		cause     error
		start     int
		wantState string
		wantTries int
		wantWait  time.Duration
		wantError string
	}{
		{name: "first rate limit", cause: flickr.ErrRateLimited, start: 0, wantState: "pending", wantTries: 1, wantWait: 30 * time.Second, wantError: "rate limited"},
		{name: "second service unavailable", cause: flickr.ErrServiceUnavailable, start: 1, wantState: "pending", wantTries: 2, wantWait: time.Minute, wantError: "service unavailable"},
		{name: "backoff is capped", cause: flickr.ErrRateLimited, start: 6, wantState: "pending", wantTries: 7, wantWait: app.MaxRetryDelay, wantError: "rate limited"},
		{name: "budget exhausted fails with a reason", cause: flickr.ErrRateLimited, start: 19, wantState: "failed", wantTries: 19, wantError: "after 20 attempts"},
		{name: "resource denied 98 is terminal", cause: &flickr.CallError{Kind: errors.Join(flickr.ErrResourceDenied, flickr.ErrPermission), Code: 98, Message: "Permission denied"}, start: 2, wantState: "failed", wantTries: 2, wantError: "unavailable: flickr: Permission denied (code 98)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := openEngineTestPool(t)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			userID := insertInterestTestUser(t, ctx, pool)
			suffix := fmt.Sprintf("%d", time.Now().UnixNano())
			subject := "retry-" + suffix
			kind := "retry_" + suffix
			cursor, _ := json.Marshal(domain.Cursor{Page: 3, Method: "flickr.people.getPhotos", Params: map[string]string{"user_id": subject}})
			var itemID int64
			if err := pool.QueryRow(ctx, `INSERT INTO work_items (kind, subject_key, user_id, state, depth, attempts, cursor) VALUES ($1, $2, $3, 'ready', 0, $5, $4) RETURNING id`, kind, subject, userID, cursor, tc.start).Scan(&itemID); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE id=$1`, itemID) })
			store, err := NewEngine(pool, func(c Conn) app.CatalogWriter {
				repo, repoErr := catalogpg.New(c)
				if repoErr != nil {
					t.Fatal(repoErr)
				}
				return repo
			})
			if err != nil {
				t.Fatal(err)
			}
			registry := app.NewRegistry()
			if err := registry.Register(intFacet{name: kind}); err != nil {
				t.Fatal(err)
			}
			engine := &app.Engine{Registry: registry, Store: store, Units: store, Keys: intKeys{}, Governor: intGovernor{}, Flickr: &intFlickr{err: tc.cause}, Settings: intSettings{}, Timing: app.Timing{MaxAttempts: 5, RecoverableMaxAttempts: 20, Backoff: 30 * time.Second}}
			before := time.Now()
			if err := engine.Handle(ctx, itemID); err != nil {
				t.Fatal(err)
			}
			var state string
			var attempts int
			var gotCursor []byte
			var lastError *string
			var notBefore time.Time
			if err := pool.QueryRow(ctx, `SELECT state, attempts, cursor, last_error, not_before FROM work_items WHERE id=$1`, itemID).Scan(&state, &attempts, &gotCursor, &lastError, &notBefore); err != nil {
				t.Fatal(err)
			}
			parsed, err := domain.ParseCursor(gotCursor)
			if err != nil {
				t.Fatal(err)
			}
			if lastError == nil || !strings.Contains(*lastError, tc.wantError) {
				t.Fatalf("last_error = %v, want it to contain %q", lastError, tc.wantError)
			}
			if tc.wantWait > 0 {
				if wait := notBefore.Sub(before); wait < tc.wantWait-time.Second || wait > tc.wantWait+10*time.Second {
					t.Fatalf("not_before is %s ahead, want about %s", wait, tc.wantWait)
				}
			}
			if state != tc.wantState || attempts != tc.wantTries || parsed.Page != 3 || parsed.Params["user_id"] != subject {
				t.Fatalf("state=%s attempts=%d cursor=%+v; want %s/%d with original page/subject", state, attempts, parsed, tc.wantState, tc.wantTries)
			}
		})
	}
}

func TestReconcileTombstonesOnlySafeMissingRowsAndPublishesVisibilityChanges(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	ownerUserID := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	person := "reconcile-owner-" + suffix
	photoIDs := []int64{time.Now().UnixNano(), time.Now().UnixNano() + 1, time.Now().UnixNano() + 2}
	scannedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=ANY($1::bigint[])`, []int64{userID, ownerUserID})
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_interests WHERE user_id=ANY($1::bigint[]) AND person_nsid=$2`, []int64{userID, ownerUserID}, person)
		_, _ = pool.Exec(context.Background(), `DELETE FROM favorites WHERE person_nsid=$1`, person)
		_, _ = pool.Exec(context.Background(), `DELETE FROM outbox_events WHERE topic='photo.visibility_changed' AND key=ANY($1)`, []string{fmt.Sprint(photoIDs[0])})
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id=ANY($1::bigint[])`, photoIDs)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, person)
	})

	catalogRepository, err := catalogpg.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := catalogRepository.UpsertPeople(ctx, []catalog.Person{{NSID: person, Username: person}}); err != nil {
		t.Fatal(err)
	}
	photos := []catalog.Photo{
		{ID: photoIDs[0], OwnerNSID: person, LastUpdate: time.Now().UTC(), Visibility: catalog.VisibilityPublic, Raw: []byte(`{"id":"present"}`)},
		{ID: photoIDs[1], OwnerNSID: person, LastUpdate: time.Now().UTC(), Visibility: catalog.VisibilityPublic, Raw: []byte(`{"id":"missing-public"}`)},
		{ID: photoIDs[2], OwnerNSID: person, LastUpdate: time.Now().UTC(), Visibility: catalog.VisibilityPrivate, Raw: []byte(`{"id":"missing-private"}`)},
	}
	if err := catalogRepository.UpsertPhotos(ctx, photos); err != nil {
		t.Fatal(err)
	}
	if err := catalogRepository.UpsertFavoritesBatch(ctx, []catalogpg.Favorite{{PersonNSID: person, PhotoID: photoIDs[1], FaveDate: scannedAt.Add(-24 * time.Hour), SeenAt: scannedAt.Add(-2 * time.Minute)}}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE photos SET last_seen_at=$2 WHERE id=ANY($1::bigint[])`, photoIDs, scannedAt.Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via,last_reconciled_at) VALUES($1,$2,0,'test',$3)`, userID, person, scannedAt.Add(-31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var ownerKeyID int64
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,'reconcile test key',$2,'{}',3600,4,5) RETURNING id`, ownerUserID, "reconcile-key-"+suffix).Scan(&ownerKeyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed) VALUES($1,$2,$3,'owner','{}','{}')`, ownerUserID, ownerKeyID, person); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via,last_reconciled_at) VALUES($1,$2,0,'test',$3)`, ownerUserID, person, scannedAt.Add(-31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	photoResponse := json.RawMessage(fmt.Sprintf(`{"photos":{"page":1,"pages":1,"perpage":500,"photo":[{"id":%q,"owner":%q,"dateupload":"1700000000","lastupdate":"1700000100","ispublic":0,"isfriend":1,"isfamily":0}]}}`, fmt.Sprint(photoIDs[0]), person))
	flickr := reconcileIntFlickr{responses: map[string]json.RawMessage{
		"flickr.people.getPhotos:1":  photoResponse,
		"flickr.favorites.getList:1": json.RawMessage(`{"photos":{"page":1,"pages":0,"perpage":500,"photo":[]}}`),
	}}
	cursor, err := json.Marshal(domain.Cursor{Page: 1, Method: "flickr.people.getPhotos", CheckpointAt: scannedAt.Unix(), Params: map[string]string{"user_id": person, "per_page": "500", "extras": app.ReconcilePhotoExtras}})
	if err != nil {
		t.Fatal(err)
	}
	var itemID int64
	if err := pool.QueryRow(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state,depth,cursor) VALUES('reconcile',$1,$2,'ready',0,$3) RETURNING id`, person, userID, cursor).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	store, err := NewEngine(pool, func(conn Conn) app.CatalogWriter {
		repository, repositoryErr := catalogpg.New(conn)
		if repositoryErr != nil {
			t.Fatal(repositoryErr)
		}
		return repository
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := app.NewRegistry()
	if err := registry.Register(facets.Reconcile{}); err != nil {
		t.Fatal(err)
	}
	engine := &app.Engine{Registry: registry, Store: store, Units: store, Keys: intKeys{}, Governor: intGovernor{}, Flickr: flickr, Settings: intSettings{}}
	if err := engine.Handle(ctx, itemID); err != nil {
		t.Fatalf("photo reconciliation page: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready' WHERE id=$1`, itemID); err != nil {
		t.Fatal(err)
	}
	if err := engine.Handle(ctx, itemID); err != nil {
		t.Fatalf("favorite reconciliation page: %v", err)
	}

	var publicTombstone, privateTombstone *time.Time
	var presentVisibility string
	if err := pool.QueryRow(ctx, `SELECT tombstoned_at FROM photos WHERE id=$1`, photoIDs[1]).Scan(&publicTombstone); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT tombstoned_at FROM photos WHERE id=$1`, photoIDs[2]).Scan(&privateTombstone); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT visibility FROM photos WHERE id=$1`, photoIDs[0]).Scan(&presentVisibility); err != nil {
		t.Fatal(err)
	}
	if publicTombstone == nil || privateTombstone != nil || presentVisibility != string(catalog.VisibilityFriends) {
		t.Fatalf("reconcile results public tombstone=%v private tombstone=%v present visibility=%q", publicTombstone, privateTombstone, presentVisibility)
	}
	var eventPayload []byte
	if err := pool.QueryRow(ctx, `SELECT payload FROM outbox_events WHERE topic='photo.visibility_changed' AND key=$1`, fmt.Sprint(photoIDs[0])).Scan(&eventPayload); err != nil {
		t.Fatalf("visibility change event: %v", err)
	}
	var event struct {
		PhotoID int64  `json:"photo_id"`
		Owner   string `json:"owner_nsid"`
		From    string `json:"from"`
		To      string `json:"to"`
	}
	if err := json.Unmarshal(eventPayload, &event); err != nil || event.PhotoID != photoIDs[0] || event.Owner != person || event.From != "public" || event.To != "friends" {
		t.Fatalf("visibility event=%+v payload=%s err=%v", event, eventPayload, err)
	}
	var removedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT removed_at FROM favorites WHERE person_nsid=$1 AND photo_id=$2`, person, photoIDs[1]).Scan(&removedAt); err != nil || removedAt != nil {
		t.Fatalf("non-owner scan changed favorite removal state: removed_at=%v err=%v", removedAt, err)
	}
	if got, err := catalogRepository.ListFavorites(ctx, person, nil, 20); err != nil || len(got.Items) != 1 {
		t.Fatalf("default favorite list=%+v err=%v", got, err)
	}
	var reconciledAt time.Time
	if err := pool.QueryRow(ctx, `SELECT last_reconciled_at FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2`, userID, person).Scan(&reconciledAt); err != nil || !reconciledAt.After(scannedAt) {
		t.Fatalf("reconcile completion timestamp=%s err=%v", reconciledAt, err)
	}

	var ownerItemID int64
	if err := pool.QueryRow(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state,depth,cursor) VALUES('reconcile',$1,$2,'ready',0,$3) RETURNING id`, person, ownerUserID, cursor).Scan(&ownerItemID); err != nil {
		t.Fatal(err)
	}
	if err := engine.Handle(ctx, ownerItemID); err != nil {
		t.Fatalf("owner photo reconciliation page: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready' WHERE id=$1`, ownerItemID); err != nil {
		t.Fatal(err)
	}
	if err := engine.Handle(ctx, ownerItemID); err != nil {
		t.Fatalf("owner favorite reconciliation page: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT tombstoned_at FROM photos WHERE id=$1`, photoIDs[2]).Scan(&privateTombstone); err != nil || privateTombstone == nil {
		t.Fatalf("owner scan did not reconcile its private photo: tombstoned_at=%v err=%v", privateTombstone, err)
	}
	if err := pool.QueryRow(ctx, `SELECT removed_at FROM favorites WHERE person_nsid=$1 AND photo_id=$2`, person, photoIDs[1]).Scan(&removedAt); err != nil || removedAt == nil {
		t.Fatalf("owner scan did not reconcile its missing favorite: removed_at=%v err=%v", removedAt, err)
	}
	if got, err := catalogRepository.ListFavorites(ctx, person, nil, 20); err != nil || len(got.Items) != 0 {
		t.Fatalf("owner-scanned default favorite list=%+v err=%v", got, err)
	}
}

func TestEnqueueDoesNotReopenCompletedFullCrawlWork(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	subject := fmt.Sprintf("done-full-crawl-%d", time.Now().UnixNano())
	cursor, err := json.Marshal(domain.Cursor{Page: 1, Method: "flickr.people.getPhotos", Params: map[string]string{"user_id": subject}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state,depth,attempts,last_error,cursor) VALUES('photos',$1,$2,'done',0,3,'previous failure',$3)`, subject, userID, cursor); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1`, userID) })
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
	if err := store.Do(ctx, func(w app.Writers) error {
		return w.Work.Enqueue(ctx, app.NewWork{Kind: "photos", Subject: subject, UserID: userID, Cursor: domain.Cursor{Page: 1, Method: "flickr.people.getPhotos"}})
	}); err != nil {
		t.Fatal(err)
	}
	var state string
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT state,attempts FROM work_items WHERE user_id=$1 AND kind='photos' AND subject_key=$2`, userID, subject).Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "done" || attempts != 3 {
		t.Fatalf("completed full crawl item state=%q attempts=%d, want done and 3", state, attempts)
	}
}

func TestEngineStopFailsOwnerItemsAndPublishes(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	other := insertInterestTestUser(t, ctx, pool)
	for _, owner := range []int64{userID, userID, other} {
		if _, err := pool.Exec(ctx, `INSERT INTO work_items (kind, subject_key, user_id, state) VALUES ('stop_test', $1, $2, 'pending')`, fmt.Sprintf("s-%d-%d", owner, time.Now().UnixNano()), owner); err != nil {
			t.Fatal(err)
		}
	}
	store, err := NewEngine(pool, func(Conn) app.CatalogWriter { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Stop(ctx, app.StopRequest{UserID: userID, Reason: app.StopTokenRevoked}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, ctx, pool, `SELECT count(*) FROM work_items WHERE user_id = $1 AND state = 'failed' AND last_error = 'token_revoked'`, userID); n != 2 {
		t.Fatalf("stopped = %d, want 2", n)
	}
	if n := count(t, ctx, pool, `SELECT count(*) FROM work_items WHERE user_id = $1 AND state = 'pending'`, other); n != 1 {
		t.Fatalf("another user's item must stay pending, got %d", n)
	}
	if n := count(t, ctx, pool, `SELECT count(*) FROM outbox_events WHERE topic = $1 AND key = $2`, TopicCrawlStopped, fmt.Sprintf("user_id:%d", userID)); n != 1 {
		t.Fatalf("crawl.stopped events = %d, want 1", n)
	}
}
