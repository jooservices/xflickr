package postgres_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/home/adapters/postgres"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestHomeSnapshotIsUserScopedVisibleAndBounded(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's isolated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := dbtest.Open(t, homeDatabase(t))
	repository, err := postgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}

	suffix := fmt.Sprintf("home-%d", time.Now().UnixNano())
	var userID, otherUserID, keyID, otherKeyID, accountID, otherAccountID int64
	personIDs := []string{suffix + "-self", suffix + "-contact-0", suffix + "-contact-1", suffix + "-contact-2", suffix + "-contact-3", suffix + "-contact-4", suffix + "-contact-5", suffix + "-discovered"}
	photoIDs := make([]int64, 0, 16)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if len(photoIDs) > 0 {
			_, _ = pool.Exec(cleanup, `DELETE FROM photo_enrichments WHERE photo_id=ANY($1::bigint[])`, photoIDs)
			_, _ = pool.Exec(cleanup, `DELETE FROM photos WHERE id=ANY($1::bigint[])`, photoIDs)
		}
		_, _ = pool.Exec(cleanup, `DELETE FROM people WHERE nsid=ANY($1::text[])`, personIDs)
		_, _ = pool.Exec(cleanup, `DELETE FROM users WHERE id=ANY($1::bigint[])`, []int64{userID, otherUserID})
	})
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration','user') RETURNING id`, suffix+"-one@example.invalid").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration','user') RETURNING id`, suffix+"-two@example.invalid").Scan(&otherUserID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,'home-test',$2,'{}',1000,10,20) RETURNING id`, userID, suffix+"-key-one").Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,'home-test',$2,'{}',1000,10,20) RETURNING id`, otherUserID, suffix+"-key-two").Scan(&otherKeyID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,'owner-one','{}','{}','ok') RETURNING id`, userID, keyID, personIDs[0]).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,'owner-two','{}','{}','ok') RETURNING id`, otherUserID, otherKeyID, suffix+"-shared-owner").Scan(&otherAccountID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sharing_settings SET enabled=true WHERE user_id=$1`, otherUserID); err != nil {
		t.Fatal(err)
	}
	for i, nsid := range personIDs {
		if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username,realname,icon) VALUES($1,$2,$3,$4)`, nsid, "user-"+fmt.Sprint(i), "Person "+fmt.Sprint(i), "https://example.invalid/icon.jpg"); err != nil {
			t.Fatal(err)
		}
	}
	sharedOwner := suffix + "-shared-owner"
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username,realname) VALUES($1,'shared-owner','Shared Owner')`, sharedOwner); err != nil {
		t.Fatal(err)
	}
	for i, nsid := range personIDs {
		depth := 2
		switch i {
		case 0:
			depth = 0
		case 1:
			depth = 1
		}
		if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via,starred) VALUES($1,$2,$3,'home-test',$4)`, userID, nsid, depth, i >= 1 && i <= 6); err != nil {
			t.Fatal(err)
		}
	}
	for i, state := range []string{"running", "pending", "failed", "done"} {
		if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state) VALUES('photos',$1,$2,$3)`, personIDs[i+1], userID, state); err != nil {
			t.Fatal(err)
		}
	}

	baseID := time.Now().UnixNano()
	baseTime := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	addPhoto := func(id int64, owner, visibility string, uploaded time.Time, status string, observerUser, observerAccount int64, caption bool) {
		t.Helper()
		photoIDs = append(photoIDs, id)
		checked := uploaded
		var sizesChecked any = checked
		if status != "confirmed" {
			sizesChecked = nil
		}
		_, err := pool.Exec(ctx, `INSERT INTO photos(id,owner_nsid,title,last_update,uploaded_at,taken_at,visibility,sizes,sizes_status,sizes_checked_at,raw,content_hash)
			VALUES($1,$2,$3,$4,$4,$4,$5,$6::jsonb,$7,$8,'{}',decode(repeat('ab',32),'hex'))`, id, owner, fmt.Sprintf("photo-%d", id), uploaded, visibility, `[{"label":"Small 240","url":"https://example.invalid/thumb.jpg"}]`, status, sizesChecked)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO observations(user_id,flickr_account_id,record_kind,record_id) VALUES($1,$2,'photo',$3)`, observerUser, observerAccount, fmt.Sprint(id)); err != nil {
			t.Fatal(err)
		}
		if caption {
			if _, err := pool.Exec(ctx, `INSERT INTO photo_enrichments(photo_id,kind,model,model_version,payload) VALUES($1,'caption','integration','1','{}')`, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	privatePhotoID := baseID
	addPhoto(privatePhotoID, personIDs[1], "private", baseTime.Add(60*time.Minute), "confirmed", userID, accountID, true)
	hiddenPhotoID := baseID + 19
	addPhoto(hiddenPhotoID, personIDs[3], "friends", baseTime.Add(45*time.Minute), "confirmed", userID, accountID, false)
	for i := 0; i < 13; i++ {
		status := "confirmed"
		if i == 12 {
			status = "provisional"
		}
		addPhoto(baseID+int64(i)+1, personIDs[1], "public", baseTime.Add(time.Duration(i+1)*time.Minute), status, userID, accountID, false)
	}
	sharedPublicID := baseID + 20
	addPhoto(sharedPublicID, personIDs[2], "public", baseTime.Add(30*time.Minute), "confirmed", otherUserID, otherAccountID, true)
	sharedPrivateID := baseID + 21
	addPhoto(sharedPrivateID, personIDs[2], "private", baseTime.Add(31*time.Minute), "confirmed", otherUserID, otherAccountID, false)

	snapshot, err := repository.Load(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.GeneratedAt.IsZero() {
		t.Fatal("generated_at is zero")
	}
	if snapshot.Contacts.Total != 7 || snapshot.Contacts.Direct != 1 || snapshot.Contacts.Discovered != 6 {
		t.Fatalf("contacts = %+v", snapshot.Contacts)
	}
	if snapshot.Photos.Total != 14 || snapshot.Photos.SizesReady != 13 || snapshot.Photos.Captioned != 1 {
		t.Fatalf("photos = %+v", snapshot.Photos)
	}
	if len(snapshot.NewPhotos) != 12 || snapshot.NewPhotos[0].ID != sharedPublicID || snapshot.NewPhotos[0].ThumbnailURL == nil || *snapshot.NewPhotos[0].ThumbnailURL == "" {
		t.Fatalf("new photos count or newest thumbnail is wrong: %+v", snapshot.NewPhotos)
	}
	for _, photo := range snapshot.NewPhotos {
		if photo.ID == privatePhotoID || photo.ID == hiddenPhotoID || photo.ID == sharedPrivateID {
			t.Fatalf("private or hidden photo %d is present in the Home snapshot", photo.ID)
		}
	}
	if len(snapshot.StarredContacts) != 5 {
		t.Fatalf("starred contacts count = %d, want 5", len(snapshot.StarredContacts))
	}
	if got := snapshot.StarredContacts[0]; got.NSID != personIDs[1] || got.PhotosCount != 13 || got.SizesReady != 12 || got.SizesTotal != 13 || got.CrawlState != "crawling" {
		t.Fatalf("first starred contact = %+v", got)
	}
	if got := snapshot.StarredContacts[1]; got.NSID != personIDs[2] || got.PhotosCount != 1 || got.CrawlState != "waiting" {
		t.Fatalf("second starred contact = %+v", got)
	}
	if got := snapshot.StarredContacts[2]; got.NSID != personIDs[3] || got.CrawlState != "attention" {
		t.Fatalf("third starred contact = %+v", got)
	}
	if got := snapshot.StarredContacts[3]; got.NSID != personIDs[4] || got.CrawlState != "idle" {
		t.Fatalf("fourth starred contact = %+v", got)
	}
	if len(snapshot.Activity) > 5 {
		t.Fatalf("activity count = %d, want at most 5", len(snapshot.Activity))
	}
}

func homeDatabase(t *testing.T) config.DBConfig {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
