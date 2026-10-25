package postgres_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	catalogpostgres "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	catalogobservations "github.com/jooservices/xflickr/internal/catalog/adapters/postgres/observations"
	"github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	sharingpostgres "github.com/jooservices/xflickr/internal/sharing/adapters/postgres"
	sharingdomain "github.com/jooservices/xflickr/internal/sharing/domain"
)

func TestObservationUpsertAndSharingOutbox(t *testing.T) {
	if _, err := sharingpostgres.New(nil); err == nil {
		t.Fatal("expected missing database error")
	}
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	pool := dbtest.Open(t, cfg)
	suffix := fmt.Sprintf("sharing-%d", time.Now().UnixNano())
	email := suffix + "@example.invalid"
	var userID, keyID, accountID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (email,password_hash,role) VALUES ($1,'hash','user') RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	photoID := time.Now().UnixNano()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id=$1`, photoID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, suffix+"@flickr")
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys (user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES ($1,'test','public-test','{}',100,10,10) RETURNING id`, userID).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts (user_id,key_id,nsid,username,token_sealed,secret_sealed) VALUES ($1,$2,$3,'test','{}','{}') RETURNING id`, userID, keyID, suffix+"@flickr").Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	record := domain.Photo{ID: photoID, OwnerNSID: suffix + "@flickr", LastUpdate: time.Now().UTC(), Visibility: domain.VisibilityPublic, Raw: []byte(`{"id":1}`)}
	write := func() {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		repo, err := catalogpostgres.New(tx)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.UpsertPeople(ctx, []domain.Person{{NSID: suffix + "@flickr", Username: "test"}}); err != nil {
			t.Fatal(err)
		}
		observed, err := catalogobservations.New(tx, userID, accountID)
		if err != nil {
			t.Fatal(err)
		}
		if err := observed.UpsertPhotos(ctx, []domain.Photo{record}); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	write()
	var firstSeen time.Time
	if err := pool.QueryRow(ctx, `SELECT first_seen_at FROM observations WHERE user_id=$1 AND flickr_account_id=$2 AND record_kind='photo' AND record_id=$3`, userID, accountID, fmt.Sprint(photoID)).Scan(&firstSeen); err != nil {
		t.Fatal(err)
	}
	write()
	var observations int
	var firstSeenAgain time.Time
	if err := pool.QueryRow(ctx, `SELECT count(*), min(first_seen_at) FROM observations WHERE user_id=$1 AND flickr_account_id=$2 AND record_kind='photo' AND record_id=$3`, userID, accountID, fmt.Sprint(photoID)).Scan(&observations, &firstSeenAgain); err != nil {
		t.Fatal(err)
	}
	if observations != 1 || !firstSeen.Equal(firstSeenAgain) {
		t.Fatalf("observation count/time = %d/%v, want once at %v", observations, firstSeenAgain, firstSeen)
	}
	settings, err := sharingpostgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	setting, err := settings.Set(ctx, sharingdomain.Setting{UserID: userID, Enabled: true})
	if err != nil || !setting.Enabled {
		t.Fatalf("enable sharing = %+v, %v", setting, err)
	}
	if _, err := settings.Set(ctx, sharingdomain.Setting{UserID: userID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE topic=$1 AND key=$2`, sharingdomain.ChangedTopic, fmt.Sprint(userID)).Scan(&observations); err != nil {
		t.Fatal(err)
	}
	if observations != 1 {
		t.Fatalf("same-value event count = %d, want 1", observations)
	}
	if _, err := settings.Set(ctx, sharingdomain.Setting{UserID: userID, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE topic=$1 AND key=$2`, sharingdomain.ChangedTopic, fmt.Sprint(userID)).Scan(&observations); err != nil {
		t.Fatal(err)
	}
	if observations != 2 {
		t.Fatalf("toggle event count = %d, want 2", observations)
	}
	if _, err := settings.Set(ctx, sharingdomain.Setting{UserID: userID + 999999999, Enabled: true}); err == nil {
		t.Fatal("expected unknown user error")
	}
	function := fmt.Sprintf("sharing_outbox_failure_%d", time.Now().UnixNano())
	trigger := function + "_trigger"
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $fn$ BEGIN IF NEW.topic='sharing.changed' AND NEW.key='%d' THEN RAISE EXCEPTION 'forced outbox error'; END IF; RETURN NEW; END; $fn$`, function, userID)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION %s()`, trigger, function)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON outbox_events`, trigger))
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, function))
	})
	if _, err := settings.Set(ctx, sharingdomain.Setting{UserID: userID, Enabled: true}); err == nil {
		t.Fatal("expected outbox publish failure")
	}
}
