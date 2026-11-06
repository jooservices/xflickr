package postgres

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	catalogpostgres "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	"github.com/jooservices/xflickr/internal/catalog/app"
	catalogdomain "github.com/jooservices/xflickr/internal/catalog/domain"
	crawlapp "github.com/jooservices/xflickr/internal/crawl/app"
	"github.com/jooservices/xflickr/internal/fakeflickr"
	"github.com/jooservices/xflickr/internal/media/adapters/cdn"
	"github.com/jooservices/xflickr/internal/media/adapters/local"
	mediaapp "github.com/jooservices/xflickr/internal/media/app"
	mediadomain "github.com/jooservices/xflickr/internal/media/domain"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/platform/outbox"
	"github.com/jooservices/xflickr/internal/platform/queue"
	"github.com/jooservices/xflickr/pkg/flickr"
)

type continuationFlickr struct {
	client *flickr.Client
	calls  atomic.Int64
}

func (f *continuationFlickr) GetSizes(ctx context.Context, _ int64, id string) ([]flickr.PhotoSize, error) {
	f.calls.Add(1)
	return f.client.PhotosGetSizes(ctx, flickr.Credentials{APIKey: "fake"}, id)
}

// This test supplies a fetch callback using the existing Fetcher and settlement
// APIs to isolate continuation from the missing production fetch registration.
func TestSizesContinuationPostgresRiverMixedManifest(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	pool := dbtest.Open(t, downloadIntegrationDatabase(t))
	base := time.Now().UnixNano()
	userID := base
	owner := fmt.Sprintf("continuation-%d", base)
	ids := []int64{base + 1, base + 2, base + 3, base + 4}
	server := httptest.NewServer(fakeflickr.NewServer(fakeflickr.Dataset{Photos: []fakeflickr.Photo{{ID: fmt.Sprint(ids[0]), Owner: owner}, {ID: fmt.Sprint(ids[1]), Owner: owner}}}, nil))
	defer server.Close()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,email,password_hash,role) VALUES($1,$2||'@example.test','unused','user')`, userID, owner); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,$1)`, owner); err != nil {
		t.Fatal(err)
	}
	var downloadID int64
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE args->'args'->>'download_id' IN (SELECT id::text FROM downloads WHERE user_id=$1::bigint) OR args->'args'->>'photo_id'=ANY($2::text[])`, fmt.Sprint(userID), []string{fmt.Sprint(ids[1]), fmt.Sprint(ids[2]), fmt.Sprint(ids[3])})
		_, _ = pool.Exec(context.Background(), `DELETE FROM outbox_events WHERE topic=$1 AND key=ANY($2::text[])`, app.SizesResolvedTopic, []string{fmt.Sprint(ids[1]), fmt.Sprint(ids[2]), fmt.Sprint(ids[3])})
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id=ANY($1::bigint[])`, ids)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, owner)
	})
	for i, id := range ids {
		status, serverID := "provisional", "ordinary"
		if i == 0 {
			status = "confirmed"
		}
		if i == 2 {
			serverID = catalogdomain.RestrictedPlaceholderServer
		}
		sizes, _ := json.Marshal([]flickr.PhotoSize{{Label: "Original", Source: server.URL + "/images/" + fmt.Sprint(id) + ".jpg", Width: 64, Height: 64}})
		if _, err := pool.Exec(ctx, `INSERT INTO photos(id,owner_nsid,last_update,visibility,server,sizes,sizes_status,sizes_checked_at,raw,content_hash) VALUES($1,$2,now()-interval '1 hour','public',$3,$4,$5,now(),'{}',decode(repeat('ab',32),'hex'))`, id, owner, serverID, sizes, status); err != nil {
			t.Fatal(err)
		}
	}
	jobs, err := queue.NewRiver(pool, map[string]int{"media": 1, "sync": 1})
	if err != nil {
		t.Fatal(err)
	}
	repo := Downloads{Pool: pool, Jobs: jobs}
	// Create uses real River insertion. A real key is only needed as an identity;
	// the test Flickr client deliberately supplies no live credentials.
	if _, err := pool.Exec(ctx, `INSERT INTO api_keys(id,user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst) VALUES($1,$1,'test','fake','{}',100,2,4)`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO flickr_accounts(id,user_id,key_id,nsid,username,token_sealed,secret_sealed) VALUES($1,$1,$1,$2,'test','{}','{}')`, userID, owner); err != nil {
		t.Fatal(err)
	}
	download := mediadomain.Download{UserID: userID, Kind: mediadomain.DownloadTray, Subject: "tray", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	var candidates []mediaapp.DownloadItemCandidate
	for i, id := range ids {
		state := mediadomain.ItemWaitingSizes
		if i == 0 {
			state = mediadomain.ItemPending
		}
		download.Items = append(download.Items, mediadomain.DownloadItem{PhotoID: id, State: state})
		candidates = append(candidates, mediaapp.DownloadItemCandidate{PhotoID: id, OwnerNSID: owner, SizesConfirmed: i == 0})
	}
	created, err := repo.Create(ctx, download, candidates)
	if err != nil {
		t.Fatal(err)
	}
	downloadID = created.ID
	catalogRepo, err := catalogpostgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	sizesStore := catalogpostgres.ResolvedSizes{Repository: catalogRepo, Pool: pool, Publish: func(ctx context.Context, tx pgx.Tx, e app.SizesResolved) error {
		return outbox.PublishContext(ctx, tx, app.SizesResolvedTopic, fmt.Sprint(e.PhotoID), e)
	}}
	publishErr := errors.New("outbox unavailable")
	failingStore := sizesStore
	failingStore.Publish = func(context.Context, pgx.Tx, app.SizesResolved) error { return publishErr }
	if err := failingStore.SetSizes(ctx, ids[1], []byte(`[]`), catalogdomain.SizesConfirmed, time.Now()); !errors.Is(err, publishErr) {
		t.Fatal(err)
	}
	var rolledBackStatus string
	if err := pool.QueryRow(ctx, `SELECT sizes_status FROM photos WHERE id=$1`, ids[1]).Scan(&rolledBackStatus); err != nil || rolledBackStatus != "provisional" {
		t.Fatalf("sizes transaction rollback: %s %v", rolledBackStatus, err)
	}
	client := &continuationFlickr{client: flickr.NewClient(server.URL+"/services/rest", nil)}
	sizes := crawlapp.Sizes{Store: sizesStore, Flickr: client}
	// Resolve and redeliver before workers start, so exact queue counts and
	// rollback are observable without timing-dependent assertions.
	for _, id := range ids[1:] {
		if err := sizes.Handle(ctx, crawlapp.SizesJob{PhotoID: id, KeyID: userID, Class: "interactive"}); err != nil {
			t.Fatal(err)
		}
	}
	handler := mediaapp.SizesResolvedHandler{Repository: repo}
	for _, id := range ids[1:] {
		var payload []byte
		if err := pool.QueryRow(ctx, `SELECT payload FROM outbox_events WHERE topic=$1 AND key=$2 ORDER BY id DESC LIMIT 1`, app.SizesResolvedTopic, fmt.Sprint(id)).Scan(&payload); err != nil {
			t.Fatal(err)
		}
		// An insertion failure must roll back both pending state and River work.
		if id == ids[1] {
			err := (mediaapp.SizesResolvedHandler{Repository: Downloads{Pool: pool, Jobs: failingQueue{}}}).Handle(ctx, payload)
			if err == nil {
				t.Fatal("queue failure committed")
			}
			var state string
			if err := pool.QueryRow(ctx, `SELECT state FROM download_items WHERE download_id=$1 AND photo_id=$2`, downloadID, id).Scan(&state); err != nil || state != "waiting_sizes" {
				t.Fatalf("rollback state=%s err=%v", state, err)
			}
		}
		for range 2 {
			if err := handler.Handle(ctx, payload); err != nil {
				t.Fatal(err)
			}
		}
	}
	unavailable := mediadomain.Download{UserID: userID, Kind: mediadomain.DownloadTray, Subject: "tray", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), Items: download.Items[2:]}
	allFailed, err := repo.Create(ctx, unavailable, candidates[2:])
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids[2:] {
		payload, _ := json.Marshal(app.SizesResolved{PhotoID: id, Status: "unavailable"})
		for range 2 {
			if err := handler.Handle(ctx, payload); err != nil {
				t.Fatal(err)
			}
		}
	}
	var packJobs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE args->>'kind'='media.pack' AND args->'args'->>'download_id'=$1`, fmt.Sprint(allFailed.ID)).Scan(&packJobs); err != nil || packJobs != 1 {
		t.Fatalf("all unavailable pack jobs=%d err=%v", packJobs, err)
	}
	var fetchJobs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE args->>'kind'='media.fetch' AND args->'args'->>'download_id'=$1`, fmt.Sprint(downloadID)).Scan(&fetchJobs); err != nil || fetchJobs != 2 {
		t.Fatalf("fetch jobs=%d err=%v", fetchJobs, err)
	}
	if client.calls.Load() != 2 {
		t.Fatalf("Flickr calls=%d; placeholder must not call Flickr", client.calls.Load())
	}
	blobs, err := local.New(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	fetcher := &mediaapp.Fetcher{Blobs: blobs, HTTP: cdn.New(nil)}
	if err := jobs.Register("media.fetch", func(ctx context.Context, job queue.Job) error {
		var args struct {
			DownloadID int64 `json:"download_id"`
			PhotoID    int64 `json:"photo_id"`
		}
		if err := json.Unmarshal(job.Args, &args); err != nil {
			return err
		}
		if args.DownloadID != downloadID {
			return errors.New("unexpected fixture job")
		}
		key, checksum, size, err := fetcher.Fetch(ctx, server.URL+"/images/"+fmt.Sprint(args.PhotoID)+".jpg")
		if err != nil {
			return err
		}
		hash, err := hex.DecodeString(checksum)
		if err != nil {
			return err
		}
		asset, err := catalogRepo.CreateAsset(ctx, catalogdomain.Asset{PhotoID: args.PhotoID, SizeLabel: "Original", SHA256: hash, Bytes: size, BlobKey: key, ExpiresAt: time.Now().Add(time.Hour)})
		if err != nil {
			return err
		}
		return repo.SettleItem(ctx, args.DownloadID, args.PhotoID, "ready", &asset.ID, "")
	}); err != nil {
		t.Fatal(err)
	}
	packer := mediaapp.Packer{Blobs: blobs, Repo: repo}
	if err := jobs.Register("media.pack", func(ctx context.Context, job queue.Job) error {
		args, err := mediaapp.ParsePackJob(job.Args)
		if err != nil {
			return err
		}
		return packer.Pack(ctx, args.DownloadID)
	}); err != nil {
		t.Fatal(err)
	}
	if err := jobs.Register("photo.sizes", func(ctx context.Context, job queue.Job) error {
		args, err := crawlapp.ParseSizesJob(job.Args)
		if err != nil {
			return err
		}
		return sizes.Handle(ctx, args)
	}); err != nil {
		t.Fatal(err)
	}
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, 2)
	go func() { done <- (queue.Worker{Queue: jobs}).Run(runCtx) }()
	go func() {
		done <- outbox.NewConsumer(pool).Consume(runCtx, app.SizesResolvedTopic, func(ctx context.Context, e outbox.Event) error { return handler.Handle(ctx, e.Payload) })
	}()
	defer func() {
		stop()
		for range 2 {
			<-done
		}
	}()
	for {
		var status string
		if err := pool.QueryRow(ctx, `SELECT status FROM downloads WHERE id=$1`, downloadID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status == "ready_with_errors" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("mixed download did not finish")
		case <-time.After(20 * time.Millisecond):
		}
	}
	for {
		var status string
		if err := pool.QueryRow(ctx, `SELECT status FROM downloads WHERE id=$1`, allFailed.ID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status == "ready_with_errors" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("all unavailable download did not finish")
		case <-time.After(20 * time.Millisecond):
		}
	}
	var key string
	if err := pool.QueryRow(ctx, `SELECT blob_key FROM download_parts WHERE download_id=$1 ORDER BY part_number DESC LIMIT 1`, downloadID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	blob, err := blobs.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(blob)
	_ = blob.Close()
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	files := 0
	var manifest [][]string
	for _, entry := range archive.File {
		if entry.Name != "manifest.csv" {
			files++
			continue
		}
		r, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		manifest, err = csv.NewReader(r).ReadAll()
		_ = r.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if files != 2 || len(manifest) != 5 {
		t.Fatalf("files=%d manifest=%v", files, manifest)
	}
	for i, row := range manifest[1:] {
		if row[0] != strconv.FormatInt(ids[i], 10) {
			t.Fatal(manifest)
		}
		if i < 2 {
			if len(row[2]) != 64 || row[3] != "" {
				t.Fatal(row)
			}
		} else if row[3] != "sizes_unavailable" {
			t.Fatal(row)
		}
	}
}
