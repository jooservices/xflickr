package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
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
	if snapshot.Photos.Total != 0 || snapshot.CountsComputedAt != nil || !snapshot.Estimated {
		t.Fatalf("missing cached photo counts = %+v, computed_at=%v, estimated=%t", snapshot.Photos, snapshot.CountsComputedAt, snapshot.Estimated)
	}
	if queueStub.calls != 1 || queueStub.job.Kind != app.RefreshJobKind {
		t.Fatalf("missing stats row was not queued once: %+v", queueStub)
	}
	assertRequestPlan(t, ctx, pool, requestDB.statement, userID)
	assertRefreshPlan(t, ctx, pool, userID, 3)
	refresher := app.Refresher{Processor: repository, BatchSize: 3}
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
	assertSharedCandidate(t, ctx, pool, sharedPublicID, false)
	snapshot, err = repository.Load(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Estimated || snapshot.Photos.Total != 0 || snapshot.StarredContacts[1].PhotosCount != 0 || queueStub.calls != 2 {
		t.Fatalf("sharing change did not invalidate the cached snapshot: %+v, queued=%d", snapshot.Photos, queueStub.calls)
	}
	if _, err := pool.Exec(ctx, `UPDATE sharing_settings SET enabled=true WHERE user_id=$1`, otherUserID); err != nil {
		t.Fatal(err)
	}
	assertSharedCandidate(t, ctx, pool, sharedPublicID, true)
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
	if !snapshot.Estimated || snapshot.Photos.Total != 14 || snapshot.CountsComputedAt == nil || queueStub.calls != 3 {
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
	oldComputedAt := *snapshot.CountsComputedAt
	if _, err := refresher.Refresh(ctx, userID); err != nil {
		t.Fatal(err)
	}
	changedPhotoID := baseID + 1
	if _, err := pool.Exec(ctx, `UPDATE photos SET visibility='private' WHERE id=$1`, changedPhotoID); err != nil {
		t.Fatal(err)
	}
	changedSnapshot, err := repository.Load(ctx, userID)
	if err != nil || !changedSnapshot.Estimated || changedSnapshot.Photos.Total != 0 || changedSnapshot.StarredContacts[0].PhotosCount != 0 || !changedSnapshot.CountsComputedAt.Equal(oldComputedAt) {
		t.Fatalf("in-progress refresh published a changed source: %+v, err=%v", changedSnapshot, err)
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
	if err != nil || snapshot.Estimated || snapshot.Photos.Total != 13 {
		t.Fatalf("refresh after source change = %+v estimated=%t err=%v", snapshot.Photos, snapshot.Estimated, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sharing_settings SET enabled=false WHERE user_id=$1`, otherUserID); err != nil {
		t.Fatal(err)
	}
	snapshot, err = repository.Load(ctx, userID)
	if err != nil || !snapshot.Estimated || snapshot.Photos.Total != 0 {
		t.Fatalf("post-refresh permission change did not invalidate snapshot: %+v estimated=%t err=%v", snapshot.Photos, snapshot.Estimated, err)
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
	RelationName string        `json:"Relation Name"`
	CTEName      string        `json:"CTE Name"`
	PlanRows     float64       `json:"Plan Rows"`
	ActualRows   float64       `json:"Actual Rows"`
	ActualLoops  float64       `json:"Actual Loops"`
	Plans        []explainNode `json:"Plans"`
}

func assertRequestPlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, statement string, userID int64) {
	t.Helper()
	if _, err := pool.Exec(ctx, `ANALYZE user_home_photo_candidates`); err != nil {
		t.Fatalf("analyze own photo candidates for request plan: %v", err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE user_home_shared_photo_candidates`); err != nil {
		t.Fatalf("analyze shared photo candidates for request plan: %v", err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE user_home_shared_version`); err != nil {
		t.Fatalf("analyze shared revision for request plan: %v", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin Home request plan transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatalf("prefer indexes for Home request plan: %v", err)
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+statement, userID).Scan(&raw); err != nil {
		t.Fatalf("explain Home request: %v", err)
	}
	var document explainDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode Home request plan: %v", err)
	}
	if len(document) != 1 {
		t.Fatalf("Home request plan has %d roots", len(document))
	}
	scanCount := 0
	var maxScanRows float64
	seenOwnCandidates, seenSharedCandidates := false, false
	var visit func(explainNode)
	visit = func(node explainNode) {
		t.Helper()
		if node.NodeType == "Seq Scan" && node.RelationName != "sharing_settings" &&
			node.RelationName != "user_home_shared_version" {
			t.Errorf("Home request plan contains Seq Scan on %q", node.RelationName)
		}
		boundedOwnCandidates := node.RelationName == "user_home_photo_candidates" && node.PlanRows <= 12
		boundedSharedCandidates := node.RelationName == "user_home_shared_photo_candidates" && node.PlanRows <= 12
		boundedCandidates := node.NodeType == "CTE Scan" && node.CTEName == "newest_photo_candidates" && node.PlanRows <= 24
		singletonVersion := node.RelationName == "user_home_shared_version"
		if node.RelationName == "user_home_photo_candidates" {
			seenOwnCandidates = true
			if node.ActualRows*node.ActualLoops > 12 {
				t.Errorf("Home request read %.0f own candidates, over the 12-row limit", node.ActualRows*node.ActualLoops)
			}
		}
		if node.RelationName == "user_home_shared_photo_candidates" {
			seenSharedCandidates = true
			if node.ActualRows*node.ActualLoops > 12 {
				t.Errorf("Home request read %.0f shared candidates, over the 12-row limit", node.ActualRows*node.ActualLoops)
			}
		}
		if strings.Contains(node.NodeType, "Scan") && node.PlanRows > 500 &&
			!boundedOwnCandidates && !boundedSharedCandidates && !boundedCandidates && !singletonVersion {
			t.Errorf("Home request %s on %q estimates %.0f rows outside its candidate limit", node.NodeType, node.RelationName, node.PlanRows)
		}
		if strings.Contains(node.NodeType, "Scan") {
			scanCount++
			if node.PlanRows > maxScanRows {
				maxScanRows = node.PlanRows
			}
		}
		for _, child := range node.Plans {
			visit(child)
		}
	}
	visit(document[0].Plan)
	if !seenOwnCandidates || !seenSharedCandidates {
		t.Fatalf("Home request plan must visit both candidate sources; own=%t shared=%t", seenOwnCandidates, seenSharedCandidates)
	}
	t.Logf("request EXPLAIN: %d scan nodes; largest estimate %.0f rows", scanCount, maxScanRows)
}

func assertRefreshPlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID int64, batchSize int) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin refresh plan transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatalf("prefer indexes for refresh plan: %v", err)
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+refreshPhotoPageSQL, userID, "", batchSize).Scan(&raw); err != nil {
		t.Fatalf("explain refresh page: %v", err)
	}
	var document explainDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode refresh page plan: %v", err)
	}
	if len(document) != 1 {
		t.Fatalf("refresh page plan has %d roots", len(document))
	}
	seenOwn, seenShared, seenPageLimit := false, false, false
	var visit func(explainNode)
	visit = func(node explainNode) {
		t.Helper()
		if node.NodeType == "Limit" {
			seenPageLimit = true
			if node.ActualRows*node.ActualLoops > float64(batchSize) {
				t.Errorf("refresh limit returned %.0f rows, over configured batch %d", node.ActualRows*node.ActualLoops, batchSize)
			}
		}
		if node.RelationName == "user_home_photo_candidates" {
			seenOwn = true
			if node.NodeType == "Seq Scan" {
				t.Error("refresh plan scans all own candidates")
			}
			if node.ActualRows*node.ActualLoops > float64(batchSize) {
				t.Errorf("refresh read %.0f own candidates, over configured batch %d", node.ActualRows*node.ActualLoops, batchSize)
			}
		}
		if node.RelationName == "user_home_shared_photo_candidates" {
			seenShared = true
			if node.NodeType == "Seq Scan" {
				t.Error("refresh plan scans all shared candidates")
			}
			if node.ActualRows*node.ActualLoops > float64(batchSize) {
				t.Errorf("refresh read %.0f shared candidates, over configured batch %d", node.ActualRows*node.ActualLoops, batchSize)
			}
		}
		for _, child := range node.Plans {
			visit(child)
		}
	}
	visit(document[0].Plan)
	if !seenOwn || !seenShared || !seenPageLimit {
		t.Fatalf("refresh plan bounded indexed sources own=%t shared=%t limit=%t", seenOwn, seenShared, seenPageLimit)
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
