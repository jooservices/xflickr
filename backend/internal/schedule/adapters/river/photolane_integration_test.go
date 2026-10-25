package river_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/queue"
	quotadomain "github.com/jooservices/xflickr/internal/quota/domain"
	scheduleriver "github.com/jooservices/xflickr/internal/schedule/adapters/river"
	"github.com/jooservices/xflickr/internal/schedule/app"
)

func TestPhotoEnqueuerDedupesReadyJobs(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	base := time.Now().UnixNano() % 1_000_000_000_000
	photo := base*10 + 1
	testQueue := fmt.Sprintf("sizes_%d", base)
	dedupe := func(id int64) string { return app.PhotoDedupeKey(id, app.PhotoFacet) }
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE args->'args'->>'dedupe_key' = ANY($1)`, []string{dedupe(photo), dedupe(photo + 1)})
	})
	adapter, err := queue.NewRiver(pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	enqueuer := scheduleriver.PhotoEnqueuer{Pool: pool, Jobs: adapter, ReadyCap: 1, Options: queue.Options{Queue: testQueue, MaxAttempts: 3}}
	job := func(id int64) app.PhotoJob {
		return app.PhotoJob{PhotoID: id, OwnerNSID: "owner", Facet: app.PhotoFacet, KeyID: 7, Class: quotadomain.Photo, DedupeKey: dedupe(id)}
	}
	var inserted atomic.Int32
	var wait sync.WaitGroup
	for i := 0; i < 8; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			ok, err := enqueuer.EnqueuePhoto(ctx, job(photo))
			if err != nil {
				t.Error(err)
			}
			if ok {
				inserted.Add(1)
			}
		}()
	}
	wait.Wait()
	if inserted.Load() != 1 {
		t.Fatalf("concurrent EnqueuePhoto inserted %d jobs, want 1", inserted.Load())
	}
	if ok, err := enqueuer.EnqueuePhoto(ctx, job(photo+1)); err != nil || ok {
		t.Fatalf("EnqueuePhoto(over ready cap) = %v, %v, want false", ok, err)
	}
	var count int
	var class string
	if err := pool.QueryRow(ctx, `SELECT count(*), max(args->'args'->>'class') FROM river_job WHERE args->'args'->>'dedupe_key' = $1`, dedupe(photo)).Scan(&count, &class); err != nil {
		t.Fatal(err)
	}
	if count != 1 || class != "photo" {
		t.Fatalf("river rows = %d class %q, want one photo job", count, class)
	}
	if _, err := pool.Exec(ctx, `UPDATE river_job SET state='completed', finalized_at=now() WHERE args->'args'->>'dedupe_key' = $1`, dedupe(photo)); err != nil {
		t.Fatal(err)
	}
	if ok, err := enqueuer.EnqueuePhoto(ctx, job(photo+1)); err != nil || !ok {
		t.Fatalf("EnqueuePhoto(after capacity frees) = %v, %v, want true", ok, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE river_job SET state='completed', finalized_at=now() WHERE args->'args'->>'dedupe_key' = $1`, dedupe(photo+1)); err != nil {
		t.Fatal(err)
	}
	if ok, err := enqueuer.EnqueuePhoto(ctx, job(photo)); err != nil || !ok {
		t.Fatalf("EnqueuePhoto(after completion) = %v, %v, want true", ok, err)
	}
}
