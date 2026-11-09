package postgres

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestDownloadCandidatesFromPostgres(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := dbtest.Open(t, downloadDatabase(t))
	base := time.Now().UnixNano()
	userID, keyID, accountID, photoID := base, base+1, base+2, base+3
	owner := fmt.Sprintf("catalog-download-%d@example.invalid", base)
	album := fmt.Sprintf("album-%d", base)
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,email,password_hash,role) VALUES($1,$2,'unused','user')`, userID, owner); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id=$1`, photoID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM albums WHERE id=$1`, album)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, owner)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO api_keys(id,user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,$2,'test','catalog-download','{}',100,2,4)`, keyID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO flickr_accounts(id,user_id,key_id,nsid,username,token_sealed,secret_sealed) VALUES($1,$2,$3,$4,'test','{}','{}')`, accountID, userID, keyID, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,$1)`, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO albums(id,owner_nsid,title) VALUES($1,$2,'test')`, album, owner); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `INSERT INTO photos(id,owner_nsid,last_update,visibility,sizes,sizes_status,sizes_checked_at,raw,content_hash) VALUES($1,$2,$3,'public','[]'::jsonb,'confirmed',$3,'{}',decode(repeat('ab',32),'hex'))`, photoID, owner, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO album_photos(album_id,photo_id,position) VALUES($1,$2,0)`, album, photoID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO observations(user_id,flickr_account_id,record_kind,record_id) VALUES($1,$2,'photo',$3)`, userID, accountID, fmt.Sprint(photoID)); err != nil {
		t.Fatal(err)
	}
	repository := Repository{Pool: pool}
	for _, request := range []struct {
		kind, subject string
		ids           []int64
	}{{"contact", owner, nil}, {"album", album, nil}, {"tray", "", []int64{photoID}}} {
		got, err := repository.DownloadCandidates(ctx, userID, request.kind, request.subject, request.ids)
		if err != nil || len(got) != 1 || got[0].PhotoID != photoID || !got[0].SizesConfirmed || len(got[0].Observers) != 1 {
			t.Fatalf("%s candidates=%+v err=%v", request.kind, got, err)
		}
	}
	if _, err := (Repository{}).DownloadCandidates(ctx, userID, "tray", "", nil); err == nil {
		t.Fatal("accepted missing database")
	}
	if _, err := repository.DownloadCandidates(nil, userID, "tray", "", nil); err == nil {
		t.Fatal("accepted missing context")
	}
	if _, err := repository.DownloadCandidates(ctx, userID, "unknown", "", nil); err == nil {
		t.Fatal("accepted invalid kind")
	}
}

func TestDownloadCandidatesScopeRestrictedSizesToTheCaller(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run with XFLICKR_INTEGRATION_TEST=1 to use this worktree's PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := dbtest.Open(t, downloadDatabase(t))
	base := time.Now().UnixNano()
	ownerUser, otherUser, keyID, accountID, photoID := base, base+1, base+2, base+3, base+4
	owner := fmt.Sprintf("catalog-account-sizes-%d", base)
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,email,password_hash,role) VALUES($1,$2,'unused','user'),($3,$4,'unused','user')`, ownerUser, owner+"-owner@example.invalid", otherUser, owner+"-other@example.invalid"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id=$1`, photoID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, owner)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=ANY($1)`, []int64{ownerUser, otherUser})
	})
	if _, err := pool.Exec(ctx, `INSERT INTO api_keys(id,user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,$2,'test',$3,'{}',100,2,4)`, keyID, ownerUser, owner+"-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO flickr_accounts(id,user_id,key_id,nsid,username,token_sealed,secret_sealed) VALUES($1,$2,$3,$4,'test','{}','{}')`, accountID, ownerUser, keyID, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,$1)`, owner); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `INSERT INTO photos(id,owner_nsid,server,last_update,visibility,sizes,sizes_status,sizes_checked_at,raw,content_hash)
		VALUES($1,$2,'ordinary',$3,'private',$4::jsonb,'confirmed',$3,'{}',decode(repeat('ab',32),'hex'))`, photoID, owner, now, `[{"label":"Original","source":"https://example.invalid/global-size"}]`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO observations(user_id,flickr_account_id,record_kind,record_id) VALUES($1,$2,'photo',$3)`, ownerUser, accountID, fmt.Sprint(photoID)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sharing_settings SET enabled=true WHERE user_id=$1`, ownerUser); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO photo_account_sizes(photo_id,account_id,photo_updated_at,photo_visibility,sizes,sizes_status,sizes_checked_at)
		VALUES($1,$2,$3,'private',$4::jsonb,'confirmed',$3)`, photoID, accountID, now, `[{"label":"Original","source":"https://example.invalid/account-size"}]`); err != nil {
		t.Fatal(err)
	}
	repository := Repository{Pool: pool}
	ownerCandidates, err := repository.DownloadCandidates(ctx, ownerUser, "tray", "", []int64{photoID})
	if err != nil || len(ownerCandidates) != 1 || !ownerCandidates[0].SizesConfirmed {
		t.Fatalf("owner download candidates=%+v err=%v", ownerCandidates, err)
	}
	otherCandidates, err := repository.DownloadCandidates(ctx, otherUser, "tray", "", []int64{photoID})
	if err != nil || len(otherCandidates) != 1 || otherCandidates[0].SizesConfirmed {
		t.Fatalf("shared user's download candidates=%+v err=%v", otherCandidates, err)
	}
}

func downloadDatabase(t *testing.T) config.DBConfig {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate catalog download integration test")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatalf("load test database settings: %v", err)
	}
	if err := dbtest.ValidateDatabaseName(cfg.Name); err != nil {
		t.Fatal(err)
	}
	return cfg
}
