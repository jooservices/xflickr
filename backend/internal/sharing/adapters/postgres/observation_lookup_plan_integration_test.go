package postgres_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	catalogdomain "github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	sharingapp "github.com/jooservices/xflickr/internal/sharing/app"
	sharingdomain "github.com/jooservices/xflickr/internal/sharing/domain"
)

func TestObservationLookupUsesRecordIndex(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	tx := beginLookupTransaction(t, ctx)
	fixture := seedLookupFixture(t, ctx, tx)
	planText := explainSharedPhotoLookup(t, ctx, tx, fixture)
	t.Logf("shared photo lookup plan:\n%s", planText)
	indexBytes := lookupIndexSize(t, ctx, tx)
	t.Logf("representative lookup metrics (the configured worktree database, which already holds other data; 400001 observation rows are added in this transaction): index_size_bytes=%d; execution time and buffer I/O are in the plan above", indexBytes)
	if strings.Contains(planText, "Seq Scan on observations") || strings.Contains(planText, "Seq Scan on observations_p") {
		t.Fatalf("photo sharing lookup scans observations sequentially:\n%s", planText)
	}
	if !strings.Contains(planText, "observations_p01_record_lookup_idx") {
		t.Fatalf("photo sharing lookup did not use the p01 record index:\n%s", planText)
	}
}

// lookupFixture is what the lookup plan test needs to know about its rows.
type lookupFixture struct {
	requesterID int64
	photoID     int64
}

// beginLookupTransaction opens the configured worktree database; everything the test writes is rolled back.
func beginLookupTransaction(t *testing.T, ctx context.Context) pgx.Tx {
	t.Helper()
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := dbtest.Open(t, cfg).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	return tx
}

func seedLookupFixture(t *testing.T, ctx context.Context, tx pgx.Tx) lookupFixture {
	t.Helper()
	suffix := fmt.Sprintf("observation-index-%d", time.Now().UnixNano())
	requesterID, contributorID, accountID := seedLookupAccounts(t, ctx, tx, suffix)
	ownerNSID := suffix + "@flickr"
	photoID := seedLookupPhoto(t, ctx, tx, ownerNSID, suffix)
	if _, err := tx.Exec(ctx, `INSERT INTO observations (user_id,flickr_account_id,record_kind,record_id) SELECT $1,$2,'photo','fixture-' || value::text FROM generate_series(1,400000) AS value`, contributorID, accountID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO observations (user_id,flickr_account_id,record_kind,record_id) VALUES ($1,$2,'photo',$3)`, contributorID, accountID, fmt.Sprint(photoID)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `ANALYZE observations`); err != nil {
		t.Fatal(err)
	}
	return lookupFixture{requesterID: requesterID, photoID: photoID}
}

// seedLookupAccounts creates a requester, a contributor who shares, and the contributor's Flickr account.
func seedLookupAccounts(t *testing.T, ctx context.Context, tx pgx.Tx, suffix string) (requesterID, contributorID, accountID int64) {
	t.Helper()
	for _, user := range []struct {
		email string
		id    *int64
	}{
		{email: suffix + "-requester@example.invalid", id: &requesterID},
		{email: suffix + "-contributor@example.invalid", id: &contributorID},
	} {
		if err := tx.QueryRow(ctx, `INSERT INTO users (email,password_hash,role) VALUES ($1,'hash','user') RETURNING id`, user.email).Scan(user.id); err != nil {
			t.Fatal(err)
		}
	}
	var keyID int64
	if err := tx.QueryRow(ctx, `INSERT INTO api_keys (user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES ($1,'test',$2,'{}',100,10,10) RETURNING id`, contributorID, suffix).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO flickr_accounts (user_id,key_id,nsid,username,token_sealed,secret_sealed) VALUES ($1,$2,$3,'test','{}','{}') RETURNING id`, contributorID, keyID, suffix+"@flickr").Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE sharing_settings SET enabled=true WHERE user_id=$1`, contributorID); err != nil {
		t.Fatal(err)
	}
	return requesterID, contributorID, accountID
}

// seedLookupPhoto stores one public photo whose observation routes to partition p01.
func seedLookupPhoto(t *testing.T, ctx context.Context, tx pgx.Tx, ownerNSID, username string) int64 {
	t.Helper()
	catalog, err := postgres.New(tx)
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.UpsertPeople(ctx, []catalogdomain.Person{{NSID: ownerNSID, Username: username}}); err != nil {
		t.Fatal(err)
	}
	var photoID int64
	if err := tx.QueryRow(ctx, `
		SELECT value
		FROM generate_series(1, 100000) AS value
		WHERE satisfies_hash_partition('observations'::regclass, 16, 1, value::text)
		LIMIT 1
	`).Scan(&photoID); err != nil {
		t.Fatalf("find photo ID routed to observations_p01: %v", err)
	}
	photo := catalogdomain.Photo{
		ID: photoID, OwnerNSID: ownerNSID, LastUpdate: time.Now().UTC(),
		Visibility: catalogdomain.VisibilityPublic, Raw: []byte(`{"id":1}`),
	}
	if err := catalog.UpsertPhotos(ctx, []catalogdomain.Photo{photo}); err != nil {
		t.Fatal(err)
	}
	return photoID
}

// explainSharedPhotoLookup runs the sharing filter for the requester, checks it finds the photo and returns its plan.
func explainSharedPhotoLookup(t *testing.T, ctx context.Context, tx pgx.Tx, fixture lookupFixture) string {
	t.Helper()
	filter := sharingapp.Filter(sharingdomain.User{ID: fixture.requesterID})
	query := `SELECT photos.id FROM photos WHERE photos.id=$2 AND ` + filter.SQL.Expression
	args := append(append([]any(nil), filter.SQL.Args...), fixture.photoID)
	var visibleID int64
	if err := tx.QueryRow(ctx, query, args...).Scan(&visibleID); err != nil {
		t.Fatalf("shared photo lookup: %v", err)
	}
	if visibleID != fixture.photoID {
		t.Fatalf("visible photo id = %d, want %d", visibleID, fixture.photoID)
	}
	return explainQuery(t, ctx, tx, query, args)
}

func explainQuery(t *testing.T, ctx context.Context, tx pgx.Tx, query string, args []any) string {
	t.Helper()
	rows, err := tx.Query(ctx, `EXPLAIN (ANALYZE, BUFFERS) `+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return plan.String()
}

func lookupIndexSize(t *testing.T, ctx context.Context, tx pgx.Tx) int64 {
	t.Helper()
	var indexBytes int64
	if err := tx.QueryRow(ctx, `SELECT pg_relation_size($1::regclass)`, "observations_p01_record_lookup_idx").Scan(&indexBytes); err != nil {
		t.Fatalf("read representative lookup index size: %v", err)
	}
	if indexBytes <= 0 {
		t.Fatal("representative lookup index has no allocated pages")
	}
	return indexBytes
}
