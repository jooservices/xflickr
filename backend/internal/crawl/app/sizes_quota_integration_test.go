package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	catalogpostgres "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/platform/queue"
	quotadomain "github.com/jooservices/xflickr/internal/quota/domain"
	scheduleriver "github.com/jooservices/xflickr/internal/schedule/adapters/river"
	"github.com/jooservices/xflickr/pkg/flickr"
)

type quotaThenOKClient struct{ calls atomic.Int32 }

func (c *quotaThenOKClient) GetSizes(context.Context, int64, string) ([]flickr.PhotoSize, error) {
	if c.calls.Add(1) == 1 {
		return nil, NewQuotaWaitError(flickr.ErrQuotaExceeded, time.Second)
	}
	return []flickr.PhotoSize{{Label: "Original", Width: 1, Height: 1, Source: "https://example.test/o"}}, nil
}

// TestSizesQuotaExhaustionSnoozesThenConfirms proves an exhausted quota delays
// the job without discarding it, and the job confirms once quota is back.
func TestSizesQuotaExhaustionSnoozesThenConfirms(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	pool := dbtest.Open(t, sizesIntegrationDatabase(t))
	repository, err := catalogpostgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UnixNano()
	owner := fmt.Sprintf("sizes-quota-%d@example.invalid", base)
	testQueue := fmt.Sprintf("sizes_quota_%d", base)
	if _, err := pool.Exec(ctx, `INSERT INTO people (nsid, username) VALUES ($1, $1)`, owner); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE args->'args'->>'photo_id' = $1`, fmt.Sprint(base))
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE owner_nsid=$1`, owner)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, owner)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO photos (id, owner_nsid, last_update, visibility, sizes, sizes_status, raw, content_hash)
		VALUES ($1,$2,now() - interval '1 hour','public','[]'::jsonb,'provisional','{}',decode(repeat('ab',32),'hex'))`, base, owner); err != nil {
		t.Fatal(err)
	}
	client := &quotaThenOKClient{}
	handler := Sizes{Store: repository, Flickr: client}
	adapter, err := queue.NewRiver(pool, map[string]int{testQueue: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.RegisterWithOptions(scheduleriver.PhotoJobKind, func(ctx context.Context, job queue.Job) error {
		args, err := ParseSizesJob(job.Args)
		if err != nil {
			return err
		}
		err = handler.Handle(ctx, args)
		var waiting *QuotaWaitError
		if errors.As(err, &waiting) {
			return queue.Snooze(waiting.Wait)
		}
		return err
	}, queue.Options{Queue: testQueue, MaxAttempts: 1}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = adapter.Stop(context.Background())
		<-adapter.Stopped()
	})
	args, _ := json.Marshal(SizesJob{PhotoID: base, KeyID: 99, Class: string(quotadomain.Photo)})
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Enqueue(ctx, tx, queue.Job{Kind: scheduleriver.PhotoJobKind, Args: args}, queue.Options{Queue: testQueue, MaxAttempts: 1}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for {
		var status string
		if err := pool.QueryRow(ctx, `SELECT sizes_status FROM photos WHERE id=$1`, base).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status == string(catalog.SizesConfirmed) {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("sizes never confirmed after quota reset; calls=%d", client.calls.Load())
		case <-time.After(250 * time.Millisecond):
		}
	}
	var discarded int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE args->'args'->>'photo_id'=$1 AND state='discarded'`, fmt.Sprint(base)).Scan(&discarded); err != nil || discarded != 0 {
		t.Fatalf("discarded jobs = %d, %v", discarded, err)
	}
	if client.calls.Load() != 2 {
		t.Fatalf("Flickr calls = %d, want 2", client.calls.Load())
	}
}
