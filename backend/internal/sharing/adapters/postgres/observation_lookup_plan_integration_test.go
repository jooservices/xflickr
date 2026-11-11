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
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	pool := dbtest.Open(t, cfg)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	suffix := fmt.Sprintf("observation-index-%d", time.Now().UnixNano())
	var requesterID, contributorID, keyID, accountID int64
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
	if err := tx.QueryRow(ctx, `INSERT INTO api_keys (user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES ($1,'test',$2,'{}',100,10,10) RETURNING id`, contributorID, suffix).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	ownerNSID := suffix + "@flickr"
	if err := tx.QueryRow(ctx, `INSERT INTO flickr_accounts (user_id,key_id,nsid,username,token_sealed,secret_sealed) VALUES ($1,$2,$3,'test','{}','{}') RETURNING id`, contributorID, keyID, ownerNSID).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE sharing_settings SET enabled=true WHERE user_id=$1`, contributorID); err != nil {
		t.Fatal(err)
	}

	catalog, err := postgres.New(tx)
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.UpsertPeople(ctx, []catalogdomain.Person{{NSID: ownerNSID, Username: suffix}}); err != nil {
		t.Fatal(err)
	}
	photoID := time.Now().UnixNano()
	photo := catalogdomain.Photo{
		ID: photoID, OwnerNSID: ownerNSID, LastUpdate: time.Now().UTC(),
		Visibility: catalogdomain.VisibilityPublic, Raw: []byte(`{"id":1}`),
	}
	if err := catalog.UpsertPhotos(ctx, []catalogdomain.Photo{photo}); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO observations (user_id,flickr_account_id,record_kind,record_id) SELECT $1,$2,'photo','fixture-' || value::text FROM generate_series(1,400000) AS value`, contributorID, accountID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO observations (user_id,flickr_account_id,record_kind,record_id) VALUES ($1,$2,'photo',$3)`, contributorID, accountID, fmt.Sprint(photoID)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `ANALYZE observations`); err != nil {
		t.Fatal(err)
	}

	filter := sharingapp.Filter(sharingdomain.User{ID: requesterID})
	query := `SELECT photos.id FROM photos WHERE photos.id=$2 AND ` + filter.SQL.Expression
	args := append(append([]any(nil), filter.SQL.Args...), photoID)
	var visibleID int64
	if err := tx.QueryRow(ctx, query, args...).Scan(&visibleID); err != nil {
		t.Fatalf("shared photo lookup: %v", err)
	}
	if visibleID != photoID {
		t.Fatalf("visible photo id = %d, want %d", visibleID, photoID)
	}

	rows, err := tx.Query(ctx, `EXPLAIN (ANALYZE, BUFFERS, TIMING OFF) `+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	planText := plan.String()
	t.Logf("shared photo lookup plan:\n%s", planText)
	if strings.Contains(planText, "Seq Scan on observations") || strings.Contains(planText, "Seq Scan on observations_p") {
		t.Fatalf("photo sharing lookup scans observations sequentially:\n%s", planText)
	}
	if !strings.Contains(planText, "observations_p00_kind_record_idx") && !strings.Contains(planText, "record_lookup_idx") {
		t.Fatalf("photo sharing lookup did not use a partition record index:\n%s", planText)
	}
}
