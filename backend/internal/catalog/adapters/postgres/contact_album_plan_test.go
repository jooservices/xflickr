//go:build album_plan

package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

const albumPlanFixtureSize = 2_000_000

type planDB struct {
	tx   pgx.Tx
	plan []byte
}

func (db *planDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return db.tx.Exec(ctx, sql, args...)
}

func (db *planDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "FROM albums") && strings.Contains(sql, "ORDER BY albums.updated_at") {
		planSQL := "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) " + sql
		if err := db.tx.QueryRow(ctx, planSQL, args...).Scan(&db.plan); err != nil {
			return nil, fmt.Errorf("explain contact album list: %w", err)
		}
		return db.tx.Query(ctx, "SELECT 1 WHERE false")
	}
	return db.tx.Query(ctx, sql, args...)
}

func (db *planDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return db.tx.QueryRow(ctx, sql, args...)
}

func (db *planDB) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	return db.tx.SendBatch(ctx, batch)
}

type explainNode struct {
	NodeType string        `json:"Node Type"`
	Relation string        `json:"Relation Name"`
	Plans    []explainNode `json:"Plans"`
}

func TestContactAlbumListPlanDoesNotReadPhotosOrObservations(t *testing.T) {
	if got := envIntegrationTest(); got != "1" {
		t.Fatal("run with XFLICKR_INTEGRATION_TEST=1 through docker/scripts/album-list-plan-check")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	pool := dbtest.Open(t, loadCatalogDatabase(t))
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	createAlbumPlanFixture(t, ctx, tx)
	db := &planDB{tx: tx}
	repository, err := postgres.New(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ListContactAlbums(ctx, 42, nil, 50); err != nil {
		t.Fatal(err)
	}

	var documents []struct {
		Plan explainNode `json:"Plan"`
	}
	if err := json.Unmarshal(db.plan, &documents); err != nil {
		t.Fatalf("decode contact album query plan: %v", err)
	}
	if len(documents) != 1 {
		t.Fatalf("EXPLAIN returned %d plan documents, want 1", len(documents))
	}
	t.Logf("EXPLAIN (ANALYZE, BUFFERS) on %d albums and %d photos: %s", 400_000, albumPlanFixtureSize, db.plan)
	if relation, nodeType, ok := findUnboundedPhotoRead(documents[0].Plan); ok {
		t.Fatalf("contact album list plan reads %s with %s", relation, nodeType)
	}
}

func createAlbumPlanFixture(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	statements := []string{
		`CREATE TEMP TABLE albums (id text PRIMARY KEY, owner_nsid text NOT NULL, title text NOT NULL, updated_at timestamptz) ON COMMIT DROP`,
		`CREATE INDEX albums_owner_updated_idx ON albums (owner_nsid, updated_at DESC, id DESC)`,
		`CREATE TEMP TABLE contacts (person_nsid text NOT NULL, contact_nsid text NOT NULL) ON COMMIT DROP`,
		`CREATE INDEX contacts_person_contact_idx ON contacts (person_nsid, contact_nsid)`,
		`CREATE TEMP TABLE flickr_accounts (user_id bigint NOT NULL, nsid text NOT NULL, status text NOT NULL) ON COMMIT DROP`,
		`CREATE INDEX flickr_accounts_user_nsid_idx ON flickr_accounts (user_id, nsid)`,
		`CREATE TEMP TABLE photos (id bigint PRIMARY KEY, tombstoned_at timestamptz, visibility text NOT NULL) ON COMMIT DROP`,
		`CREATE TEMP TABLE album_photos (album_id text NOT NULL, photo_id bigint NOT NULL, position integer NOT NULL) ON COMMIT DROP`,
		`CREATE TEMP TABLE observations (user_id bigint NOT NULL, record_kind text NOT NULL, record_id text NOT NULL) ON COMMIT DROP`,
		`CREATE TEMP TABLE sharing_settings (user_id bigint PRIMARY KEY, enabled boolean NOT NULL) ON COMMIT DROP`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			t.Fatalf("create album plan fixture: %v", err)
		}
	}
	fixtures := []struct {
		name string
		sql  string
	}{
		{name: "account", sql: `INSERT INTO flickr_accounts(user_id,nsid,status) VALUES (42,'account-owner','ok')`},
		{name: "contact", sql: `INSERT INTO contacts(person_nsid,contact_nsid) VALUES ('account-owner','contact-owner')`},
		{name: "albums", sql: `INSERT INTO albums(id,owner_nsid,title,updated_at) SELECT 'album-'||g,'contact-owner','Album '||g,TIMESTAMPTZ '2026-01-01 00:00:00+00' - g * INTERVAL '1 second' FROM generate_series(1,400000) AS g`},
		{name: "photos", sql: `INSERT INTO photos(id,tombstoned_at,visibility) SELECT g,NULL,'public' FROM generate_series(1,2000000) AS g`},
		{name: "album photos", sql: `INSERT INTO album_photos(album_id,photo_id,position) SELECT 'album-'||((g-1)%400000+1),g,0 FROM generate_series(1,2000000) AS g`},
		{name: "observations", sql: `INSERT INTO observations(user_id,record_kind,record_id) SELECT 42,'photo',g::text FROM generate_series(1,2000000) AS g`},
	}
	for _, fixture := range fixtures {
		if _, err := tx.Exec(ctx, fixture.sql); err != nil {
			t.Fatalf("insert %s plan fixture: %v", fixture.name, err)
		}
	}
	for _, table := range []string{"albums", "contacts", "flickr_accounts", "photos", "album_photos", "observations"} {
		if _, err := tx.Exec(ctx, "ANALYZE "+table); err != nil {
			t.Fatalf("analyze %s plan fixture: %v", table, err)
		}
	}
}

func findUnboundedPhotoRead(node explainNode) (string, string, bool) {
	if node.Relation == "photos" || node.Relation == "observations" || node.Relation == "album_photos" {
		return node.Relation, node.NodeType, true
	}
	for _, child := range node.Plans {
		if relation, nodeType, ok := findUnboundedPhotoRead(child); ok {
			return relation, nodeType, true
		}
	}
	return "", "", false
}

func envIntegrationTest() string { return os.Getenv("XFLICKR_INTEGRATION_TEST") }
