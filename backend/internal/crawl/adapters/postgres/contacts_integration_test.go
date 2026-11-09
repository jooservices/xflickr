package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	catalogpg "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	catalogapp "github.com/jooservices/xflickr/internal/catalog/app"
	catalogdomain "github.com/jooservices/xflickr/internal/catalog/domain"
	crawlapp "github.com/jooservices/xflickr/internal/crawl/app"
	"github.com/jooservices/xflickr/internal/crawl/facets"
)

type countingContactsFlickr struct {
	calls atomic.Int64
	body  json.RawMessage
}

type contactsResponse struct {
	body json.RawMessage
	err  error
}

type scriptedContactsFlickr struct {
	responses []contactsResponse
	calls     int
}

func (f *scriptedContactsFlickr) Call(_ context.Context, request crawlapp.Request) (json.RawMessage, error) {
	if request.Method != "flickr.contacts.getList" {
		return nil, fmt.Errorf("unexpected method %s", request.Method)
	}
	if f.calls >= len(f.responses) {
		return nil, errors.New("unexpected contacts request")
	}
	response := f.responses[f.calls]
	f.calls++
	return response.body, response.err
}

func contactsRefreshEngine(t *testing.T, pool *pgxpool.Pool, flickr crawlapp.Flickr) *crawlapp.Engine {
	t.Helper()
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
	if err := registry.Register(facets.Contacts{}); err != nil {
		t.Fatal(err)
	}
	return &crawlapp.Engine{
		Registry: registry, Store: store, Units: store, Keys: intKeys{}, Governor: intGovernor{},
		Flickr: flickr, Settings: photosFavoritesSettings{maxDepth: 2},
	}
}

func setContactsCheckpoint(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID int64, checkpoint time.Time) {
	t.Helper()
	cursor, err := json.Marshal(map[string]any{
		"page": 1, "method": "flickr.contacts.getList", "checkpoint_at": checkpoint.Unix(),
		"params": map[string]string{"user_id": "", "per_page": "500"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE work_items SET cursor=jsonb_set($2::jsonb,'{params,user_id}',to_jsonb(subject_key)) WHERE id=$1`, itemID, cursor); err != nil {
		t.Fatal(err)
	}
}

func TestContactsRefreshRemovesMissingRowsOnlyAfterFinalPage(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	keyID := seedOwnerKey(t, ctx, pool, userID, "")
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	account := "refresh-acct-" + suffix
	oldContact := "refresh-old-" + suffix
	firstContact := "refresh-first-" + suffix
	lastContact := "refresh-last-" + suffix
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM contacts WHERE person_nsid=$1 OR contact_nsid=ANY($2)`, account, []string{oldContact, firstContact, lastContact})
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=ANY($1)`, []string{account, oldContact, firstContact, lastContact})
		_, _ = pool.Exec(context.Background(), `DELETE FROM flickr_accounts WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM api_keys WHERE id=$1`, keyID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1`, userID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,'account','{}','{}','ok')`, userID, keyID, account); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,'account'),($2,'old'),($3,'first'),($4,'last')`, account, oldContact, firstContact, lastContact); err != nil {
		t.Fatal(err)
	}
	checkpoint := time.Now().UTC().Truncate(time.Second)
	if _, err := pool.Exec(ctx, `INSERT INTO contacts(person_nsid,contact_nsid,seen_at) VALUES($1,$2,$3)`, account, oldContact, checkpoint.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	flickr := &scriptedContactsFlickr{responses: []contactsResponse{
		{body: json.RawMessage(fmt.Sprintf(`{"contacts":{"pages":2,"contact":[{"nsid":%q,"username":"first"}]}}`, firstContact))},
		{body: json.RawMessage(fmt.Sprintf(`{"contacts":{"pages":2,"contact":[{"nsid":%q,"username":"last"}]}}`, lastContact))},
	}}
	engine := contactsRefreshEngine(t, pool, flickr)
	itemID := insertPhotosFavoritesWork(t, ctx, pool, userID, "contacts", account, 0, facets.Contacts{})
	setContactsCheckpoint(t, ctx, pool, itemID, checkpoint)
	if err := engine.Handle(ctx, itemID); err != nil {
		t.Fatalf("first contacts page: %v", err)
	}
	if got := count(t, ctx, pool, `SELECT count(*) FROM contacts WHERE person_nsid=$1 AND contact_nsid=$2`, account, oldContact); got != 1 {
		t.Fatalf("old relationship removed before final page: count=%d", got)
	}
	if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready' WHERE id=$1`, itemID); err != nil {
		t.Fatal(err)
	}
	if err := engine.Handle(ctx, itemID); err != nil {
		t.Fatalf("final contacts page: %v", err)
	}
	if got := count(t, ctx, pool, `SELECT count(*) FROM contacts WHERE person_nsid=$1 AND contact_nsid=$2`, account, oldContact); got != 0 {
		t.Fatalf("old relationship remains after successful refresh: count=%d", got)
	}
	if got := count(t, ctx, pool, `SELECT count(*) FROM contacts WHERE person_nsid=$1 AND contact_nsid=ANY($2) AND seen_at=$3`, account, []string{firstContact, lastContact}, checkpoint); got != 2 {
		t.Fatalf("refreshed relationships count=%d, want 2", got)
	}
}

func TestContactsRefreshFailureRetainsExistingRows(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	keyID := seedOwnerKey(t, ctx, pool, userID, "")
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	account := "failed-refresh-acct-" + suffix
	oldContact := "failed-refresh-old-" + suffix
	newContact := "failed-refresh-new-" + suffix
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM contacts WHERE person_nsid=$1 OR contact_nsid=ANY($2)`, account, []string{oldContact, newContact})
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=ANY($1)`, []string{account, oldContact, newContact})
		_, _ = pool.Exec(context.Background(), `DELETE FROM flickr_accounts WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM api_keys WHERE id=$1`, keyID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1`, userID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,'account','{}','{}','ok')`, userID, keyID, account); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,'account'),($2,'old'),($3,'new')`, account, oldContact, newContact); err != nil {
		t.Fatal(err)
	}
	checkpoint := time.Now().UTC().Truncate(time.Second)
	if _, err := pool.Exec(ctx, `INSERT INTO contacts(person_nsid,contact_nsid,seen_at) VALUES($1,$2,$3)`, account, oldContact, checkpoint.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	flickr := &scriptedContactsFlickr{responses: []contactsResponse{
		{body: json.RawMessage(fmt.Sprintf(`{"contacts":{"pages":2,"contact":[{"nsid":%q,"username":"new"}]}}`, newContact))},
		{err: errors.New("network unavailable")},
	}}
	engine := contactsRefreshEngine(t, pool, flickr)
	itemID := insertPhotosFavoritesWork(t, ctx, pool, userID, "contacts", account, 0, facets.Contacts{})
	setContactsCheckpoint(t, ctx, pool, itemID, checkpoint)
	if err := engine.Handle(ctx, itemID); err != nil {
		t.Fatalf("first contacts page: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready' WHERE id=$1`, itemID); err != nil {
		t.Fatal(err)
	}
	_ = engine.Handle(ctx, itemID)
	if got := count(t, ctx, pool, `SELECT count(*) FROM contacts WHERE person_nsid=$1 AND contact_nsid=$2`, account, oldContact); got != 1 {
		t.Fatalf("existing relationship removed after incomplete refresh: count=%d", got)
	}
}

func (f *countingContactsFlickr) Call(_ context.Context, request crawlapp.Request) (json.RawMessage, error) {
	if request.Method != "flickr.contacts.getList" {
		return nil, fmt.Errorf("unexpected method %s", request.Method)
	}
	f.calls.Add(1)
	return f.body, nil
}

func TestContactsCrawlWithoutPreInsertedAccountPerson(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	account := "ensure-acct-" + suffix
	contact := "ensure-contact-" + suffix
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM contacts WHERE person_nsid=$1 OR contact_nsid=$2`, account, contact)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=ANY($1)`, []string{account, contact})
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_interests WHERE user_id=$1`, userID)
	})
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM people WHERE nsid=$1)`, account).Scan(&exists); err != nil || exists {
		t.Fatalf("account people row must be absent before crawl: exists=%v err=%v", exists, err)
	}
	payload := json.RawMessage(fmt.Sprintf(
		`{"contacts":{"pages":1,"contact":[{"nsid":%q,"username":"friend","realname":"Friend","friend":"1"}]}}`,
		contact,
	))
	flickr := &countingContactsFlickr{body: payload}
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
	if err := registry.Register(facets.Contacts{}); err != nil {
		t.Fatal(err)
	}
	engine := &crawlapp.Engine{
		Registry: registry, Store: store, Units: store, Keys: intKeys{}, Governor: intGovernor{},
		Flickr: flickr, Settings: photosFavoritesSettings{maxDepth: 2},
	}
	itemID := insertPhotosFavoritesWork(t, ctx, pool, userID, "contacts", account, 0, facets.Contacts{})
	setContactsCheckpoint(t, ctx, pool, itemID, time.Now().UTC().Truncate(time.Second))
	if err := engine.Handle(ctx, itemID); err != nil {
		t.Fatalf("crawl without account people row: %v", err)
	}
	var username, realname string
	if err := pool.QueryRow(ctx, `SELECT username, realname FROM people WHERE nsid=$1`, account).Scan(&username, &realname); err != nil {
		t.Fatalf("subject people row missing: %v", err)
	}
	if username != account || realname != "" {
		t.Fatalf("placeholder subject = (%q,%q), want username=nsid and empty realname", username, realname)
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM contacts WHERE person_nsid=$1 AND contact_nsid=$2`, account, contact).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("contact rows=%d err=%v", rows, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE people SET username='real-acct', realname='Real Account' WHERE nsid=$1`, account); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready', attempts=0 WHERE id=$1`, itemID); err != nil {
		t.Fatal(err)
	}
	if err := engine.Handle(ctx, itemID); err != nil {
		t.Fatalf("recrawl with existing subject: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT username, realname FROM people WHERE nsid=$1`, account).Scan(&username, &realname); err != nil {
		t.Fatal(err)
	}
	if username != "real-acct" || realname != "Real Account" {
		t.Fatalf("ensure overwrote real identity: (%q,%q)", username, realname)
	}
	if flickr.calls.Load() != 2 {
		t.Fatalf("flickr calls = %d, want 2", flickr.calls.Load())
	}
}

func TestContactsStoredIdempotent(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	account := "contacts-acct-" + suffix
	contactA := "contacts-a-" + suffix
	contactB := "contacts-b-" + suffix
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM contacts WHERE person_nsid=$1 OR contact_nsid=ANY($2)`, account, []string{contactA, contactB})
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=ANY($1)`, []string{account, contactA, contactB})
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM crawl_interests WHERE user_id=$1`, userID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,'acct')`, account); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(fmt.Sprintf(`{"contacts":{"pages":1,"contact":[{"nsid":%q,"username":"a","friend":"1"},{"nsid":%q,"username":"b","friend":0}]}}`, contactA, contactB))
	flickr := &countingContactsFlickr{body: payload}
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
	if err := registry.Register(facets.Contacts{}); err != nil {
		t.Fatal(err)
	}
	engine := &crawlapp.Engine{
		Registry: registry, Store: store, Units: store, Keys: intKeys{}, Governor: intGovernor{},
		Flickr: flickr, Settings: photosFavoritesSettings{maxDepth: 2},
	}
	itemID := insertPhotosFavoritesWork(t, ctx, pool, userID, "contacts", account, 0, facets.Contacts{})
	setContactsCheckpoint(t, ctx, pool, itemID, time.Now().UTC().Truncate(time.Second))
	if err := engine.Handle(ctx, itemID); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM contacts WHERE person_nsid=$1`, account).Scan(&rows); err != nil || rows != 2 {
		t.Fatalf("first store rows=%d err=%v", rows, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready', attempts=0 WHERE id=$1`, itemID); err != nil {
		t.Fatal(err)
	}
	if err := engine.Handle(ctx, itemID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM contacts WHERE person_nsid=$1`, account).Scan(&rows); err != nil || rows != 2 {
		t.Fatalf("second store rows=%d err=%v", rows, err)
	}
	if flickr.calls.Load() != 2 {
		t.Fatalf("flickr calls = %d, want 2", flickr.calls.Load())
	}
}

func TestListContactsFromStoredRelationships(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	account := "list-acct-" + suffix
	contact := "list-contact-" + suffix
	keyID := seedOwnerKey(t, ctx, pool, userID, "")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM contacts WHERE person_nsid=$1 OR contact_nsid=$2`, account, contact)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=ANY($1)`, []string{account, contact})
		_, _ = pool.Exec(context.Background(), `DELETE FROM flickr_accounts WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM api_keys WHERE id=$1`, keyID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username,realname,photos_count) VALUES($1,'acct','Account',0),($2,'friend','Friend',3)`, account, contact); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,'acct','{}','{}','ok')`, userID, keyID, account); err != nil {
		t.Fatal(err)
	}
	repo, err := catalogpg.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertContacts(ctx, []catalogdomain.Contact{{PersonNSID: account, ContactNSID: contact}}); err != nil {
		t.Fatal(err)
	}
	reads := catalogapp.ReadService{Repository: repo}
	page, next, err := reads.ListContacts(ctx, userID, "", 50)
	if err != nil || next != "" || len(page.Items) != 1 {
		t.Fatalf("page=%+v next=%q err=%v", page, next, err)
	}
	if item := page.Items[0]; item.NSID != contact || item.Username != "friend" || item.Realname != "Friend" || item.PhotosCount != 3 || item.AccountNSID != account {
		t.Fatalf("contact = %+v", item)
	}
}

func TestContactsCalledOncePerIntervalPerAccount(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	userID := insertInterestTestUser(t, ctx, pool)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	account := "interval-acct-" + suffix
	keyID := seedOwnerKey(t, ctx, pool, userID, "")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM flickr_accounts WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM api_keys WHERE id=$1`, keyID)
	})
	if _, err := pool.Exec(ctx, `UPDATE api_keys SET key_public=$2 WHERE id=$1`, keyID, "interval-"+suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,'acct','{}','{}','ok')`, userID, keyID, account); err != nil {
		t.Fatal(err)
	}
	repo, err := NewCycleRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	queued, err := repo.ScheduleContacts(ctx, userID, now, time.Hour)
	if err != nil || !queued {
		t.Fatalf("first schedule = %v, %v", queued, err)
	}
	var state string
	var updated time.Time
	if err := pool.QueryRow(ctx, `SELECT state, updated_at FROM work_items WHERE user_id=$1 AND kind='contacts' AND subject_key=$2`, userID, account).Scan(&state, &updated); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE work_items SET state='done', updated_at=$2 WHERE user_id=$1 AND kind='contacts'`, userID, now); err != nil {
		t.Fatal(err)
	}
	queued, err = repo.ScheduleContacts(ctx, userID, now.Add(30*time.Minute), time.Hour)
	if err != nil || queued {
		t.Fatalf("within interval schedule = %v, %v; want no reopen", queued, err)
	}
	queued, err = repo.ScheduleContacts(ctx, userID, now.Add(2*time.Hour), time.Hour)
	if err != nil || !queued {
		t.Fatalf("after interval schedule = %v, %v; want reopen", queued, err)
	}
	if err := pool.QueryRow(ctx, `SELECT state FROM work_items WHERE user_id=$1 AND kind='contacts' AND subject_key=$2`, userID, account).Scan(&state); err != nil || state != "pending" {
		t.Fatalf("reopened state = %q err=%v", state, err)
	}
}
