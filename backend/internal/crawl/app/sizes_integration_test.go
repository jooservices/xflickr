package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	catalogpostgres "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/platform/queue"
	quotaapp "github.com/jooservices/xflickr/internal/quota/app"
	quotadomain "github.com/jooservices/xflickr/internal/quota/domain"
	scheduleriver "github.com/jooservices/xflickr/internal/schedule/adapters/river"
	scheduleapp "github.com/jooservices/xflickr/internal/schedule/app"
	"github.com/jooservices/xflickr/pkg/flickr"
)

func TestSizesRiverHandlerWithPostgresAndFakeFlickr(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := dbtest.Open(t, sizesIntegrationDatabase(t))
	repository, err := catalogpostgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}

	base := time.Now().UnixNano()
	testQueue := fmt.Sprintf("sizes_%d", base)
	photoIDs := []int64{base, base + 1, base + 2, base + 3}
	owner := fmt.Sprintf("sizes-integration-%d@example.invalid", base)
	if _, err := pool.Exec(ctx, `INSERT INTO people (nsid, username) VALUES ($1, $1)`, owner); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'test','user') RETURNING id`, owner+"@integration.invalid").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via) VALUES($1,$2,0,'integration')`, userID, owner); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE args->'args'->>'photo_id' = ANY($1)`, []string{fmt.Sprint(photoIDs[0]), fmt.Sprint(photoIDs[1]), fmt.Sprint(photoIDs[2]), fmt.Sprint(photoIDs[3])})
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE owner_nsid=$1`, owner)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, owner)
	})

	checkedAt := time.Now().UTC().Truncate(time.Microsecond)
	lastUpdate := checkedAt.Add(-time.Hour)
	insertPhoto := func(id int64, status catalog.SizesStatus, checked *time.Time) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO photos (id, owner_nsid, last_update, visibility, sizes, sizes_status, sizes_checked_at, raw, content_hash)
			VALUES ($1,$2,$3,'public','[]'::jsonb,$4,$5,'{}',decode(repeat('ab',32),'hex'))`, id, owner, lastUpdate, string(status), checked); err != nil {
			t.Fatal(err)
		}
	}
	insertPhoto(photoIDs[0], catalog.SizesProvisional, nil)

	var mu sync.Mutex
	calls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse fake Flickr request: %v", err)
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if got := r.Form.Get("method"); got != "flickr.photos.getSizes" {
			t.Errorf("Flickr method = %q, want flickr.photos.getSizes", got)
		}
		photoID := r.Form.Get("photo_id")
		mu.Lock()
		calls[photoID]++
		attempt := calls[photoID]
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if photoID == fmt.Sprint(photoIDs[2]) {
			_, _ = w.Write([]byte(`{"stat":"fail","code":1,"message":"Photo not found"}`))
			return
		}
		if photoID == fmt.Sprint(photoIDs[3]) && attempt == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"stat":"ok","sizes":{"size":[{"label":"Original","width":100,"height":80,"source":"https://example.test/original","media":"photo"}]}}`))
	}))
	defer server.Close()
	flickrClient := flickr.NewClient(server.URL, server.Client(), flickr.EventObserverFunc(func(string, map[string]any) {}))
	client := riverSizesClient{client: flickrClient}
	handler := Sizes{Store: repository, Flickr: client, Now: func() time.Time { return checkedAt }}
	adapter, err := queue.NewRiver(pool, map[string]int{testQueue: 1})
	if err != nil {
		t.Fatal(err)
	}
	processed := make(chan error, 2*len(photoIDs))
	if err := adapter.RegisterWithOptions(scheduleriver.PhotoJobKind, func(ctx context.Context, job queue.Job) error {
		args, err := ParseSizesJob(job.Args)
		if err == nil {
			err = handler.Handle(ctx, args)
		}
		processed <- err
		return err
	}, queue.Options{Queue: testQueue, MaxAttempts: 2, Backoff: 5 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := adapter.Stop(context.Background()); err != nil {
			t.Errorf("stop River adapter: %v", err)
		}
		<-adapter.Stopped()
	})
	enqueuer := scheduleriver.PhotoEnqueuer{Pool: pool, Jobs: adapter, ReadyCap: 2, Options: queue.Options{Queue: testQueue, MaxAttempts: 2, Backoff: 5 * time.Millisecond}}
	lane := scheduleapp.PhotoLane{Selector: catalogpostgres.PhotoLane{Pool: pool}, Keys: integrationPhotoKeys{}, Queue: enqueuer}

	enqueue := func(photoID int64) {
		t.Helper()
		args, err := json.Marshal(SizesJob{PhotoID: photoID, KeyID: 99, Class: string(quotadomain.Photo)})
		if err != nil {
			t.Fatal(err)
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := adapter.Enqueue(ctx, tx, queue.Job{Kind: scheduleriver.PhotoJobKind, Args: args}, queue.Options{Queue: testQueue, MaxAttempts: 2, Backoff: 5 * time.Millisecond}); err != nil {
			_ = tx.Rollback(context.Background())
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	waitJob := func(wantErr bool) {
		t.Helper()
		select {
		case err := <-processed:
			if (err != nil) != wantErr {
				t.Fatalf("photo.sizes result = %v, want error=%t", err, wantErr)
			}
		case <-ctx.Done():
			t.Fatalf("wait for photo.sizes job: %v", ctx.Err())
		}
	}

	ids, err := lane.Next(ctx, 1)
	if err != nil || len(ids) != 1 || ids[0] != photoIDs[0] {
		t.Fatalf("photo lane Next() = %v, %v; want provisional photo %d", ids, err, photoIDs[0])
	}
	waitJob(false)
	var storedSizes []byte
	var storedStatus string
	var storedChecked time.Time
	if err := pool.QueryRow(ctx, `SELECT sizes, sizes_status, sizes_checked_at FROM photos WHERE id=$1`, photoIDs[0]).Scan(&storedSizes, &storedStatus, &storedChecked); err != nil {
		t.Fatal(err)
	}
	var sizes []flickr.PhotoSize
	if err := json.Unmarshal(storedSizes, &sizes); err != nil || len(sizes) != 1 || sizes[0].Label != "Original" {
		t.Fatalf("stored sizes = %s, %v", storedSizes, err)
	}
	if storedStatus != string(catalog.SizesConfirmed) || !storedChecked.Equal(checkedAt) {
		t.Fatalf("stored outcome = %s at %s", storedStatus, storedChecked)
	}
	if ids, err := lane.Next(ctx, 1); err != nil || len(ids) != 0 {
		t.Fatalf("photo lane after confirmation = %v, %v; want no work", ids, err)
	}
	unchangedChecked := lastUpdate
	insertPhoto(photoIDs[1], catalog.SizesConfirmed, &unchangedChecked)
	staleChecked := lastUpdate.Add(-time.Minute)
	insertPhoto(photoIDs[2], catalog.SizesConfirmed, &staleChecked)

	enqueue(photoIDs[1])
	waitJob(false)
	enqueue(photoIDs[2])
	waitJob(false)
	insertPhoto(photoIDs[3], catalog.SizesProvisional, nil)
	enqueue(photoIDs[3])
	waitJob(true)
	waitJob(false)
	mu.Lock()
	confirmedCalls, skippedCalls, missingCalls, transientCalls := calls[fmt.Sprint(photoIDs[0])], calls[fmt.Sprint(photoIDs[1])], calls[fmt.Sprint(photoIDs[2])], calls[fmt.Sprint(photoIDs[3])]
	mu.Unlock()
	if confirmedCalls != 1 || skippedCalls != 0 || missingCalls != 1 || transientCalls != 2 {
		t.Fatalf("fake Flickr calls: confirmed=%d skipped=%d stale-missing=%d transient-retry=%d", confirmedCalls, skippedCalls, missingCalls, transientCalls)
	}
	if err := pool.QueryRow(ctx, `SELECT sizes, sizes_status, sizes_checked_at FROM photos WHERE id=$1`, photoIDs[2]).Scan(&storedSizes, &storedStatus, &storedChecked); err != nil {
		t.Fatal(err)
	}
	if string(storedSizes) != `[]` || storedStatus != string(catalog.SizesUnavailable) || !storedChecked.Equal(checkedAt) {
		t.Fatalf("stale confirmed not-found outcome = %s, %s at %s", storedSizes, storedStatus, storedChecked)
	}
	if err := pool.QueryRow(ctx, `SELECT sizes_status FROM photos WHERE id=$1`, photoIDs[3]).Scan(&storedStatus); err != nil {
		t.Fatal(err)
	}
	if storedStatus != string(catalog.SizesConfirmed) {
		t.Fatalf("transient size result = %s, want confirmed after retry", storedStatus)
	}
}

type riverSizesClient struct{ client *flickr.Client }

type integrationPhotoKeys struct{}

func (integrationPhotoKeys) Choose(_ context.Context, _ string, class quotadomain.Class) (quotaapp.KeyID, error) {
	if class != quotadomain.Photo {
		return 0, fmt.Errorf("photo lane class = %q", class)
	}
	return 99, nil
}

func (c riverSizesClient) GetSizes(ctx context.Context, _ int64, photoID string) ([]flickr.PhotoSize, error) {
	return c.client.PhotosGetSizes(ctx, flickr.Credentials{APIKey: "integration-key"}, photoID)
}

func (c riverSizesClient) GetSizesClass(ctx context.Context, keyID int64, photoID string, class quotadomain.Class) ([]flickr.PhotoSize, error) {
	if class != quotadomain.Photo {
		return nil, fmt.Errorf("sizes quota class = %q", class)
	}
	return c.GetSizes(ctx, keyID, photoID)
}

func sizesIntegrationDatabase(t *testing.T) config.DBConfig {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate sizes integration test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../"))
	dbConfig, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatalf("load test database settings: %v", err)
	}
	if err := dbtest.ValidateDatabaseName(dbConfig.Name); err != nil {
		t.Fatal(err)
	}
	return dbConfig
}
