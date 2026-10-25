package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	catalogdownloadspostgres "github.com/jooservices/xflickr/internal/catalog/adapters/downloads/postgres"
	catalogapp "github.com/jooservices/xflickr/internal/catalog/app"
	"github.com/jooservices/xflickr/internal/media/app"
	mediadomain "github.com/jooservices/xflickr/internal/media/domain"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/platform/queue"
)

func TestCreateAlbumDownloadWithPostgres(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := dbtest.Open(t, downloadIntegrationDatabase(t))
	// Earlier interrupted runs can leave size jobs for fixtures that have
	// already been deleted. Remove only size jobs whose catalog photo is gone.
	if _, err := pool.Exec(ctx, `DELETE FROM river_job r WHERE r.queue='sync' AND r.args->'args'->>'photo_id' IS NOT NULL AND NOT EXISTS (SELECT 1 FROM photos p WHERE p.id=(r.args->'args'->>'photo_id')::bigint)`); err != nil {
		t.Fatal(err)
	}
	jobs := &recordingQueue{}
	base := time.Now().UnixNano()
	userID, keyID, accountID := base, base+1, base+2
	albumID := fmt.Sprintf("album-%d", base)
	photoIDs := []int64{base + 3, base + 4, base + 5}
	owner := fmt.Sprintf("download-%d@example.invalid", base)
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,email,password_hash,role) VALUES($1,$2,'not-used','user')`, userID, owner); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id=ANY($1::bigint[])`, photoIDs)
		_, _ = pool.Exec(context.Background(), `DELETE FROM albums WHERE id=$1`, albumID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, owner)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO api_keys(id,user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,$2,'integration','download-test','{}',100,2,4)`, keyID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO flickr_accounts(id,user_id,key_id,nsid,username,token_sealed,secret_sealed) VALUES($1,$2,$3,$4,'download','{}','{}')`, accountID, userID, keyID, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,$1)`, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO albums(id,owner_nsid,title) VALUES($1,$2,'album')`, albumID, owner); err != nil {
		t.Fatal(err)
	}
	lastUpdate := time.Now().UTC().Truncate(time.Microsecond)
	for i, id := range photoIDs {
		visibility := "public"
		status := "confirmed"
		checked := lastUpdate
		if i == 1 {
			status = "provisional"
		}
		if i == 2 {
			visibility = "friends"
			status = "provisional"
		}
		if _, err := pool.Exec(ctx, `INSERT INTO photos(id,owner_nsid,last_update,visibility,sizes,sizes_status,sizes_checked_at,raw,content_hash) VALUES($1,$2,$3,$4,'[]'::jsonb,$5,$6,'{}',decode(repeat('ab',32),'hex'))`, id, owner, lastUpdate, visibility, status, checked); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO album_photos(album_id,photo_id,position) VALUES($1,$2,$3)`, albumID, id, i); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range photoIDs[:2] {
		if _, err := pool.Exec(ctx, `INSERT INTO observations(user_id,flickr_account_id,record_kind,record_id) VALUES($1,$2,'photo',$3)`, userID, accountID, fmt.Sprint(id)); err != nil {
			t.Fatal(err)
		}
	}
	catalogRepository := catalogdownloadspostgres.Repository{Pool: pool}
	catalog := catalogapp.DownloadCatalog{Photos: catalogRepository}
	if got, err := catalog.Candidates(ctx, string(mediadomain.DownloadContact), owner, nil); err != nil || len(got) != 3 {
		t.Fatalf("contact candidates = %d, %v", len(got), err)
	}
	if got, err := catalog.Candidates(ctx, string(mediadomain.DownloadTray), "", photoIDs); err != nil || len(got) != 3 {
		t.Fatalf("tray candidates = %d, %v", len(got), err)
	}
	if _, err := catalog.Candidates(ctx, "unknown", "", nil); err == nil {
		t.Fatal("accepted unsupported candidate kind")
	}
	service := app.Downloads{Catalog: catalog, Repository: Downloads{Pool: pool, Jobs: jobs}, Now: func() time.Time { return lastUpdate }}
	created, err := service.Create(ctx, app.CreateDownloadInput{UserID: userID, Kind: mediadomain.DownloadAlbum, Subject: albumID})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID <= 0 || len(created.Items) != 2 || created.Skipped != 1 {
		t.Fatalf("created=%+v", created)
	}
	var waiting, pending int
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE state='waiting_sizes'),count(*) FILTER(WHERE state='pending') FROM download_items WHERE download_id=$1`, created.ID).Scan(&waiting, &pending); err != nil {
		t.Fatal(err)
	}
	if waiting != 1 || pending != 1 {
		t.Fatalf("item states waiting=%d pending=%d", waiting, pending)
	}
	if _, _, err := (Downloads{Pool: pool}).PackItems(ctx, created.ID); err == nil {
		t.Fatal("PackItems accepted unsettled items")
	}
	if originals, complete, err := (Downloads{Pool: pool}).PackedOriginals(ctx, created.ID); err != nil || complete || len(originals) != 0 {
		t.Fatalf("PackedOriginals before completion = %v, %v, %v", originals, complete, err)
	}
	if len(jobs.jobs) != 2 {
		t.Fatalf("queued jobs=%+v", jobs.jobs)
	}
	queued := map[string]queue.Options{}
	for i, job := range jobs.jobs {
		queued[job.Kind] = jobs.options[i]
	}
	if queued["photo.sizes"].Queue != "sync" || queued["media.fetch"].Queue != "media" {
		t.Fatalf("queued jobs=%+v options=%+v", jobs.jobs, jobs.options)
	}
	var sizesArgs map[string]any
	for _, job := range jobs.jobs {
		if job.Kind == "photo.sizes" {
			_ = json.Unmarshal(job.Args, &sizesArgs)
		}
	}
	if sizesArgs["key_id"] != float64(keyID) || sizesArgs["class"] != "interactive" {
		t.Fatalf("direct action sizes args = %+v", sizesArgs)
	}
	if _, err := (Downloads{Pool: pool}).Create(ctx, created, nil); err == nil {
		t.Fatal("accepted missing queue")
	}
	failing := failingQueue{}
	failed := mediadomain.Download{UserID: userID, Kind: mediadomain.DownloadAlbum, Subject: albumID, CreatedAt: lastUpdate, ExpiresAt: lastUpdate.Add(time.Hour), Items: []mediadomain.DownloadItem{{PhotoID: photoIDs[0], State: mediadomain.ItemPending}}}
	if _, err := (Downloads{Pool: pool, Jobs: failing}).Create(ctx, failed, []app.DownloadItemCandidate{{PhotoID: photoIDs[0], OwnerNSID: owner, SizesConfirmed: true}}); err == nil {
		t.Fatal("accepted queue insertion failure")
	}
	failed.Items[0] = mediadomain.DownloadItem{PhotoID: photoIDs[1], State: mediadomain.ItemWaitingSizes}
	if _, err := (Downloads{Pool: pool, Jobs: failing}).Create(ctx, failed, []app.DownloadItemCandidate{{PhotoID: photoIDs[1], OwnerNSID: owner}}); err == nil {
		t.Fatal("accepted sizes queue insertion failure")
	}
	if _, err := pool.Exec(ctx, `UPDATE api_keys SET status='revoked' WHERE id=$1`, keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := (Downloads{Pool: pool, Jobs: jobs}).Create(ctx, failed, []app.DownloadItemCandidate{{PhotoID: photoIDs[1], OwnerNSID: owner}}); !errors.Is(err, app.ErrNoFlickrKey) {
		t.Fatalf("missing valid user key error = %v", err)
	}
	packJobsBefore := countJobs(jobs.jobs, "media.pack")
	if err := (Downloads{Pool: pool, Jobs: jobs}).SettleItem(ctx, created.ID, photoIDs[0], "failed", nil, "first failed"); err != nil {
		t.Fatal(err)
	}
	if countJobs(jobs.jobs, "media.pack") != packJobsBefore {
		t.Fatal("enqueued pack job before the final item settled")
	}
	if err := (Downloads{Pool: pool, Jobs: failingQueue{}}).SettleItem(ctx, created.ID, photoIDs[1], "failed", nil, "second failed"); err == nil {
		t.Fatal("settlement did not roll back when pack enqueue failed")
	}
	if err := (Downloads{Pool: pool, Jobs: jobs}).SettleItem(ctx, created.ID, photoIDs[1], "failed", nil, "second failed"); err != nil {
		t.Fatal(err)
	}
	if err := (Downloads{Pool: pool, Jobs: jobs}).SettleItem(ctx, created.ID, photoIDs[1], "failed", nil, "second failed"); err != nil {
		t.Fatalf("repeated terminal settlement failed: %v", err)
	}
	if countJobs(jobs.jobs, "media.pack") != packJobsBefore+1 {
		t.Fatalf("pack jobs after final settlement = %d, want %d", countJobs(jobs.jobs, "media.pack"), packJobsBefore+1)
	}
}

func countJobs(jobs []queue.Job, kind string) int {
	count := 0
	for _, job := range jobs {
		if job.Kind == kind {
			count++
		}
	}
	return count
}

type recordingQueue struct {
	jobs    []queue.Job
	options []queue.Options
}

func (q *recordingQueue) Enqueue(_ context.Context, tx pgx.Tx, job queue.Job, options queue.Options) error {
	if tx == nil {
		return errors.New("transaction required")
	}
	q.jobs = append(q.jobs, job)
	q.options = append(q.options, options)
	return nil
}

func (*recordingQueue) Register(string, queue.Handler) error { return nil }

type failingQueue struct{}

func (failingQueue) Enqueue(context.Context, pgx.Tx, queue.Job, queue.Options) error {
	return errors.New("queue unavailable")
}
func (failingQueue) Register(string, queue.Handler) error { return nil }

func downloadIntegrationDatabase(t *testing.T) config.DBConfig {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate download integration test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatalf("load test database settings: %v", err)
	}
	if err := dbtest.ValidateDatabaseName(cfg.Name); err != nil {
		t.Fatal(err)
	}
	return cfg
}
