package roles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	redisv9 "github.com/redis/go-redis/v9"

	catalogpg "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	catalogapp "github.com/jooservices/xflickr/internal/catalog/app"
	catalogdomain "github.com/jooservices/xflickr/internal/catalog/domain"
	crawlapp "github.com/jooservices/xflickr/internal/crawl/app"
	crawldomain "github.com/jooservices/xflickr/internal/crawl/domain"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	sharingapp "github.com/jooservices/xflickr/internal/sharing/app"
	sharingdomain "github.com/jooservices/xflickr/internal/sharing/domain"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

type observationKeys int64

func (k observationKeys) Choose(context.Context, string, crawldomain.Lane) (int64, error) {
	return int64(k), nil
}

type (
	observationGovernor struct{}
	observationPermit   struct{}
)

func (observationPermit) Done(error) error { return nil }
func (observationGovernor) Acquire(context.Context, int64, crawldomain.Lane) (crawlapp.Permit, time.Duration, error) {
	return observationPermit{}, 0, nil
}

type crashObservationUnit struct{ base crawlapp.UnitOfWork }

func (u crashObservationUnit) Do(ctx context.Context, fn func(crawlapp.Writers) error) error {
	return u.base.Do(ctx, func(w crawlapp.Writers) error {
		if err := fn(w); err != nil {
			return err
		}
		return errors.New("crash before commit")
	})
}

func TestCrawlObservationsAtomicReplayAndVisibility(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("integration gate not enabled")
	}
	cfg := crawlIntegrationConfig(t)
	if cfg.DB.Name == "xflickr_test" {
		t.Fatal("worktree database required")
	}
	pool := dbtest.Open(t, cfg.DB)
	if _, err := BackfillObservations(t.Context(), ObservationBackfillOptions{}); err == nil {
		t.Fatal("missing explicit IDs accepted")
	}
	ctx := t.Context()
	users, keys, items, accounts := make([]int64, 3), make([]int64, 3), make([]int64, 3), make([]int64, 3)
	people := make([]string, 3)
	ids := []int64{770000000000330001, 770000000000330002, 770000000000330003, 770000000000330004}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id=ANY($1)`, ids)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=ANY($1)`, people)
	})
	for i := range users {
		users[i], keys[i], items[i], people[i] = seedCrawlFixture(t, ctx, pool, cfg, "ready", nil)
		if err := pool.QueryRow(ctx, `SELECT id FROM flickr_accounts WHERE user_id=$1`, users[i]).Scan(&accounts[i]); err != nil {
			t.Fatal(err)
		}
	}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		index := -1
		for i, p := range people {
			if r.Form.Get("user_id") == p {
				index = i
			}
		}
		if index < 0 {
			t.Error("unexpected crawl subject")
			w.WriteHeader(500)
			return
		}
		if r.Form.Get("api_key") != strings.TrimSuffix(people[index], "@N00") {
			t.Error("request used another user's key")
		}
		visibility := 1
		if index == 0 {
			visibility = 0
		}
		photo := fmt.Sprintf(`{"id":"%d","owner":%q,"ownername":"Observed","title":"Crawled","lastupdate":"1700000100","dateupload":"1700000000","ispublic":%d}`, ids[index], people[index], visibility)
		extra := ""
		if index == 1 {
			extra = fmt.Sprintf(`,{"id":"%d","owner":%q,"ownername":"Observed","lastupdate":"1700000100","dateupload":"1700000000","ispublic":0}`, ids[3], people[index])
		}
		_, _ = fmt.Fprintf(w, `{"stat":"ok","photos":{"pages":1,"photo":[%s,%s%s]}}`, photo, photo, extra)
	}))
	defer fake.Close()
	redisClient := redisv9.NewClient(&redisv9.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	t.Cleanup(func() { _ = redisClient.Close() })
	engine, err := newCrawlEngine(pool, redisClient, cfg, flickrclient.NewClient(fake.URL, nil))
	if err != nil {
		t.Fatal(err)
	}
	// Suggest another user's key; production resolution must replace it before
	// signing and quota acquisition, without attributing anything to that user.
	engine.Keys = observationKeys(keys[1])
	engine.Governor = observationGovernor{}
	base := engine.Units
	for i := range users {
		cursor, _ := json.Marshal(crawldomain.Cursor{Page: 1, Method: "flickr.people.getPhotos", Params: map[string]string{"user_id": people[i]}})
		if _, err = pool.Exec(ctx, `UPDATE work_items SET kind='photos',cursor=$2 WHERE id=$1`, items[i], cursor); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			engine.Units = crashObservationUnit{base}
			if err = engine.Handle(ctx, items[i]); err == nil {
				t.Fatal("expected crash")
			}
			var n int
			if err = pool.QueryRow(ctx, `SELECT count(*) FROM observations WHERE user_id=$1`, users[i]).Scan(&n); err != nil || n != 0 {
				t.Fatalf("observations survived rollback: %d %v", n, err)
			}
			if err = pool.QueryRow(ctx, `SELECT count(*) FROM photos WHERE id=$1`, ids[i]).Scan(&n); err != nil || n != 0 {
				t.Fatalf("photo survived rollback: %d %v", n, err)
			}
			engine.Units = base
		}
		if err = engine.Handle(ctx, items[i]); err != nil {
			t.Fatal(err)
		}
		var first time.Time
		if err = pool.QueryRow(ctx, `SELECT first_seen_at FROM observations WHERE user_id=$1 AND flickr_account_id=$2 AND record_kind='photo' AND record_id=$3`, users[i], accounts[i], fmt.Sprint(ids[i])).Scan(&first); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE work_items SET state='ready',cursor=$2 WHERE id=$1`, items[i], cursor); err != nil {
			t.Fatal(err)
		}
		if err = engine.Handle(ctx, items[i]); err != nil {
			t.Fatal(err)
		}
		var same bool
		if err = pool.QueryRow(ctx, `SELECT first_seen_at=$4 FROM observations WHERE user_id=$1 AND flickr_account_id=$2 AND record_kind='photo' AND record_id=$3`, users[i], accounts[i], fmt.Sprint(ids[i]), first).Scan(&same); err != nil || !same {
			t.Fatalf("first seen changed: %v %v", same, err)
		}
		var n int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM observations WHERE user_id=$1`, users[i]).Scan(&n); err != nil {
			t.Fatal(err)
		}
		want := 2
		if i == 1 {
			want = 3
		}
		if n != want {
			t.Fatalf("observations=%d want %d", n, want)
		}
	}
	if _, err = pool.Exec(ctx, `UPDATE sharing_settings SET enabled=true WHERE user_id=$1`, users[1]); err != nil {
		t.Fatal(err)
	}
	repo, _ := catalogpg.New(pool)
	filter := sharingapp.Filter(sharingdomain.User{ID: users[0]}).SQL
	for i, id := range ids {
		_, found, readErr := repo.GetPhoto(ctx, users[0], id, filter)
		want := i == 0 || i == 1
		if readErr != nil || found != want {
			t.Fatalf("photo=%d visible=%v want=%v err=%v", id, found, want, readErr)
		}
	}
	page, err := repo.ListPeople(ctx, users[0], filter, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range people {
		found := false
		for _, row := range page.Items {
			if row.NSID == p {
				found = true
			}
		}
		if found != (i < 2) {
			t.Fatalf("person %s visible=%v", p, found)
		}
	}
	if _, err = pool.Exec(ctx, `UPDATE sharing_settings SET enabled=false WHERE user_id=$1`, users[1]); err != nil {
		t.Fatal(err)
	}
	_, found, err := repo.GetPhoto(ctx, users[0], ids[1], filter)
	if err != nil || found {
		t.Fatalf("opt-out visible=%v err=%v", found, err)
	}
	var _ catalogapp.ReadRepository = repo
}

func TestResolveContactsAccountMatchesUserAndSubject(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("integration gate not enabled")
	}
	cfg := crawlIntegrationConfig(t)
	if cfg.DB.Name == "xflickr_test" {
		t.Fatal("worktree database required")
	}
	pool := dbtest.Open(t, cfg.DB)
	ctx := t.Context()
	suffix := fmt.Sprintf("resolve-contacts-%d", time.Now().UnixNano())
	var userID, otherUserID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration','user') RETURNING id`, suffix+"-one@example.invalid").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration','user') RETURNING id`, suffix+"-two@example.invalid").Scan(&otherUserID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=ANY($1)`, []int64{userID, otherUserID})
	})
	insertKey := func(owner int64, label string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst,status) VALUES($1,$2,$3,'{}',3600,10,10,'valid') RETURNING id`, owner, label, suffix+"-"+label).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	sharedKey := insertKey(userID, "shared")
	separateKey := insertKey(userID, "separate")
	otherKey := insertKey(otherUserID, "other")
	insertAccount := func(owner, key int64, nsid string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,$3,'{}','{}','ok') RETURNING id`, owner, key, nsid).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	accountA := insertAccount(userID, sharedKey, suffix+"-a")
	accountB := insertAccount(userID, sharedKey, suffix+"-b")
	accountC := insertAccount(userID, separateKey, suffix+"-c")
	otherAccountA := insertAccount(otherUserID, otherKey, suffix+"-a")
	contactA, contactB := suffix+"-contact-a", suffix+"-contact-b"
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,$1),($2,$2),($3,$3),($4,$4)`, suffix+"-a", suffix+"-b", contactA, contactB); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM contacts WHERE person_nsid=ANY($1)`, []string{suffix + "-a", suffix + "-b"})
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=ANY($1)`, []string{suffix + "-a", suffix + "-b", contactA, contactB})
	})
	catalog, err := catalogpg.New(pool)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name        string
		owner       int64
		keyID       int64
		subject     string
		wantKey     int64
		wantAccount int64
		contact     string
	}{
		{name: "shared key first account", owner: userID, keyID: sharedKey, subject: suffix + "-a", wantKey: sharedKey, wantAccount: accountA, contact: contactA},
		{name: "shared key second account", owner: userID, keyID: sharedKey, subject: suffix + "-b", wantKey: sharedKey, wantAccount: accountB, contact: contactB},
		{name: "different key account", owner: userID, keyID: sharedKey, subject: suffix + "-c", wantKey: separateKey, wantAccount: accountC},
		{name: "same NSID under another user", owner: otherUserID, keyID: sharedKey, subject: suffix + "-a", wantKey: otherKey, wantAccount: otherAccountA},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, err := (&crawlFlickr{pool: pool}).Resolve(ctx, crawlapp.Request{
				UserID: test.owner, KeyID: test.keyID, SubjectNSID: test.subject, Facet: "contacts", Method: "flickr.contacts.getList",
				Params: map[string]string{"user_id": test.subject},
			})
			if err != nil {
				t.Fatal(err)
			}
			if request.KeyID != test.wantKey || request.AccountID != test.wantAccount {
				t.Fatalf("resolved key/account = %d/%d, want %d/%d", request.KeyID, request.AccountID, test.wantKey, test.wantAccount)
			}
			if test.contact != "" {
				if err := catalog.UpsertContacts(ctx, []catalogdomain.Contact{{PersonNSID: request.SubjectNSID, ContactNSID: test.contact}}); err != nil {
					t.Fatal(err)
				}
				var stored bool
				if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM contacts WHERE person_nsid=$1 AND contact_nsid=$2)`, test.subject, test.contact).Scan(&stored); err != nil {
					t.Fatal(err)
				}
				if !stored {
					t.Fatalf("contact %q was not stored under account %q", test.contact, test.subject)
				}
			}
		})
	}
}
