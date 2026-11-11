package postgres

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/home/app"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/platform/queue"
)

func TestHomeSnapshotIsUserScopedVisibleAndBounded(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's isolated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	pool := dbtest.Open(t, homeDatabase(t))
	queueStub := &homeRefreshQueueStub{}
	requestDB := &requestRecorder{Pool: pool}
	repository, err := New(requestDB, queueStub)
	if err != nil {
		t.Fatal(err)
	}

	suffix := fmt.Sprintf("home-%d", time.Now().UnixNano())
	var userID, otherUserID, emptyUserID, keyID, otherKeyID, accountID, otherAccountID, downloadID, quotaCallID int64
	var runningWorkItemID, failedWorkItemID, doneWorkItemID int64
	personIDs := []string{suffix + "-self", suffix + "-contact-0", suffix + "-contact-1", suffix + "-contact-2", suffix + "-contact-3", suffix + "-contact-4", suffix + "-contact-5", suffix + "-discovered"}
	photoIDs := make([]int64, 0, 16)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if downloadID > 0 {
			_, _ = pool.Exec(cleanup, `DELETE FROM downloads WHERE id=$1`, downloadID)
		}
		if len(photoIDs) > 0 {
			_, _ = pool.Exec(cleanup, `DELETE FROM photo_enrichments WHERE photo_id=ANY($1::bigint[])`, photoIDs)
			_, _ = pool.Exec(cleanup, `DELETE FROM photos WHERE id=ANY($1::bigint[])`, photoIDs)
		}
		_, _ = pool.Exec(cleanup, `DELETE FROM people WHERE nsid=ANY($1::text[])`, personIDs)
		_, _ = pool.Exec(cleanup, `DELETE FROM users WHERE id=ANY($1::bigint[])`, []int64{userID, otherUserID, emptyUserID})
	})
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration','user') RETURNING id`, suffix+"-one@example.invalid").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration','user') RETURNING id`, suffix+"-two@example.invalid").Scan(&otherUserID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration','user') RETURNING id`, suffix+"-empty@example.invalid").Scan(&emptyUserID); err != nil {
		t.Fatal(err)
	}
	emptyRepository, err := New(pool, &homeRefreshQueueStub{})
	if err != nil {
		t.Fatal(err)
	}
	done, err := emptyRepository.RefreshBatch(ctx, emptyUserID, 3)
	if err != nil || !done {
		t.Fatalf("refresh an empty new account = done %t, err %v", done, err)
	}
	emptySnapshot, err := emptyRepository.Load(ctx, emptyUserID)
	if err != nil || emptySnapshot.Estimated || emptySnapshot.Photos.Total != 0 {
		t.Fatalf("empty new account snapshot = %+v, err %v", emptySnapshot, err)
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
		occurredAt := time.Now().UTC().Add(-time.Duration(i+3) * time.Minute)
		var workItemID int64
		if err := pool.QueryRow(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state,created_at,updated_at) VALUES('photos',$1,$2,$3,$4,$4) RETURNING id`, personIDs[i+1], userID, state, occurredAt).Scan(&workItemID); err != nil {
			t.Fatal(err)
		}
		switch state {
		case "running":
			runningWorkItemID = workItemID
		case "failed":
			failedWorkItemID = workItemID
		case "done":
			doneWorkItemID = workItemID
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
	assertSharedCandidate(t, ctx, pool, sharedPublicID, true)
	assertSharedCandidate(t, ctx, pool, sharedPrivateID, false)
	if err := pool.QueryRow(ctx, `INSERT INTO downloads(user_id,kind,subject,status,created_at,expires_at)
VALUES($1,'contact',$2,'ready',statement_timestamp()-interval '2 minutes',statement_timestamp()+interval '1 day') RETURNING id`, userID, personIDs[1]).Scan(&downloadID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO download_items(download_id,photo_id) VALUES($1,$2),($1,$3)`, downloadID, baseID+1, baseID+2); err != nil {
		t.Fatal(err)
	}
	quotaAt := time.Now().UTC().Add(-time.Minute)
	if err := pool.QueryRow(ctx, `INSERT INTO api_call_log(at,key_id,class,called_at,user_id,method)
VALUES($1,$2,'integration',$1,$3,'test') RETURNING id`, quotaAt, keyID, userID).Scan(&quotaCallID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO api_call_log(at,key_id,class,called_at,user_id,method)
VALUES(statement_timestamp(),$1,'integration',statement_timestamp(),$2,'test')`, otherKeyID, otherUserID); err != nil {
		t.Fatal(err)
	}

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
	if snapshot.Photos.Total != 0 || snapshot.CountsComputedAt != nil || !snapshot.Estimated || !snapshot.CountsStale {
		t.Fatalf("missing cached photo counts = %+v, computed_at=%v, estimated=%t", snapshot.Photos, snapshot.CountsComputedAt, snapshot.Estimated)
	}
	if queueStub.calls != 1 || queueStub.job.Kind != app.RefreshJobKind {
		t.Fatalf("missing stats row was not queued once: %+v", queueStub)
	}
	refresher := app.Refresher{Processor: repository, BatchSize: 3}
	for {
		done, err := refresher.Refresh(ctx, otherUserID)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
	}
	for {
		done, err := refresher.Refresh(ctx, userID)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
	}
	snapshot, err = repository.Load(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Photos.Total != 14 || snapshot.Photos.SizesReady != 13 || snapshot.Photos.Captioned != 1 || snapshot.CountsComputedAt == nil || snapshot.Estimated {
		t.Fatalf("refreshed photos = %+v, computed_at=%v, estimated=%t", snapshot.Photos, snapshot.CountsComputedAt, snapshot.Estimated)
	}
	if queueStub.calls != 1 {
		t.Fatalf("fresh stats were enqueued again, calls=%d", queueStub.calls)
	}
	if len(snapshot.Activity) != 5 {
		t.Fatalf("activity count = %d, want five recent user events", len(snapshot.Activity))
	}
	wantQuotaID := "quota:" + fmt.Sprint(quotaCallID)
	if event := snapshot.Activity[0]; event.ID != wantQuotaID || event.Kind != "quota" || event.MessageKey != "quota_updated" || event.Severity != "info" || event.ContactName != "" || event.OccurredAt.IsZero() {
		t.Fatalf("newest quota activity = %+v, want id %q", event, wantQuotaID)
	}
	if event := snapshot.Activity[1]; event.Kind != "download" || event.ID != "download:"+fmt.Sprint(downloadID) || event.Count != 2 || event.ContactName != "" || event.OccurredAt.IsZero() {
		t.Fatalf("recent download activity = %+v", event)
	}
	if event := snapshot.Activity[2]; event.Kind != "crawl" || event.ID != "crawl:"+fmt.Sprint(runningWorkItemID) || event.MessageKey != "unknown" || event.Count != 0 || event.ContactName != "Person 1" || event.Severity != "info" || event.OccurredAt.IsZero() {
		t.Fatalf("recent running crawl activity = %+v", event)
	}
	if event := snapshot.Activity[3]; event.Kind != "crawl" || event.ID != "crawl:"+fmt.Sprint(failedWorkItemID) || event.MessageKey != "unknown" || event.Count != 0 || event.ContactName != "Person 3" || event.Severity != "error" || event.OccurredAt.IsZero() {
		t.Fatalf("recent failed crawl activity = %+v", event)
	}
	if event := snapshot.Activity[4]; event.Kind != "crawl" || event.ID != "crawl:"+fmt.Sprint(doneWorkItemID) || event.MessageKey != "unknown" || event.Count != 0 || event.ContactName != "Person 4" || event.Severity != "info" || event.OccurredAt.IsZero() {
		t.Fatalf("recent completed crawl activity = %+v", event)
	}
	if _, err := pool.Exec(ctx, `UPDATE sharing_settings SET enabled=false WHERE user_id=$1`, otherUserID); err != nil {
		t.Fatal(err)
	}
	assertSharingSyncPending(t, ctx, pool, otherUserID, true)
	assertSharedCandidate(t, ctx, pool, sharedPublicID, true)
	snapshot, err = repository.Load(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Estimated || !snapshot.CountsStale || snapshot.Photos.Total != 0 || snapshot.StarredContacts[1].PhotosCount != 0 || queueStub.calls != 2 {
		t.Fatalf("sharing change did not invalidate the cached snapshot: %+v, queued=%d", snapshot.Photos, queueStub.calls)
	}
	assertPhotoMissing(t, snapshot.NewPhotos, sharedPublicID)
	for {
		done, err := repository.RefreshBatch(ctx, otherUserID, 1)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
	}
	assertSharingSyncPending(t, ctx, pool, otherUserID, false)
	assertSharedCandidate(t, ctx, pool, sharedPublicID, false)
	if _, err := pool.Exec(ctx, `UPDATE sharing_settings SET enabled=true WHERE user_id=$1`, otherUserID); err != nil {
		t.Fatal(err)
	}
	assertSharingSyncPending(t, ctx, pool, otherUserID, true)
	assertSharedCandidate(t, ctx, pool, sharedPublicID, false)
	snapshot, err = repository.Load(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	assertPhotoMissing(t, snapshot.NewPhotos, sharedPublicID)
	for {
		done, err := repository.RefreshBatch(ctx, otherUserID, 1)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
	}
	assertSharingSyncPending(t, ctx, pool, otherUserID, false)
	assertSharedCandidate(t, ctx, pool, sharedPublicID, true)
	for {
		done, err := repository.RefreshBatch(ctx, userID, 3)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
	}
	snapshot, err = repository.Load(ctx, userID)
	if err != nil || snapshot.Estimated || snapshot.Photos.Total != 14 {
		t.Fatalf("refreshed shared snapshot = %+v estimated=%t err=%v", snapshot.Photos, snapshot.Estimated, err)
	}
	var priorInterval string
	if err := pool.QueryRow(ctx, `SELECT value FROM app_settings WHERE key='home.stats_refresh_interval'`).Scan(&priorInterval); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `UPDATE app_settings SET value=$1::jsonb WHERE key='home.stats_refresh_interval'`, priorInterval)
	})
	if _, err := pool.Exec(ctx, `UPDATE app_settings SET value='60'::jsonb WHERE key='home.stats_refresh_interval'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE user_home_stats SET computed_at=statement_timestamp()-interval '61 seconds' WHERE user_id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	snapshot, err = repository.Load(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Estimated || snapshot.Photos.Total != 14 || snapshot.CountsComputedAt == nil || queueStub.calls != 4 {
		t.Fatalf("stale counts snapshot = %+v, computed_at=%v, estimated=%t, enqueue calls=%d", snapshot.Photos, snapshot.CountsComputedAt, snapshot.Estimated, queueStub.calls)
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
	if done, err := refresher.Refresh(ctx, userID); err != nil || done {
		t.Fatalf("first refresh batch = done %t, err %v", done, err)
	}
	var priorCursor string
	if err := pool.QueryRow(ctx, `SELECT cursor_record_id FROM user_home_stats_refreshes WHERE user_id=$1`, userID).Scan(&priorCursor); err != nil {
		t.Fatal(err)
	}
	changedPhotoID := baseID + 1
	if _, err := pool.Exec(ctx, `UPDATE photos SET visibility='private' WHERE id=$1`, changedPhotoID); err != nil {
		t.Fatal(err)
	}
	changedSnapshot, err := repository.Load(ctx, userID)
	if err != nil || !changedSnapshot.Estimated || !changedSnapshot.CountsStale || changedSnapshot.Photos.Total != 0 || changedSnapshot.StarredContacts[0].PhotosCount != 0 || changedSnapshot.CountsComputedAt != nil {
		t.Fatalf("in-progress refresh published a changed source: %+v, err=%v", changedSnapshot, err)
	}
	if done, err := refresher.Refresh(ctx, userID); err != nil || done {
		t.Fatalf("refresh after a source revision change = done %t, err %v", done, err)
	}
	var nextCursor string
	if err := pool.QueryRow(ctx, `SELECT cursor_record_id FROM user_home_stats_refreshes WHERE user_id=$1`, userID).Scan(&nextCursor); err != nil {
		t.Fatal(err)
	}
	if nextCursor != fmt.Sprint(changedPhotoID+3) {
		t.Fatalf("refresh did not restart at the first current candidate after access revocation: cursor=%q, want %d (previous %q)", nextCursor, changedPhotoID+3, priorCursor)
	}
	for {
		done, err := refresher.Refresh(ctx, userID)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
	}
	snapshot, err = repository.Load(ctx, userID)
	if err != nil || snapshot.Estimated || snapshot.CountsStale || snapshot.Photos.Total != 13 {
		t.Fatalf("refresh after access revocation = %+v estimated=%t stale=%t err=%v", snapshot.Photos, snapshot.Estimated, snapshot.CountsStale, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sharing_settings SET enabled=false WHERE user_id=$1`, otherUserID); err != nil {
		t.Fatal(err)
	}
	snapshot, err = repository.Load(ctx, userID)
	if err != nil || !snapshot.Estimated || !snapshot.CountsStale || snapshot.Photos.Total != 0 {
		t.Fatalf("post-refresh permission change did not invalidate snapshot: %+v estimated=%t err=%v", snapshot.Photos, snapshot.Estimated, err)
	}
	if len(snapshot.NewPhotos) != 12 {
		t.Fatalf("revoked shared photo reduced the visible page to %d entries", len(snapshot.NewPhotos))
	}
	assertPhotoMissing(t, snapshot.NewPhotos, sharedPublicID)
	for {
		done, err := refresher.Refresh(ctx, otherUserID)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
	}
	for {
		done, err := refresher.Refresh(ctx, userID)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
	}
	snapshot, err = repository.Load(ctx, userID)
	if err != nil || snapshot.Estimated || snapshot.CountsStale || snapshot.Photos.Total != 12 {
		t.Fatalf("post-revocation refresh = %+v estimated=%t err=%v", snapshot.Photos, snapshot.Estimated, err)
	}
}

func TestHomeRefreshKeepsCountsAvailableAfterConcurrentCatalogChange(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's isolated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	pool := dbtest.Open(t, homeDatabase(t))
	suffix := fmt.Sprintf("home-refresh-change-%d", time.Now().UnixNano())
	owner := suffix + "-owner"
	var userID, keyID, accountID int64
	photoIDs := []int64{}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if len(photoIDs) > 0 {
			_, _ = pool.Exec(cleanup, `DELETE FROM photos WHERE id=ANY($1::bigint[])`, photoIDs)
		}
		_, _ = pool.Exec(cleanup, `DELETE FROM people WHERE nsid=$1`, owner)
		_, _ = pool.Exec(cleanup, `DELETE FROM users WHERE id=$1`, userID)
	})
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration','user') RETURNING id`, suffix+"@example.invalid").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,$1)`, owner); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,'home-test',$2,'{}',100,10,10) RETURNING id`, userID, suffix).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,$3,'{}','{}','ok') RETURNING id`, userID, keyID, owner).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	baseID := time.Now().UnixNano()
	baseTime := time.Now().UTC().Truncate(time.Microsecond)
	addPhoto := func(id int64) {
		t.Helper()
		photoIDs = append(photoIDs, id)
		if _, err := pool.Exec(ctx, `INSERT INTO photos(id,owner_nsid,last_update,uploaded_at,visibility,sizes,sizes_status,raw,content_hash)
			VALUES($1,$2,$3,$3,'public','[]'::jsonb,'provisional','{}',decode(repeat('cd',32),'hex'))`, id, owner, baseTime); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO observations(user_id,flickr_account_id,record_kind,record_id) VALUES($1,$2,'photo',$3)`, userID, accountID, fmt.Sprint(id)); err != nil {
			t.Fatal(err)
		}
	}
	addPhoto(baseID)
	addPhoto(baseID + 1)
	queueStub := &homeRefreshQueueStub{}
	repository, err := New(pool, queueStub)
	if err != nil {
		t.Fatal(err)
	}
	refresher := app.Refresher{Processor: repository, BatchSize: 1}
	done, err := refresher.Refresh(ctx, userID)
	if err != nil || done {
		t.Fatalf("first refresh batch = done %t, err %v", done, err)
	}
	addPhoto(baseID - 1)
	for {
		done, err = refresher.Refresh(ctx, userID)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
	}
	snapshot, err := repository.Load(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CountsStale || !snapshot.Estimated || snapshot.CountsComputedAt == nil || snapshot.Photos.Total != 2 || queueStub.calls != 0 {
		t.Fatalf("concurrent catalog change snapshot = %+v computed_at=%v estimated=%t stale=%t queued=%d", snapshot.Photos, snapshot.CountsComputedAt, snapshot.Estimated, snapshot.CountsStale, queueStub.calls)
	}
	if _, err := pool.Exec(ctx, `UPDATE user_home_stats SET computed_at=statement_timestamp()-interval '1 day' WHERE user_id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	snapshot, err = repository.Load(ctx, userID)
	if err != nil || queueStub.calls != 1 {
		t.Fatalf("expired estimated snapshot queued=%d, err=%v", queueStub.calls, err)
	}
	for {
		done, err = refresher.Refresh(ctx, userID)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
	}
	snapshot, err = repository.Load(ctx, userID)
	if err != nil || snapshot.CountsStale || snapshot.Estimated || snapshot.Photos.Total != 3 {
		t.Fatalf("reconciled catalog snapshot = %+v estimated=%t stale=%t err=%v", snapshot.Photos, snapshot.Estimated, snapshot.CountsStale, err)
	}
}

func TestHomeBackfillWaitsForObservationDelete(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's isolated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	pool := dbtest.Open(t, homeDatabase(t))
	suffix := fmt.Sprintf("home-backfill-race-%d", time.Now().UnixNano())
	owner := suffix + "-owner"
	var userID, keyID, accountID, photoID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration','user') RETURNING id`, suffix+"@example.invalid").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_, _ = pool.Exec(cleanup, `DELETE FROM photos WHERE id=$1`, photoID)
		_, _ = pool.Exec(cleanup, `DELETE FROM people WHERE nsid=$1`, owner)
		_, _ = pool.Exec(cleanup, `DELETE FROM users WHERE id=$1`, userID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,$1)`, owner); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,'home-test',$2,'{}',100,10,10) RETURNING id`, userID, suffix).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,$3,'{}','{}','ok') RETURNING id`, userID, keyID, owner).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COALESCE(max(id),0)+1 FROM photos`).Scan(&photoID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO photos(id,owner_nsid,last_update,uploaded_at,visibility,sizes,sizes_status,raw,content_hash)
		VALUES($1,$2,statement_timestamp(),statement_timestamp(),'public','[]'::jsonb,'provisional','{}',decode(repeat('ef',32),'hex'))`, photoID, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO observations(user_id,flickr_account_id,record_kind,record_id) VALUES($1,$2,'photo',$3)`, userID, accountID, fmt.Sprint(photoID)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM user_home_photo_candidates WHERE user_id=$1 AND photo_id=$2`, userID, photoID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_home_candidates_backfill_progress(id,cursor_photo_id) VALUES(TRUE,$1)
		ON CONFLICT(id) DO UPDATE SET cursor_photo_id=EXCLUDED.cursor_photo_id`, photoID-1); err != nil {
		t.Fatal(err)
	}
	lockKey := photoID + 987654321
	function := pgx.Identifier{fmt.Sprintf("home_backfill_pause_%d", userID)}.Sanitize()
	trigger := pgx.Identifier{fmt.Sprintf("home_backfill_pause_trigger_%d", userID)}.Sanitize()
	functionSQL := fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $body$
BEGIN
  IF NEW.photo_id = %d THEN PERFORM pg_advisory_xact_lock(%d); END IF;
  RETURN NEW;
END;
$body$`, function, photoID, lockKey)
	if _, err := pool.Exec(ctx, functionSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE TRIGGER `+trigger+` BEFORE INSERT ON user_home_photo_candidates FOR EACH ROW EXECUTE FUNCTION `+function+`()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_, _ = pool.Exec(cleanup, `DROP TRIGGER IF EXISTS `+trigger+` ON user_home_photo_candidates`)
		_, _ = pool.Exec(cleanup, `DROP FUNCTION IF EXISTS `+function+`() `)
	})
	holder, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lockHeld := true
	var backfill, deletion *pgxpool.Conn
	var backfillDone, deleteDone chan struct{}
	var backfillErr, deleteErr error
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if lockHeld {
			_, _ = holder.Exec(cleanup, `SELECT pg_advisory_unlock($1)`, lockKey)
		}
		if backfillDone != nil {
			select {
			case <-backfillDone:
			case <-cleanup.Done():
			}
		}
		if deleteDone != nil {
			select {
			case <-deleteDone:
			case <-cleanup.Done():
			}
		}
		if deletion != nil {
			deletion.Release()
		}
		if backfill != nil {
			backfill.Release()
		}
		holder.Release()
	}()
	if _, err := holder.Exec(ctx, `SELECT pg_advisory_lock($1)`, lockKey); err != nil {
		t.Fatal(err)
	}
	backfill, err = pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var backfillPID int32
	if err := backfill.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&backfillPID); err != nil {
		t.Fatal(err)
	}
	backfillDone = make(chan struct{})
	go func() {
		_, backfillErr = backfill.Exec(ctx, `CALL backfill_home_photo_candidates(1)`)
		close(backfillDone)
	}()
	if !awaitBackendLock(ctx, pool, backfillPID, backfillDone) {
		t.Fatal("candidate backfill did not pause at the controlled insert")
	}
	deletion, err = pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var deletionPID int32
	if err := deletion.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&deletionPID); err != nil {
		t.Fatal(err)
	}
	deleteDone = make(chan struct{})
	go func() {
		_, deleteErr = deletion.Exec(ctx, `DELETE FROM observations WHERE user_id=$1 AND record_kind='photo' AND record_id=$2`, userID, fmt.Sprint(photoID))
		close(deleteDone)
	}()
	if !awaitBackendLock(ctx, pool, deletionPID, deleteDone) {
		t.Fatal("observation deletion completed while backfill held its candidate snapshot")
	}
	if _, err := holder.Exec(ctx, `SELECT pg_advisory_unlock($1)`, lockKey); err != nil {
		t.Fatal(err)
	}
	lockHeld = false
	<-backfillDone
	<-deleteDone
	if backfillErr != nil || deleteErr != nil {
		t.Fatalf("concurrent backfill/delete errors = %v / %v", backfillErr, deleteErr)
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_home_photo_candidates WHERE user_id=$1 AND photo_id=$2)`, userID, photoID).Scan(&exists); err != nil || exists {
		t.Fatalf("candidate remains after concurrent observation deletion: exists=%t err=%v", exists, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_home_photo_candidates(user_id,photo_id,uploaded_at)
		SELECT $1,id,uploaded_at FROM photos WHERE id=$2`, userID, photoID); err != nil {
		t.Fatal(err)
	}
	repository, err := New(pool)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.Load(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	assertPhotoMissing(t, snapshot.NewPhotos, photoID)
}

func awaitBackendLock(ctx context.Context, pool *pgxpool.Pool, pid int32, done <-chan struct{}) bool {
	for {
		select {
		case <-done:
			return false
		default:
		}
		var waiting bool
		if err := pool.QueryRow(ctx, `SELECT COALESCE(wait_event_type='Lock',FALSE) FROM pg_stat_activity WHERE pid=$1`, pid).Scan(&waiting); err != nil || waiting {
			return err == nil && waiting
		}
		runtime.Gosched()
	}
}

func TestHomeSnapshotContinuesPastRevokedSharedPhotos(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's isolated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	pool := dbtest.Open(t, homeDatabase(t))
	suffix := fmt.Sprintf("home-revoked-%d", time.Now().UnixNano())
	var viewerID, revokedOwnerID, allowedOwnerID int64
	var revokedKeyID, allowedKeyID, revokedAccountID, allowedAccountID int64
	photoIDs := make([]int64, 0, 13)
	revokedNSID, allowedNSID := suffix+"-revoked", suffix+"-allowed"
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if len(photoIDs) > 0 {
			_, _ = pool.Exec(cleanup, `DELETE FROM photos WHERE id=ANY($1::bigint[])`, photoIDs)
		}
		_, _ = pool.Exec(cleanup, `DELETE FROM people WHERE nsid=ANY($1::text[])`, []string{revokedNSID, allowedNSID})
		_, _ = pool.Exec(cleanup, `DELETE FROM users WHERE id=ANY($1::bigint[])`, []int64{viewerID, revokedOwnerID, allowedOwnerID})
	})
	for i, target := range []*int64{&viewerID, &revokedOwnerID, &allowedOwnerID} {
		if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration','user') RETURNING id`, fmt.Sprintf("%s-%d@example.invalid", suffix, i)).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	for _, owner := range []struct {
		userID    int64
		name      string
		keyID     *int64
		accountID *int64
	}{{revokedOwnerID, revokedNSID, &revokedKeyID, &revokedAccountID}, {allowedOwnerID, allowedNSID, &allowedKeyID, &allowedAccountID}} {
		if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,'home-test',$2,'{}',100,10,10) RETURNING id`, owner.userID, suffix+owner.name).Scan(owner.keyID); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,'home-test','{}','{}','ok') RETURNING id`, owner.userID, *owner.keyID, owner.name).Scan(owner.accountID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO sharing_settings(user_id,enabled,changed_at) VALUES($1,true,statement_timestamp())
			ON CONFLICT (user_id) DO UPDATE SET enabled=EXCLUDED.enabled, changed_at=EXCLUDED.changed_at`, owner.userID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username,realname) VALUES($1,$2,$2)`, owner.name, owner.name); err != nil {
			t.Fatal(err)
		}
	}
	baseID := time.Now().UnixNano()
	baseTime := time.Date(2999, time.January, 1, 0, 0, 0, 0, time.UTC)
	const revokedCount = 1
	const allowedCount = 12
	ownerNSIDs := make([]string, 0, revokedCount+allowedCount)
	titles := make([]string, 0, revokedCount+allowedCount)
	uploadedAt := make([]time.Time, 0, revokedCount+allowedCount)
	observerUserIDs := make([]int64, 0, revokedCount+allowedCount)
	observerAccountIDs := make([]int64, 0, revokedCount+allowedCount)
	recordIDs := make([]string, 0, revokedCount+allowedCount)
	for i := 0; i < revokedCount+allowedCount; i++ {
		ownerNSID, ownerID, accountID := revokedNSID, revokedOwnerID, revokedAccountID
		photoID := baseID + int64(i)
		if i >= revokedCount {
			ownerNSID, ownerID, accountID = allowedNSID, allowedOwnerID, allowedAccountID
			photoID = baseID + 1000 + int64(i-revokedCount)
		}
		photoTime := baseTime.Add(time.Duration(revokedCount+allowedCount-i) * time.Minute)
		photoIDs = append(photoIDs, photoID)
		ownerNSIDs = append(ownerNSIDs, ownerNSID)
		titles = append(titles, fmt.Sprintf("photo-%d", i))
		uploadedAt = append(uploadedAt, photoTime)
		observerUserIDs = append(observerUserIDs, ownerID)
		observerAccountIDs = append(observerAccountIDs, accountID)
		recordIDs = append(recordIDs, fmt.Sprint(photoID))
	}
	if _, err := pool.Exec(ctx, `INSERT INTO photos(id,owner_nsid,title,last_update,uploaded_at,taken_at,visibility,sizes,sizes_status,raw,content_hash)
SELECT photo.id, photo.owner_nsid, photo.title, photo.uploaded_at, photo.uploaded_at, photo.uploaded_at,
       'public', '[]'::jsonb, 'provisional', '{}', decode(repeat('ab',32),'hex')
FROM unnest($1::bigint[], $2::text[], $3::text[], $4::timestamptz[]) AS photo(id, owner_nsid, title, uploaded_at)`, photoIDs, ownerNSIDs, titles, uploadedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO observations(user_id,flickr_account_id,record_kind,record_id)
SELECT observation.user_id, observation.account_id, 'photo', observation.record_id
FROM unnest($1::bigint[], $2::bigint[], $3::text[]) AS observation(user_id, account_id, record_id)`, observerUserIDs, observerAccountIDs, recordIDs); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sharing_settings SET enabled=false WHERE user_id=$1`, revokedOwnerID); err != nil {
		t.Fatal(err)
	}
	repository, err := New(pool)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.Load(ctx, viewerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.NewPhotos) != 12 {
		t.Fatalf("visible newest photos = %d, want 12 after skipping a revoked candidate", len(snapshot.NewPhotos))
	}
	for i, photo := range snapshot.NewPhotos {
		want := baseID + 1000 + int64(i)
		if photo.ID != want {
			t.Fatalf("new photo %d = %d, want allowed photo %d", i, photo.ID, want)
		}
	}
}

type homeRefreshQueueStub struct {
	job   queue.Job
	opts  queue.Options
	calls int
}

func (q *homeRefreshQueueStub) EnqueueStandalone(_ context.Context, job queue.Job, options queue.Options) error {
	q.job, q.opts = job, options
	q.calls++
	return nil
}

type requestRecorder struct {
	*pgxpool.Pool
	statement string
}

func (r *requestRecorder) QueryRow(ctx context.Context, statement string, args ...any) pgx.Row {
	r.statement = statement
	return r.Pool.QueryRow(ctx, statement, args...)
}

type explainDocument []struct {
	Plan explainNode `json:"Plan"`
}

type explainNode struct {
	NodeType     string        `json:"Node Type"`
	IndexName    string        `json:"Index Name"`
	RelationName string        `json:"Relation Name"`
	CTEName      string        `json:"CTE Name"`
	PlanRows     float64       `json:"Plan Rows"`
	ActualRows   float64       `json:"Actual Rows"`
	ActualLoops  float64       `json:"Actual Loops"`
	Plans        []explainNode `json:"Plans"`
}

func assertSharingSyncPending(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID int64, want bool) {
	t.Helper()
	var pending bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_home_sharing_syncs WHERE user_id=$1)`, userID).Scan(&pending); err != nil {
		t.Fatalf("check pending Home sharing sync: %v", err)
	}
	if pending != want {
		t.Fatalf("Home sharing sync pending=%t, want %t", pending, want)
	}
}

func assertPhotoMissing(t *testing.T, photos []app.Photo, photoID int64) {
	t.Helper()
	for _, photo := range photos {
		if photo.ID == photoID {
			t.Fatalf("photo %d remains visible after sharing was revoked or before it was granted", photoID)
		}
	}
}

func assertSharedCandidate(t *testing.T, ctx context.Context, pool *pgxpool.Pool, photoID int64, want bool) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_home_shared_photo_candidates WHERE photo_id=$1)`, photoID).Scan(&exists); err != nil {
		t.Fatalf("query shared candidate %d: %v", photoID, err)
	}
	if exists != want {
		t.Fatalf("shared candidate %d exists=%t, want %t", photoID, exists, want)
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
