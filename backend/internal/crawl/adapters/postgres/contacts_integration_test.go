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
	crawldomain "github.com/jooservices/xflickr/internal/crawl/domain"
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
	responses      []contactsResponse
	responseByUser map[int64][]contactsResponse
	userCalls      map[int64]int
	calls          int
}

func (f *scriptedContactsFlickr) Call(_ context.Context, request crawlapp.Request) (json.RawMessage, error) {
	if request.Method != "flickr.contacts.getList" {
		return nil, fmt.Errorf("unexpected method %s", request.Method)
	}
	var response contactsResponse
	if responses, ok := f.responseByUser[request.UserID]; ok {
		index := f.userCalls[request.UserID]
		if index >= len(responses) {
			return nil, errors.New("unexpected contacts request")
		}
		response = responses[index]
		f.userCalls[request.UserID] = index + 1
	} else {
		if f.calls >= len(f.responses) {
			return nil, errors.New("unexpected contacts request")
		}
		response = f.responses[f.calls]
	}
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

func cleanupContactsScanState(t *testing.T, pool *pgxpool.Pool, accountNSID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `DELETE FROM contacts_scan_state WHERE account_nsid=$1`, accountNSID); err != nil {
		t.Errorf("delete contacts scan state: %v", err)
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
	emptyContact := "refresh-empty-" + suffix
	t.Cleanup(func() {
		cleanupContactsScanState(t, pool, account)
		_, _ = pool.Exec(context.Background(), `DELETE FROM contacts WHERE person_nsid=$1 OR contact_nsid=ANY($2)`, account, []string{oldContact, firstContact, lastContact, emptyContact})
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=ANY($1)`, []string{account, oldContact, firstContact, lastContact, emptyContact})
		_, _ = pool.Exec(context.Background(), `DELETE FROM flickr_accounts WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM api_keys WHERE id=$1`, keyID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1`, userID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,'account','{}','{}','ok')`, userID, keyID, account); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,'account'),($2,'old'),($3,'first'),($4,'last'),($5,'empty')`, account, oldContact, firstContact, lastContact, emptyContact); err != nil {
		t.Fatal(err)
	}
	checkpoint := time.Now().UTC().Truncate(time.Second)
	if _, err := pool.Exec(ctx, `INSERT INTO contacts(person_nsid,contact_nsid,seen_at) VALUES($1,$2,$3)`, account, oldContact, checkpoint.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	flickr := &scriptedContactsFlickr{responses: []contactsResponse{
		{body: json.RawMessage(fmt.Sprintf(`{"contacts":{"pages":2,"contact":[{"nsid":%q,"username":"first"}]}}`, firstContact))},
		{body: json.RawMessage(fmt.Sprintf(`{"contacts":{"pages":2,"contact":[{"nsid":%q,"username":"last"}]}}`, lastContact))},
		{body: json.RawMessage(`{"contacts":{"pages":1,"contact":[]}}`)},
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
	if _, err := pool.Exec(ctx, `INSERT INTO contacts(person_nsid,contact_nsid,seen_at) VALUES($1,$2,$3)`, account, emptyContact, checkpoint); err != nil {
		t.Fatal(err)
	}
	emptyCheckpoint := checkpoint.Add(time.Minute)
	if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready' WHERE id=$1`, itemID); err != nil {
		t.Fatal(err)
	}
	setContactsCheckpoint(t, ctx, pool, itemID, emptyCheckpoint)
	if err := engine.Handle(ctx, itemID); err != nil {
		t.Fatalf("empty contacts refresh: %v", err)
	}
	if got := count(t, ctx, pool, `SELECT count(*) FROM contacts WHERE person_nsid=$1`, account); got != 0 {
		t.Fatalf("empty successful refresh left %d old relationships", got)
	}
}

func TestInterleavedContactsScansKeepTheNewestRelationship(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	firstUserID := insertInterestTestUser(t, ctx, pool)
	secondUserID := insertInterestTestUser(t, ctx, pool)
	firstKeyID := seedOwnerKey(t, ctx, pool, firstUserID, "")
	secondKeyID := seedOwnerKey(t, ctx, pool, secondUserID, "")
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	account := "interleaved-acct-" + suffix
	contact := "interleaved-contact-" + suffix
	t.Cleanup(func() {
		cleanupContactsScanState(t, pool, account)
		if _, err := pool.Exec(context.Background(), `DELETE FROM contacts WHERE person_nsid=$1 OR contact_nsid=$2`, account, contact); err != nil {
			t.Errorf("delete contacts: %v", err)
		}
		if _, err := pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=ANY($1)`, []string{account, contact}); err != nil {
			t.Errorf("delete people: %v", err)
		}
		if _, err := pool.Exec(context.Background(), `DELETE FROM flickr_accounts WHERE user_id=ANY($1)`, []int64{firstUserID, secondUserID}); err != nil {
			t.Errorf("delete Flickr accounts: %v", err)
		}
		if _, err := pool.Exec(context.Background(), `DELETE FROM api_keys WHERE id=ANY($1)`, []int64{firstKeyID, secondKeyID}); err != nil {
			t.Errorf("delete API keys: %v", err)
		}
		if _, err := pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=ANY($1)`, []int64{firstUserID, secondUserID}); err != nil {
			t.Errorf("delete work items: %v", err)
		}
	})
	if _, err := pool.Exec(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status)
		VALUES($1,$2,$3,'first account','{}','{}','ok'),($4,$5,$3,'second account','{}','{}','ok')`,
		firstUserID, firstKeyID, account, secondUserID, secondKeyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,'account'),($2,'contact')`, account, contact); err != nil {
		t.Fatal(err)
	}

	olderCheckpoint := time.Now().UTC().Truncate(time.Second)
	newerCheckpoint := olderCheckpoint.Add(time.Second)
	flickr := &scriptedContactsFlickr{
		responseByUser: map[int64][]contactsResponse{
			firstUserID: {
				{body: json.RawMessage(fmt.Sprintf(`{"contacts":{"pages":2,"contact":[{"nsid":%q}]}}`, contact))},
				{body: json.RawMessage(fmt.Sprintf(`{"contacts":{"pages":2,"contact":[{"nsid":%q}]}}`, contact))},
			},
			secondUserID: {
				{body: json.RawMessage(fmt.Sprintf(`{"contacts":{"pages":2,"contact":[{"nsid":%q}]}}`, contact))},
				{body: json.RawMessage(`{"contacts":{"pages":2,"contact":[]}}`)},
			},
		},
		userCalls: make(map[int64]int),
	}
	engine := contactsRefreshEngine(t, pool, flickr)
	olderItemID := insertPhotosFavoritesWork(t, ctx, pool, firstUserID, "contacts", account, 0, facets.Contacts{})
	newerItemID := insertPhotosFavoritesWork(t, ctx, pool, secondUserID, "contacts", account, 0, facets.Contacts{})
	setContactsCheckpoint(t, ctx, pool, olderItemID, olderCheckpoint)
	setContactsCheckpoint(t, ctx, pool, newerItemID, newerCheckpoint)

	if err := engine.Handle(ctx, olderItemID); err != nil {
		t.Fatalf("older contacts page one: %v", err)
	}
	if err := engine.Handle(ctx, newerItemID); err != nil {
		t.Fatalf("newer contacts page one: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready' WHERE id=$1`, olderItemID); err != nil {
		t.Fatal(err)
	}
	if err := engine.Handle(ctx, olderItemID); err != nil {
		t.Fatalf("older contacts page after newer scan: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready' WHERE id=$1`, newerItemID); err != nil {
		t.Fatal(err)
	}
	if err := engine.Handle(ctx, newerItemID); err != nil {
		t.Fatalf("newer contacts final page: %v", err)
	}

	if got := count(t, ctx, pool, `SELECT count(*) FROM contacts WHERE person_nsid=$1 AND contact_nsid=$2`, account, contact); got != 1 {
		t.Fatalf("contact still present in newest scan was reconciled away: count=%d", got)
	}
	if got := count(t, ctx, pool, `SELECT count(*) FROM contacts WHERE person_nsid=$1 AND contact_nsid=$2 AND seen_at=$3`, account, contact, newerCheckpoint); got != 1 {
		t.Fatalf("contact seen_at was not kept at newest checkpoint: count=%d", got)
	}
	for _, userID := range []int64{firstUserID, secondUserID} {
		var depth int
		if err := pool.QueryRow(ctx, `SELECT depth FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2`, userID, contact).Scan(&depth); err != nil {
			t.Fatalf("read contact interest for user %d: %v", userID, err)
		}
		if depth != 1 {
			t.Errorf("contact interest for user %d has depth %d, want 1", userID, depth)
		}
	}
	if flickr.calls != 3 || flickr.userCalls[firstUserID] != 1 || flickr.userCalls[secondUserID] != 2 {
		t.Fatalf("Flickr calls = total:%d older:%d newer:%d; want only one older and two newer pages", flickr.calls, flickr.userCalls[firstUserID], flickr.userCalls[secondUserID])
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
		cleanupContactsScanState(t, pool, account)
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
		{body: json.RawMessage(`{"contacts":{"pages":1,"contact":[{"username":"missing-id"}]}}`)},
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
	if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready' WHERE id=$1`, itemID); err != nil {
		t.Fatal(err)
	}
	setContactsCheckpoint(t, ctx, pool, itemID, checkpoint)
	if err := engine.Handle(ctx, itemID); err != nil {
		t.Fatalf("malformed contacts page should be deferred for retry: %v", err)
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM work_items WHERE id=$1`, itemID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state == "done" {
		t.Fatal("contacts page with a missing NSID completed reconciliation")
	}
	if got := count(t, ctx, pool, `SELECT count(*) FROM contacts WHERE person_nsid=$1 AND contact_nsid=$2`, account, oldContact); got != 1 {
		t.Fatalf("existing relationship removed after malformed contact page: count=%d", got)
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
		cleanupContactsScanState(t, pool, account)
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
		cleanupContactsScanState(t, pool, account)
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
	if item := page.Items[0]; item.NSID != contact || item.Username != "friend" || item.Realname != "Friend" || item.AccountNSID != account {
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

func TestContactsScheduleRequiresUsableCredentialsAndReassignsWork(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	firstUserID := insertInterestTestUser(t, ctx, pool)
	secondUserID := insertInterestTestUser(t, ctx, pool)
	firstKeyID := seedOwnerKey(t, ctx, pool, firstUserID, "")
	secondKeyID := seedOwnerKey(t, ctx, pool, secondUserID, "")
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	account := "credential-refresh-acct-" + suffix
	if _, err := pool.Exec(ctx, `UPDATE api_keys SET status='invalid' WHERE id=$1`, firstKeyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status)
		VALUES($1,$2,$3,'first account','{}','{}','ok'),($4,$5,$3,'second account','{}','{}','ok')`,
		firstUserID, firstKeyID, account, secondUserID, secondKeyID); err != nil {
		t.Fatal(err)
	}
	repository, err := NewCycleRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	queued, err := repository.ScheduleContacts(ctx, firstUserID, now, time.Hour)
	if err != nil || queued {
		t.Fatalf("schedule with invalid credentials = %v, %v; want no work", queued, err)
	}
	if got := count(t, ctx, pool, `SELECT count(*) FROM work_items WHERE kind='contacts' AND subject_key=$1`, account); got != 0 {
		t.Fatalf("contacts work items with only invalid credentials = %d, want 0", got)
	}

	cursor := []byte(`{"page":3,"method":"flickr.contacts.getList","params":{"user_id":"` + account + `","per_page":"500"}}`)
	var itemID int64
	if err := pool.QueryRow(ctx, `INSERT INTO work_items(kind,subject_key,user_id,key_id,state,cursor,last_error)
		VALUES('contacts',$1,$2,$3,'pending',$4,'no eligible key') RETURNING id`, account, firstUserID, firstKeyID, cursor).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	queued, err = repository.ScheduleContacts(ctx, secondUserID, now.Add(time.Minute), time.Hour)
	if err != nil || !queued {
		t.Fatalf("reassign to valid credentials = %v, %v; want reassignment", queued, err)
	}
	if got := count(t, ctx, pool, `SELECT count(*) FROM work_items WHERE kind='contacts' AND subject_key=$1`, account); got != 1 {
		t.Fatalf("contacts work items after reassignment = %d, want 1", got)
	}
	var gotUserID, gotKeyID int64
	var state, lastError string
	var cursorBytes []byte
	if err := pool.QueryRow(ctx, `SELECT user_id,key_id,state,cursor,last_error FROM work_items WHERE id=$1`, itemID).
		Scan(&gotUserID, &gotKeyID, &state, &cursorBytes, &lastError); err != nil {
		t.Fatal(err)
	}
	gotCursor, err := crawldomain.ParseCursor(cursorBytes)
	if err != nil {
		t.Fatal(err)
	}
	if gotUserID != secondUserID || gotKeyID != secondKeyID || state != "pending" || gotCursor.Page != 3 ||
		gotCursor.Method != "flickr.contacts.getList" || gotCursor.Params["user_id"] != account ||
		gotCursor.Params["per_page"] != "500" || lastError != "no eligible key" {
		t.Fatalf("reassigned item user=%d key=%d state=%q cursor=%+v error=%q", gotUserID, gotKeyID, state, gotCursor, lastError)
	}
}

func TestContactsSharedNSIDUsesOneRefreshAcrossUsers(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	firstUserID := insertInterestTestUser(t, ctx, pool)
	secondUserID := insertInterestTestUser(t, ctx, pool)
	firstKeyID := seedOwnerKey(t, ctx, pool, firstUserID, "")
	secondKeyID := seedOwnerKey(t, ctx, pool, secondUserID, "")
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	account := "shared-refresh-acct-" + suffix
	contact := "shared-refresh-contact-" + suffix
	t.Cleanup(func() {
		cleanupContactsScanState(t, pool, account)
		_, _ = pool.Exec(context.Background(), `DELETE FROM contacts WHERE person_nsid=$1 OR contact_nsid=$2`, account, contact)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=ANY($1)`, []string{account, contact})
	})
	if _, err := pool.Exec(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status)
		VALUES($1,$2,$3,'first account','{}','{}','ok'),($4,$5,$3,'second account','{}','{}','ok')`,
		firstUserID, firstKeyID, account, secondUserID, secondKeyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,'account'),($2,'contact')`, account, contact); err != nil {
		t.Fatal(err)
	}
	repository, err := NewCycleRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	queued, err := repository.ScheduleContacts(ctx, firstUserID, now, time.Hour)
	if err != nil || !queued {
		t.Fatalf("first user's schedule = %v, %v; want queued", queued, err)
	}
	queued, err = repository.ScheduleContacts(ctx, secondUserID, now, time.Hour)
	if err != nil || queued {
		t.Fatalf("second user's schedule = %v, %v; want the shared refresh to remain queued once", queued, err)
	}
	if got := count(t, ctx, pool, `SELECT count(*) FROM work_items WHERE kind='contacts' AND subject_key=$1`, account); got != 1 {
		t.Fatalf("queued contact work items = %d, want one for the shared NSID", got)
	}
	var itemID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM work_items WHERE kind='contacts' AND subject_key=$1`, account).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	flickr := &scriptedContactsFlickr{responses: []contactsResponse{
		{body: json.RawMessage(fmt.Sprintf(`{"contacts":{"pages":2,"contact":[{"nsid":%q}]}}`, contact))},
		{body: json.RawMessage(fmt.Sprintf(`{"contacts":{"pages":2,"contact":[{"nsid":%q}]}}`, contact))},
	}}
	engine := contactsRefreshEngine(t, pool, flickr)
	for page := 1; page <= 2; page++ {
		if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready' WHERE id=$1`, itemID); err != nil {
			t.Fatal(err)
		}
		if err := engine.Handle(ctx, itemID); err != nil {
			t.Fatalf("handle shared contacts page %d: %v", page, err)
		}
	}
	if flickr.calls != 2 {
		t.Fatalf("Flickr calls = %d, want each of two pages exactly once", flickr.calls)
	}
	for _, userID := range []int64{firstUserID, secondUserID} {
		var depth int
		if err := pool.QueryRow(ctx, `SELECT depth FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2`, userID, contact).Scan(&depth); err != nil {
			t.Fatalf("read contact interest for user %d: %v", userID, err)
		}
		if depth != 1 {
			t.Errorf("contact interest for user %d has depth %d, want 1", userID, depth)
		}
	}
	queued, err = repository.ScheduleContacts(ctx, firstUserID, now.Add(30*time.Minute), time.Hour)
	if err != nil || queued {
		t.Fatalf("first user's within-interval schedule = %v, %v; want no refresh", queued, err)
	}
	queued, err = repository.ScheduleContacts(ctx, secondUserID, now.Add(30*time.Minute), time.Hour)
	if err != nil || queued {
		t.Fatalf("second user's within-interval schedule = %v, %v; want no refresh", queued, err)
	}
	queued, err = repository.ScheduleContacts(ctx, firstUserID, now.Add(2*time.Hour), time.Hour)
	if err != nil || !queued {
		t.Fatalf("first user's elapsed-interval schedule = %v, %v; want refresh", queued, err)
	}
	queued, err = repository.ScheduleContacts(ctx, secondUserID, now.Add(2*time.Hour), time.Hour)
	if err != nil || queued {
		t.Fatalf("second user's elapsed-interval schedule = %v, %v; want the shared refresh to remain queued once", queued, err)
	}
}
