package postgres_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/platform/metrics/adapters/postgres"
	"github.com/jooservices/xflickr/internal/platform/metrics/domain"
)

func TestWriteBatchIsIdempotent(t *testing.T) {
	pool := integrationPool(t)
	cleanupMetrics(t, pool)
	store := postgres.Store{Pool: pool}
	at := time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC)
	sample := domain.Sample{
		Name:   domain.MetricProcessGoroutines,
		Labels: map[string]string{"role": "api", "node": "n1"},
		At:     at,
		Value:  10,
	}
	if err := store.WriteBatch(t.Context(), []domain.Sample{sample}); err != nil {
		t.Fatal(err)
	}
	sample.Value = 12
	if err := store.WriteBatch(t.Context(), []domain.Sample{sample}); err != nil {
		t.Fatal(err)
	}
	var count int
	var value float64
	if err := pool.QueryRow(t.Context(), `SELECT count(*), max(value) FROM metric_samples WHERE name = $1`, domain.MetricProcessGoroutines).Scan(&count, &value); err != nil {
		t.Fatal(err)
	}
	if count != 1 || value != 12 {
		t.Fatalf("count=%d value=%v want 1/12", count, value)
	}
}

func TestWriteBatchDeduplicatesSamplesWithTheSameKey(t *testing.T) {
	pool := integrationPool(t)
	cleanupMetrics(t, pool)
	store := postgres.Store{Pool: pool}
	at := time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC)
	labels := map[string]string{"role": "api", "node": "n1", "suite": "duplicate-key"}
	samples := []domain.Sample{
		{Name: domain.MetricProcessGoroutines, Labels: labels, At: at, Value: 10},
		{Name: domain.MetricProcessGoroutines, Labels: labels, At: at, Value: 12},
	}
	if err := store.WriteBatch(t.Context(), samples); err != nil {
		t.Fatal(err)
	}
	var count int
	var value float64
	if err := pool.QueryRow(t.Context(), `SELECT count(*), max(value) FROM metric_samples WHERE name = $1 AND labels->>'suite' = $2`, domain.MetricProcessGoroutines, "duplicate-key").Scan(&count, &value); err != nil {
		t.Fatal(err)
	}
	if count != 1 || value != 12 {
		t.Fatalf("count=%d value=%v want 1/12", count, value)
	}
}

func TestBatchOf1200SamplesChunks(t *testing.T) {
	pool := integrationPool(t)
	cleanupMetrics(t, pool)
	store := postgres.Store{Pool: pool}
	at := time.Date(2026, 11, 9, 6, 1, 0, 0, time.UTC)
	const suite = "batch-1200"
	samples := make([]domain.Sample, 0, 1200)
	for i := 0; i < 1200; i++ {
		samples = append(samples, domain.Sample{
			Name:   domain.MetricCollectorErrors,
			Labels: map[string]string{"role": "api", "node": "n1", "suite": suite, "i": strconv.Itoa(i)},
			At:     at,
			Value:  float64(i),
		})
	}
	if err := store.WriteBatch(t.Context(), samples); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM metric_samples WHERE name = $1 AND labels->>'suite' = $2`, domain.MetricCollectorErrors, suite).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1200 {
		t.Fatalf("count=%d want 1200", count)
	}
}

func TestRollupThenPurge(t *testing.T) {
	pool := integrationPool(t)
	cleanupMetrics(t, pool)
	store := postgres.Store{Pool: pool}
	hour := time.Date(2026, 11, 9, 5, 0, 0, 0, time.UTC)
	const suite = "rollup-purge"
	values := []float64{10, 20, 30, 40}
	samples := make([]domain.Sample, 0, len(values))
	for i, value := range values {
		samples = append(samples, domain.Sample{
			Name:   domain.MetricProcessCPUPercent,
			Labels: map[string]string{"role": "api", "node": "n1", "suite": suite},
			At:     hour.Add(time.Duration(i) * time.Minute),
			Value:  value,
		})
	}
	if err := store.WriteBatch(t.Context(), samples); err != nil {
		t.Fatal(err)
	}
	through := time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC)
	rolled, err := store.Rollup(t.Context(), hour, through)
	if err != nil {
		t.Fatal(err)
	}
	if rolled < 1 {
		t.Fatalf("rolled=%d want >= 1", rolled)
	}
	var avg, min, max, p95 float64
	var n int
	if err := pool.QueryRow(t.Context(), `SELECT avg, min, max, p95, samples FROM metric_rollups WHERE name = $1 AND labels->>'suite' = $2`, domain.MetricProcessCPUPercent, suite).Scan(&avg, &min, &max, &p95, &n); err != nil {
		t.Fatal(err)
	}
	if n != 4 || avg != 25 || min != 10 || max != 40 {
		t.Fatalf("rollup avg=%v min=%v max=%v samples=%d", avg, min, max, n)
	}
	// percentile_cont(0.95) of 10,20,30,40 is 38.5.
	if p95 < 37 || p95 > 40 {
		t.Fatalf("p95=%v unexpected", p95)
	}
	purged, err := store.Purge(t.Context(), through, 100)
	if err != nil {
		t.Fatal(err)
	}
	if purged < 4 {
		t.Fatalf("purged=%d want >= 4", purged)
	}
	var remaining int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM metric_samples WHERE labels->>'suite' = $1`, suite).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("remaining=%d", remaining)
	}
	var rollups int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM metric_rollups WHERE labels->>'suite' = $1`, suite).Scan(&rollups); err != nil {
		t.Fatal(err)
	}
	if rollups != 1 {
		t.Fatalf("rollups=%d", rollups)
	}
}

func TestRollupDoesNotReplaceExpiredHourAfterBoundedPurge(t *testing.T) {
	pool := integrationPool(t)
	cleanupMetrics(t, pool)
	store := postgres.Store{Pool: pool}
	hour := time.Date(2026, 11, 8, 4, 0, 0, 0, time.UTC)
	const suite = "partial-purge-rollup"
	samples := []domain.Sample{
		{Name: domain.MetricProcessCPUPercent, Labels: map[string]string{"role": "api", "suite": suite}, At: hour, Value: 10},
		{Name: domain.MetricProcessCPUPercent, Labels: map[string]string{"role": "api", "suite": suite}, At: hour.Add(time.Minute), Value: 20},
	}
	if err := store.WriteBatch(t.Context(), samples); err != nil {
		t.Fatal(err)
	}
	cutoff := time.Date(2026, 11, 8, 5, 0, 0, 0, time.UTC)
	firstThrough := time.Date(2026, 11, 8, 5, 0, 0, 0, time.UTC)
	if _, err := store.Rollup(t.Context(), hour, firstThrough); err != nil {
		t.Fatal(err)
	}
	if purged, err := store.Purge(t.Context(), cutoff, 1); err != nil || purged != 1 {
		t.Fatalf("Purge=%d err=%v want 1", purged, err)
	}
	secondThrough := time.Date(2026, 11, 8, 6, 0, 0, 0, time.UTC)
	if _, err := store.Rollup(t.Context(), cutoff, secondThrough); err != nil {
		t.Fatal(err)
	}
	var avg float64
	var count int
	if err := pool.QueryRow(t.Context(), `SELECT avg, samples FROM metric_rollups WHERE name = $1 AND labels->>'suite' = $2`, domain.MetricProcessCPUPercent, suite).Scan(&avg, &count); err != nil {
		t.Fatal(err)
	}
	if avg != 15 || count != 2 {
		t.Fatalf("expired hour rollup avg=%v samples=%d want 15/2", avg, count)
	}
}

func TestPurgeIsBounded(t *testing.T) {
	pool := integrationPool(t)
	cleanupMetrics(t, pool)
	store := postgres.Store{Pool: pool}
	at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	const suite = "purge-bounded"
	samples := make([]domain.Sample, 0, 10)
	for i := 0; i < 10; i++ {
		samples = append(samples, domain.Sample{
			Name:   domain.MetricOutboxBacklog,
			Labels: map[string]string{"role": "scheduler", "node": "n1", "suite": suite, "i": strconv.Itoa(i)},
			At:     at.Add(time.Duration(i) * time.Minute),
			Value:  float64(i),
		})
	}
	if err := store.WriteBatch(t.Context(), samples); err != nil {
		t.Fatal(err)
	}
	before := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	var beforeCount int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM metric_samples WHERE labels->>'suite' = $1 AND bucket < $2`, suite, before).Scan(&beforeCount); err != nil {
		t.Fatal(err)
	}
	if beforeCount != 10 {
		t.Fatalf("suite rows=%d want 10", beforeCount)
	}
	purged, err := store.Purge(t.Context(), before, 3)
	if err != nil {
		t.Fatal(err)
	}
	if purged != 3 {
		t.Fatalf("purged=%d want 3", purged)
	}
	var remaining int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM metric_samples WHERE labels->>'suite' = $1`, suite).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 7 {
		t.Fatalf("remaining=%d want 7", remaining)
	}
}

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../.."))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if err := dbtest.ValidateDatabaseName(cfg.Name); err != nil {
		t.Fatal(err)
	}
	return dbtest.Open(t, cfg)
}

func cleanupMetrics(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	// Serialize writers that share the worktree database.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire lock connection: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(736800368)`); err != nil {
		conn.Release()
		t.Fatalf("advisory lock: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		defer conn.Release()
		for _, query := range []string{`DELETE FROM metric_samples`, `DELETE FROM metric_rollups`} {
			if _, err := conn.Exec(cleanupCtx, query); err != nil {
				t.Errorf("cleanup metrics: %v", err)
			}
		}
		var unlocked bool
		if err := conn.QueryRow(cleanupCtx, `SELECT pg_advisory_unlock(736800368)`).Scan(&unlocked); err != nil {
			t.Errorf("unlock metrics cleanup: %v", err)
		} else if !unlocked {
			t.Error("metrics cleanup lock was not held on its connection")
		}
	})
	if _, err := conn.Exec(ctx, `DELETE FROM metric_samples`); err != nil {
		t.Fatalf("cleanup samples: %v", err)
	}
	if _, err := conn.Exec(ctx, `DELETE FROM metric_rollups`); err != nil {
		t.Fatalf("cleanup rollups: %v", err)
	}
}
