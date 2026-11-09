package postgres_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/platform/ops"
	opspostgres "github.com/jooservices/xflickr/internal/platform/ops/adapters/postgres"
)

func openOpsIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../.."))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return dbtest.Open(t, cfg)
}

func TestOnlyOneMonitorRunsAtATime(t *testing.T) {
	pool := openOpsIntegrationPool(t)
	inner := opspostgres.Store{Pool: pool, DB: pool, StatementTimeout: time.Minute}
	lockKey := opspostgres.HealthLockKey + 369
	adapter := opspostgres.PoolAdapter{Pool: pool}
	first := opspostgres.ExclusiveStore{
		Inner: blockingStore{delay: 400 * time.Millisecond, inner: inner},
		Pool:  adapter,
		Key:   lockKey,
	}
	second := opspostgres.ExclusiveStore{Inner: inner, Pool: adapter, Key: lockKey}

	var busy atomic.Int64
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if _, err := first.Snapshot(t.Context()); err != nil {
			t.Errorf("first snapshot: %v", err)
		}
	}()
	time.Sleep(50 * time.Millisecond)
	go func() {
		defer wg.Done()
		_, err := second.Snapshot(t.Context())
		if err == nil {
			t.Error("second snapshot should be busy")
			return
		}
		if !strings.Contains(err.Error(), ops.ErrSnapshotBusy.Error()) {
			t.Errorf("second snapshot err=%v", err)
			return
		}
		busy.Add(1)
	}()
	wg.Wait()
	if busy.Load() != 1 {
		t.Fatal("expected the second monitor to observe the advisory lock")
	}
}

type blockingStore struct {
	delay time.Duration
	inner ops.Store
}

func (b blockingStore) Snapshot(ctx context.Context) (ops.Snapshot, error) {
	timer := time.NewTimer(b.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ops.Snapshot{}, ctx.Err()
	case <-timer.C:
		return b.inner.Snapshot(ctx)
	}
}

func TestCountsNeverScansPhotos(t *testing.T) {
	pool := openOpsIntegrationPool(t)
	ctx := t.Context()
	rows, err := pool.Query(ctx, `EXPLAIN (FORMAT TEXT) SELECT coalesce(round(sum(
  greatest(c.reltuples, 0) * coalesce((
    SELECT s.most_common_freqs[ord.idx]
    FROM pg_stats s
    CROSS JOIN LATERAL unnest(s.most_common_vals::text::text[]) WITH ORDINALITY AS ord(val, idx)
    WHERE s.schemaname = current_schema()
      AND s.tablename = c.relname
      AND s.attname = 'sizes_status'
      AND ord.val = 'confirmed'
    LIMIT 1
  ), 0)
)), 0)::bigint
FROM pg_class c
WHERE c.relkind IN ('r', 'p')
  AND (c.oid = 'photos'::regclass
       OR c.oid IN (SELECT inhrelid FROM pg_inherits WHERE inhparent = 'photos'::regclass))`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plans []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plans = append(plans, line)
	}
	joined := strings.Join(plans, "\n")
	if strings.Contains(joined, "Seq Scan on photos") {
		t.Fatalf("photo sizes estimate must not seq-scan photos:\n%s", joined)
	}

	snap, err := (opspostgres.Store{Pool: pool, DB: pool}).Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.CountsEstimated {
		t.Fatalf("expected estimated counts: %+v", snap)
	}
}

func TestPhotoSizesPendingUsesPartialIndex(t *testing.T) {
	pool := openOpsIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()

	suffix := fmt.Sprintf("plan%d", time.Now().UnixNano())
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,$1) ON CONFLICT DO NOTHING`, suffix); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE owner_nsid=$1`, suffix)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, suffix)
	})

	rowsWanted := int64(5_000)
	if os.Getenv("XFLICKR_LARGE_PLAN_TEST") == "1" {
		rowsWanted = 3_000_000
	}
	startID := time.Now().UnixNano()
	if _, err := pool.Exec(ctx, `
INSERT INTO photos(id, owner_nsid, last_update, visibility, sizes_status, sizes, raw, content_hash)
SELECT $1 + g, $2, now(), 'public', 'provisional', '[]'::jsonb, '{}'::bytea, decode(repeat('aa', 32), 'hex')
FROM generate_series(1, $3) AS g`, startID, suffix, rowsWanted); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE photos`); err != nil {
		t.Fatal(err)
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SET enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	explainSQL := `EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) SELECT count(*) FROM photos
WHERE tombstoned_at IS NULL
  AND (sizes_status NOT IN ('confirmed', 'unavailable')
       OR (sizes_status = 'confirmed' AND sizes_checked_at < last_update))`
	rows, err := conn.Query(ctx, explainSQL)
	if err != nil {
		t.Fatal(err)
	}
	var plans []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		plans = append(plans, line)
	}
	rows.Close()
	joined := strings.Join(plans, "\n")
	t.Logf("photo sizes pending plan (%d rows):\n%s", rowsWanted, joined)
	if !strings.Contains(joined, "photos_sizes_pending_idx") {
		t.Fatalf("expected photos_sizes_pending_idx in plan:\n%s", joined)
	}
	if strings.Contains(joined, "Seq Scan on photos") {
		t.Fatalf("pending count must not seq-scan photos partitions:\n%s", joined)
	}

	snap, err := (opspostgres.Store{Pool: pool, DB: pool}).Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snap.PhotoSizesJobs.Pending < rowsWanted {
		t.Fatalf("pending=%d want at least %d", snap.PhotoSizesJobs.Pending, rowsWanted)
	}
}
