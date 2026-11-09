package postgres_test

import (
	"context"
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
	locker := pgxLockPool{pool: pool}
	first := opspostgres.ExclusiveStore{
		Inner: blockingStore{delay: 400 * time.Millisecond, inner: inner},
		Pool:  locker,
		Key:   lockKey,
	}
	second := opspostgres.ExclusiveStore{Inner: inner, Pool: locker, Key: lockKey}

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

type pgxLockPool struct{ pool *pgxpool.Pool }

func (p pgxLockPool) Acquire(ctx context.Context) (opspostgres.LockConn, error) {
	return p.pool.Acquire(ctx)
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

func TestSnapshotContinuesAfterStatementTimeout(t *testing.T) {
	pool := openOpsIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	// 1ms cancels the first non-trivial statement; SAVEPOINTs must keep the
	// transaction usable so later metrics still load instead of 25P02.
	snap, err := (opspostgres.Store{Pool: pool, DB: pool, StatementTimeout: time.Millisecond}).Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot after timeout must not abort the transaction: %v", err)
	}
	if !snap.Degraded || len(snap.DegradedQueries) == 0 {
		t.Fatalf("expected degraded queries, got %+v", snap)
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
