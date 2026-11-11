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
	"github.com/jooservices/xflickr/internal/sharing/app"
	"github.com/jooservices/xflickr/internal/sharing/domain"
)

const albumPlanFixtureSize = 400_000

type planDB struct {
	tx        pgx.Tx
	plan      []byte
	ownerPlan []byte
}

func (db *planDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return db.tx.Exec(ctx, sql, args...)
}

func (db *planDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.HasPrefix(strings.TrimSpace(sql), "SELECT DISTINCT contacts.contact_nsid") {
		if err := savePlan(ctx, db.tx, sql, args, &db.ownerPlan); err != nil {
			return nil, fmt.Errorf("explain contact album owners: %w", err)
		}
		return db.tx.Query(ctx, "SELECT 1 WHERE false")
	}
	if strings.Contains(sql, "FROM albums a") && strings.Contains(sql, "ORDER BY albums.updated_at") {
		if err := savePlan(ctx, db.tx, sql, args, &db.plan); err != nil {
			return nil, fmt.Errorf("explain contact album list: %w", err)
		}
		return db.tx.Query(ctx, "SELECT 1 WHERE false")
	}
	return db.tx.Query(ctx, sql, args...)
}

func savePlan(ctx context.Context, tx pgx.Tx, sql string, args []any, target *[]byte) error {
	rows, err := tx.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return fmt.Errorf("explain returned no plan")
	}
	values, err := rows.Values()
	if err != nil {
		return err
	}
	if len(values) != 1 {
		return fmt.Errorf("explain returned %d columns, want 1", len(values))
	}
	switch value := values[0].(type) {
	case []byte:
		*target = append((*target)[:0], value...)
	case string:
		*target = append((*target)[:0], value...)
	default:
		*target, err = json.Marshal(value)
		if err != nil {
			return fmt.Errorf("encode plan: %w", err)
		}
	}
	return nil
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
	Index    string        `json:"Index Name"`
	Plans    []explainNode `json:"Plans"`
}

func TestContactAlbumListPlanUsesVisibilityIndexes(t *testing.T) {
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

	userID, observationUserID, observationAccountID, viewerNSID, contactNSID := createPlanIdentity(t, ctx, tx)
	createAlbumPlanFixture(t, ctx, tx, userID, observationUserID, observationAccountID, viewerNSID, contactNSID)
	db := &planDB{tx: tx}
	repository, err := postgres.New(db)
	if err != nil {
		t.Fatal(err)
	}
	predicate := app.Filter(domain.User{ID: userID}).SQL
	if _, err := repository.ListContactAlbums(ctx, userID, "", predicate, nil, 50); err != nil {
		t.Fatal(err)
	}

	listPlan := decodePlan(t, db.plan, "contact album list")
	ownerPlan := decodePlan(t, db.ownerPlan, "contact album owners")
	t.Logf("EXPLAIN (ANALYZE, BUFFERS) on %d albums, photos, memberships, and observations; list: %s; owners: %s", albumPlanFixtureSize, db.plan, db.ownerPlan)
	for _, plan := range []struct {
		name string
		root explainNode
	}{{name: "contact album list", root: listPlan}, {name: "contact album owners", root: ownerPlan}} {
		if relation, nodeType, ok := findSequentialScan(plan.root); ok {
			t.Fatalf("%s plan uses %s on %s", plan.name, nodeType, relation)
		}
	}
	for _, index := range []string{"albums_contact_page_idx", "album_photos_order_idx", "photos_pkey"} {
		if !containsIndex(listPlan, index) {
			t.Fatalf("contact album list plan does not use %s: %s", index, db.plan)
		}
	}
	verifyObservationPartitionPlans(t, ctx, tx)
}

func decodePlan(t *testing.T, raw []byte, name string) explainNode {
	t.Helper()
	var documents []struct {
		Plan explainNode `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &documents); err != nil {
		preview := raw
		if len(preview) > 200 {
			preview = preview[:200]
		}
		t.Fatalf("decode %s plan (%d bytes, prefix %q): %v", name, len(raw), preview, err)
	}
	if len(documents) != 1 {
		t.Fatalf("%s EXPLAIN returned %d plan documents, want 1", name, len(documents))
	}
	return documents[0].Plan
}

func createPlanIdentity(t *testing.T, ctx context.Context, tx pgx.Tx) (int64, int64, int64, string, string) {
	t.Helper()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	viewerNSID, contactNSID := "album-plan-viewer-"+suffix, "album-plan-contact-"+suffix
	var userID, observationUserID, observationKeyID, observationAccountID int64
	if err := tx.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration','user') RETURNING id`, "album-plan-viewer-"+suffix+"@example.invalid").Scan(&userID); err != nil {
		t.Fatalf("create plan user: %v", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration','user') RETURNING id`, "album-plan-observer-"+suffix+"@example.invalid").Scan(&observationUserID); err != nil {
		t.Fatalf("create plan observation user: %v", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,'plan',$2,'{}',1000,10,20) RETURNING id`, observationUserID, "album-plan-observer-key-"+suffix).Scan(&observationKeyID); err != nil {
		t.Fatalf("create plan observation key: %v", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,'plan','{}','{}','ok') RETURNING id`, observationUserID, observationKeyID, "album-plan-observer-"+suffix).Scan(&observationAccountID); err != nil {
		t.Fatalf("create plan observation account: %v", err)
	}
	return userID, observationUserID, observationAccountID, viewerNSID, contactNSID
}

func createAlbumPlanFixture(t *testing.T, ctx context.Context, tx pgx.Tx, userID, observationUserID, observationAccountID int64, viewerNSID, contactNSID string) {
	t.Helper()
	statements := []string{
		`CREATE TEMP TABLE albums (id text PRIMARY KEY, owner_nsid text NOT NULL, title text NOT NULL, updated_at timestamptz) ON COMMIT DROP`,
		`CREATE INDEX albums_contact_page_idx ON albums (owner_nsid, updated_at DESC NULLS LAST, id DESC)`,
		`CREATE TEMP TABLE contacts (person_nsid text NOT NULL, contact_nsid text NOT NULL, PRIMARY KEY(person_nsid, contact_nsid)) ON COMMIT DROP`,
		`CREATE TEMP TABLE flickr_accounts (user_id bigint NOT NULL, nsid text NOT NULL, status text NOT NULL) ON COMMIT DROP`,
		`CREATE UNIQUE INDEX flickr_accounts_user_nsid_idx ON flickr_accounts (user_id, nsid)`,
		`CREATE TEMP TABLE photos (id bigint PRIMARY KEY, tombstoned_at timestamptz, visibility text NOT NULL) ON COMMIT DROP`,
		`CREATE TEMP TABLE album_photos (album_id text NOT NULL, photo_id bigint NOT NULL, position integer NOT NULL) ON COMMIT DROP`,
		`CREATE INDEX album_photos_order_idx ON album_photos (album_id, position, photo_id)`,
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
		args []any
	}{
		{name: "contact account", sql: `INSERT INTO flickr_accounts(user_id,nsid,status) VALUES ($1,$2,'ok')`, args: []any{userID, viewerNSID}},
		{name: "contact", sql: `INSERT INTO contacts(person_nsid,contact_nsid) VALUES ($1,$2)`, args: []any{viewerNSID, contactNSID}},
		{name: "sharing setting", sql: `INSERT INTO sharing_settings(user_id,enabled) VALUES ($1,true)`, args: []any{observationUserID}},
		{name: "albums", sql: `INSERT INTO albums(id,owner_nsid,title,updated_at) SELECT 'album-'||g,$1,'Album '||g,TIMESTAMPTZ '2026-01-01 00:00:00+00' - g * INTERVAL '1 second' FROM generate_series(1,400000) AS g`, args: []any{contactNSID}},
		{name: "photos", sql: `INSERT INTO photos(id,tombstoned_at,visibility) SELECT g,NULL,'public' FROM generate_series(1,400000) AS g`},
		{name: "album photos", sql: `INSERT INTO album_photos(album_id,photo_id,position) SELECT 'album-'||g,g,0 FROM generate_series(1,400000) AS g`},
	}
	for _, fixture := range fixtures {
		if _, err := tx.Exec(ctx, fixture.sql, fixture.args...); err != nil {
			t.Fatalf("insert %s plan fixture: %v", fixture.name, err)
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO observations(user_id,flickr_account_id,record_kind,record_id) SELECT $1,$2,'photo',g::text FROM generate_series(1,400000) AS g`, observationUserID, observationAccountID); err != nil {
		t.Fatalf("insert observations plan fixture: %v", err)
	}
	for _, remainder := range []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15} {
		var recordID string
		if err := tx.QueryRow(ctx, `SELECT ('plan-marker-'||candidate)::text FROM generate_series(1,512) AS candidate WHERE satisfies_hash_partition('observations'::regclass,16,$1,('plan-marker-'||candidate)::text) LIMIT 1`, remainder).Scan(&recordID); err != nil {
			t.Fatalf("find observation id for partition %02d: %v", remainder, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO observations(user_id,flickr_account_id,record_kind,record_id) VALUES($1,$2,'photo',$3)`, observationUserID, observationAccountID, recordID); err != nil {
			t.Fatalf("insert observation partition marker %02d: %v", remainder, err)
		}
	}
	for _, table := range []string{"albums", "contacts", "flickr_accounts", "photos", "album_photos", "observations", "sharing_settings"} {
		if _, err := tx.Exec(ctx, "ANALYZE "+table); err != nil {
			t.Fatalf("analyze %s plan fixture: %v", table, err)
		}
	}
}

func verifyObservationPartitionPlans(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	for remainder := 0; remainder < 16; remainder++ {
		partition := fmt.Sprintf("observations_p%02d", remainder)
		var recordID string
		if err := tx.QueryRow(ctx, "SELECT record_id FROM "+partition+" WHERE record_id LIKE 'plan-marker-%' LIMIT 1").Scan(&recordID); err != nil {
			t.Fatalf("read marker from %s: %v", partition, err)
		}
		var raw []byte
		query := `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) SELECT 1 FROM observations o JOIN sharing_settings ss ON ss.user_id=o.user_id WHERE o.record_kind='photo' AND o.record_id=$1 AND ss.enabled`
		if err := tx.QueryRow(ctx, query, recordID).Scan(&raw); err != nil {
			t.Fatalf("explain lookup on %s: %v", partition, err)
		}
		root := decodePlan(t, raw, partition)
		index := partition + "_kind_record_idx"
		if relation, nodeType, ok := findSequentialScan(root); ok {
			t.Fatalf("lookup plan for %s uses %s on %s: %s", partition, nodeType, relation, raw)
		}
		if !containsIndex(root, index) {
			t.Fatalf("lookup plan for %s does not use %s: %s", partition, index, raw)
		}
	}
}

func findSequentialScan(node explainNode) (string, string, bool) {
	if node.NodeType == "Seq Scan" && isLargeCatalogRelation(node.Relation) {
		return node.Relation, node.NodeType, true
	}
	for _, child := range node.Plans {
		if relation, nodeType, ok := findSequentialScan(child); ok {
			return relation, nodeType, true
		}
	}
	return "", "", false
}

func isLargeCatalogRelation(relation string) bool {
	if strings.HasPrefix(relation, "albums_") || strings.HasPrefix(relation, "album_photos_") ||
		strings.HasPrefix(relation, "photos_") || strings.HasPrefix(relation, "observations_") {
		return true
	}
	switch relation {
	case "albums", "album_photos", "photos", "observations":
		return true
	}
	return false
}

func containsIndex(node explainNode, index string) bool {
	if node.Index == index {
		return true
	}
	for _, child := range node.Plans {
		if containsIndex(child, index) {
			return true
		}
	}
	return false
}

func envIntegrationTest() string { return os.Getenv("XFLICKR_INTEGRATION_TEST") }
