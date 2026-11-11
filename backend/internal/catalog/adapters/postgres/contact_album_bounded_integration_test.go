package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	sharingapp "github.com/jooservices/xflickr/internal/sharing/app"
	sharingdomain "github.com/jooservices/xflickr/internal/sharing/domain"
)

func TestContactAlbumVisibilityProbeResumesAfterBoundedPhotoBatch(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's isolated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	pool := dbtest.Open(t, loadCatalogDatabase(t))
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	createBoundedContactAlbumFixture(t, ctx, tx)

	repository, err := postgres.New(tx)
	if err != nil {
		t.Fatal(err)
	}
	userID := int64(42)
	predicate := sharingapp.Filter(sharingdomain.User{ID: userID}).SQL
	page, err := repository.ListContactAlbums(ctx, userID, "", predicate, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 || page.NextCursor == nil {
		t.Fatalf("first page=%+v, want a bounded empty page with a continuation cursor", page)
	}
	page, err = repository.ListContactAlbums(ctx, userID, "", predicate, page.NextCursor, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "long-album" {
		t.Fatalf("resumed page=%+v, want the album after its visible photo is reached", page)
	}
}

func createBoundedContactAlbumFixture(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	statements := []string{
		`CREATE TEMP TABLE albums (id text PRIMARY KEY, owner_nsid text NOT NULL, title text NOT NULL, updated_at timestamptz) ON COMMIT DROP`,
		`CREATE INDEX albums_contact_page_idx ON albums (owner_nsid, updated_at DESC NULLS LAST, id DESC)`,
		`CREATE TEMP TABLE contacts (person_nsid text NOT NULL, contact_nsid text NOT NULL, PRIMARY KEY(person_nsid, contact_nsid)) ON COMMIT DROP`,
		`CREATE TEMP TABLE flickr_accounts (user_id bigint NOT NULL, nsid text NOT NULL, status text NOT NULL) ON COMMIT DROP`,
		`CREATE TEMP TABLE photos (id bigint PRIMARY KEY, tombstoned_at timestamptz, visibility text NOT NULL) ON COMMIT DROP`,
		`CREATE TEMP TABLE album_photos (album_id text NOT NULL, photo_id bigint NOT NULL, position integer NOT NULL) ON COMMIT DROP`,
		`CREATE INDEX album_photos_order_idx ON album_photos (album_id, position, photo_id)`,
		`CREATE TEMP TABLE observations (user_id bigint NOT NULL, record_kind text NOT NULL, record_id text NOT NULL, PRIMARY KEY(user_id, record_kind, record_id)) ON COMMIT DROP`,
		`CREATE TEMP TABLE sharing_settings (user_id bigint PRIMARY KEY, enabled boolean NOT NULL) ON COMMIT DROP`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			t.Fatalf("create contact album fixture: %v", err)
		}
	}
	fixtures := []struct {
		name string
		sql  string
	}{
		{name: "account", sql: `INSERT INTO flickr_accounts(user_id,nsid,status) VALUES(42,'account-owner','ok')`},
		{name: "contact", sql: `INSERT INTO contacts(person_nsid,contact_nsid) VALUES('account-owner','contact-owner')`},
		{name: "album", sql: `INSERT INTO albums(id,owner_nsid,title,updated_at) VALUES('long-album','contact-owner','Long album',now())`},
		{name: "photos", sql: `INSERT INTO photos(id,tombstoned_at,visibility) SELECT g,NULL,'private' FROM generate_series(1,129) AS g`},
		{name: "album photos", sql: `INSERT INTO album_photos(album_id,photo_id,position) SELECT 'long-album',g,g::integer FROM generate_series(1,129) AS g`},
		{name: "owner observations", sql: `INSERT INTO observations(user_id,record_kind,record_id) SELECT 99,'photo',g::text FROM generate_series(1,129) AS g`},
		{name: "viewer observation", sql: `INSERT INTO observations(user_id,record_kind,record_id) VALUES(42,'photo','129')`},
		{name: "sharing setting", sql: `INSERT INTO sharing_settings(user_id,enabled) VALUES(42,false)`},
	}
	for _, fixture := range fixtures {
		if _, err := tx.Exec(ctx, fixture.sql); err != nil {
			t.Fatalf("insert %s fixture: %v", fixture.name, err)
		}
	}
}
