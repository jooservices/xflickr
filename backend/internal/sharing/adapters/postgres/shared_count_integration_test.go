package postgres_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	catalogpostgres "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	catalogdomain "github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	sharingpostgres "github.com/jooservices/xflickr/internal/sharing/adapters/postgres"
	sharingapp "github.com/jooservices/xflickr/internal/sharing/app"
	sharingdomain "github.com/jooservices/xflickr/internal/sharing/domain"
)

// sharedCountFixture seeds users, Flickr accounts, photos and observations for
// the shared record count. Each helper registers its own cleanup.
type sharedCountFixture struct {
	t      *testing.T
	ctx    context.Context
	pool   *pgxpool.Pool
	prefix string
	base   int64
}

func (f *sharedCountFixture) user(name string) (userID, accountID, secondAccountID int64) {
	f.t.Helper()
	email := fmt.Sprintf("%s-%s@example.invalid", f.prefix, name)
	if err := f.pool.QueryRow(f.ctx, `INSERT INTO users (email,password_hash,role) VALUES ($1,'hash','user') RETURNING id`, email).Scan(&userID); err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() {
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	var keyID int64
	if err := f.pool.QueryRow(f.ctx, `INSERT INTO api_keys (user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES ($1,'test',$2,'{}',100,10,10) RETURNING id`, userID, email).Scan(&keyID); err != nil {
		f.t.Fatal(err)
	}
	for _, nsid := range []string{email + "-a", email + "-b"} {
		var id int64
		if err := f.pool.QueryRow(f.ctx, `INSERT INTO flickr_accounts (user_id,key_id,nsid,username,token_sealed,secret_sealed) VALUES ($1,$2,$3,'test','{}','{}') RETURNING id`, userID, keyID, nsid).Scan(&id); err != nil {
			f.t.Fatal(err)
		}
		if accountID == 0 {
			accountID = id
		} else {
			secondAccountID = id
		}
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE sharing_settings SET enabled=true WHERE user_id=$1`, userID); err != nil {
		f.t.Fatal(err)
	}
	return userID, accountID, secondAccountID
}

// photo inserts a photo owned by ownerNSID and returns its id. Visibility is
// stored as given, so friends, family and private records are seeded too.
func (f *sharedCountFixture) photo(index int64, visibility catalogdomain.Visibility) int64 {
	f.t.Helper()
	id := f.base + index
	catalog, err := catalogpostgres.New(f.pool)
	if err != nil {
		f.t.Fatal(err)
	}
	photo := catalogdomain.Photo{ID: id, OwnerNSID: f.prefix + "@flickr", LastUpdate: time.Now().UTC(), Visibility: visibility, Raw: []byte(`{"id":1}`)}
	if err := catalog.UpsertPhotos(f.ctx, []catalogdomain.Photo{photo}); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *sharedCountFixture) observe(userID, accountID, photoID int64) {
	f.t.Helper()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO observations (user_id,flickr_account_id,record_kind,record_id) VALUES ($1,$2,'photo',$3)`, userID, accountID, fmt.Sprint(photoID)); err != nil {
		f.t.Fatal(err)
	}
}

func sharedCountPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return dbtest.Open(t, cfg)
}

func TestSharedRecordCountIsDistinctPublicAndUserScoped(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := sharedCountPool(t)
	suffix := fmt.Sprintf("shared-count-%d", time.Now().UnixNano())
	fixture := &sharedCountFixture{t: t, ctx: ctx, pool: pool, prefix: suffix, base: time.Now().UnixMicro()}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE owner_nsid=$1`, suffix+"@flickr")
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, suffix+"@flickr")
	})
	catalog, err := catalogpostgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.UpsertPeople(ctx, []catalogdomain.Person{{NSID: suffix + "@flickr", Username: suffix}}); err != nil {
		t.Fatal(err)
	}
	owner, ownerAccount, ownerSecondAccount := fixture.user("owner")
	other, otherAccount, _ := fixture.user("other")
	for i := range int64(3) {
		id := fixture.photo(i, catalogdomain.VisibilityPublic)
		fixture.observe(owner, ownerAccount, id)
		if i == 0 {
			// The same photo seen through a second account of the owner counts once.
			fixture.observe(owner, ownerSecondAccount, id)
		}
	}
	fixture.observe(owner, ownerAccount, fixture.photo(10, catalogdomain.VisibilityFriends))
	fixture.observe(owner, ownerAccount, fixture.photo(11, catalogdomain.VisibilityPrivate))
	for i := range int64(5) {
		fixture.observe(other, otherAccount, fixture.photo(20+i, catalogdomain.VisibilityPublic))
	}
	repo, err := sharingpostgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	read := sharingapp.Read{Repository: repo}
	summary, err := read.Execute(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if summary.SharedRecordCount != 3 || !summary.Setting.Enabled {
		t.Fatalf("owner summary = %+v, want 3 shared records with sharing on", summary)
	}
	otherSummary, err := read.Execute(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	if otherSummary.SharedRecordCount != 5 {
		t.Fatalf("other summary count = %d, want 5", otherSummary.SharedRecordCount)
	}
	if _, err := repo.Set(ctx, sharingdomain.Setting{UserID: owner, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	off, err := read.Execute(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if off.SharedRecordCount != 0 || off.Setting.Enabled {
		t.Fatalf("opted-out summary = %+v, want 0 shared records with sharing off", off)
	}
}
