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
	scheduleriver "github.com/jooservices/xflickr/internal/schedule/adapters/river"
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
	owner := fmt.Sprintf("sizes-integration-%d@example.invalid", base)
	if _, err := pool.Exec(ctx, `INSERT INTO people (nsid, username) VALUES ($1, $1)`, owner); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE owner_nsid=$1`, owner)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, owner)
	})

	checkedAt := time.Now().UTC().Truncate(time.Microsecond)
	lastUpdate := checkedAt.Add(-time.Hour)
	photoIDs := []int64{base, base + 1, base + 2}
	insertPhoto := func(id int64, status catalog.SizesStatus, checked *time.Time) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO photos (id, owner_nsid, last_update, visibility, sizes, sizes_status, sizes_checked_at, raw, content_hash)
			VALUES ($1,$2,$3,'public','[]'::jsonb,$4,$5,'{}',decode(repeat('ab',32),'hex'))`, id, owner, lastUpdate, string(status), checked); err != nil {
			t.Fatal(err)
		}
	}
	insertPhoto(photoIDs[0], catalog.SizesProvisional, nil)
	unchangedChecked := lastUpdate
	insertPhoto(photoIDs[1], catalog.SizesConfirmed, &unchangedChecked)
	staleChecked := lastUpdate.Add(-time.Minute)
	insertPhoto(photoIDs[2], catalog.SizesConfirmed, &staleChecked)

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
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if photoID == fmt.Sprint(photoIDs[2]) {
			_, _ = w.Write([]byte(`{"stat":"fail","code":1,"message":"Photo not found"}`))
			return
		}
		_, _ = w.Write([]byte(`{"stat":"ok","sizes":{"size":[{"label":"Original","width":100,"height":80,"source":"https://example.test/original","media":"photo"}]}}`))
	}))
	defer server.Close()
	flickrClient := flickr.NewClient(server.URL, server.Client(), flickr.EventObserverFunc(func(string, map[string]any) {}))
	client := riverSizesClient{client: flickrClient}
	handler := Sizes{Store: repository, Flickr: client, Now: func() time.Time { return checkedAt }}
	adapter, err := queue.NewRiver(pool, map[string]int{scheduleriver.PhotoQueue: 1})
	if err != nil {
		t.Fatal(err)
	}
	processed := make(chan error, len(photoIDs))
	if err := adapter.RegisterWithOptions(scheduleriver.PhotoJobKind, func(ctx context.Context, job queue.Job) error {
		args, err := ParseSizesJob(job.Args)
		if err == nil {
			err = handler.Handle(ctx, args)
		}
		processed <- err
		return err
	}, queue.Options{Queue: scheduleriver.PhotoQueue, MaxAttempts: 1}); err != nil {
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

	enqueue := func(photoID int64) {
		t.Helper()
		args, err := json.Marshal(SizesJob{PhotoID: photoID, KeyID: 99})
		if err != nil {
			t.Fatal(err)
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := adapter.Enqueue(ctx, tx, queue.Job{Kind: scheduleriver.PhotoJobKind, Args: args}, queue.Options{Queue: scheduleriver.PhotoQueue, MaxAttempts: 1}); err != nil {
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

	enqueue(photoIDs[0])
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

	enqueue(photoIDs[1])
	waitJob(false)
	enqueue(photoIDs[2])
	waitJob(false)
	mu.Lock()
	confirmedCalls, skippedCalls, missingCalls := calls[fmt.Sprint(photoIDs[0])], calls[fmt.Sprint(photoIDs[1])], calls[fmt.Sprint(photoIDs[2])]
	mu.Unlock()
	if confirmedCalls != 1 || skippedCalls != 0 || missingCalls != 1 {
		t.Fatalf("fake Flickr calls: confirmed=%d skipped=%d stale-missing=%d", confirmedCalls, skippedCalls, missingCalls)
	}
	if err := pool.QueryRow(ctx, `SELECT sizes, sizes_status, sizes_checked_at FROM photos WHERE id=$1`, photoIDs[2]).Scan(&storedSizes, &storedStatus, &storedChecked); err != nil {
		t.Fatal(err)
	}
	if string(storedSizes) != `[]` || storedStatus != string(catalog.SizesUnavailable) || !storedChecked.Equal(checkedAt) {
		t.Fatalf("stale confirmed not-found outcome = %s, %s at %s", storedSizes, storedStatus, storedChecked)
	}
}

type riverSizesClient struct{ client *flickr.Client }

func (c riverSizesClient) GetSizes(ctx context.Context, _ int64, photoID string) ([]flickr.PhotoSize, error) {
	return c.client.PhotosGetSizes(ctx, flickr.Credentials{APIKey: "integration-key"}, photoID)
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
