package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jooservices/xflickr/internal/platform/metrics/adapters/postgres"
	"github.com/jooservices/xflickr/internal/platform/metrics/domain"
)

type fakeStorePool struct {
	execCalls int
	execArgs  [][]any
	execErr   error
	rowValue  int64
	rowErr    error
}

func (p *fakeStorePool) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	p.execCalls++
	p.execArgs = append(p.execArgs, args)
	if p.execErr != nil {
		return pgconn.CommandTag{}, p.execErr
	}
	return pgconn.NewCommandTag("DELETE 3"), nil
}

func (p *fakeStorePool) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakeStoreRow{value: p.rowValue, err: p.rowErr}
}

type fakeStoreRow struct {
	value int64
	err   error
}

func (r fakeStoreRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*int64) = r.value
	return nil
}

func TestWriteBatchChunksAndRejectsUnknownMetric(t *testing.T) {
	pool := &fakeStorePool{}
	store := postgres.Store{Pool: pool}
	samples := make([]domain.Sample, 0, 501)
	at := time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC)
	for i := 0; i < 501; i++ {
		samples = append(samples, domain.Sample{
			Name:   domain.MetricCollectorErrors,
			Labels: map[string]string{"role": "api", "i": string(rune('a' + (i % 26)))},
			At:     at,
			Value:  float64(i),
		})
	}
	// Make labels unique with numeric suffix in Value-only uniqueness via labels hash of i as string through Value... use strconv via labels
	samples = samples[:0]
	for i := 0; i < 501; i++ {
		samples = append(samples, domain.Sample{
			Name:   domain.MetricCollectorErrors,
			Labels: map[string]string{"role": "api", "n": itoa(i)},
			At:     at,
			Value:  1,
		})
	}
	if err := store.WriteBatch(t.Context(), samples); err != nil {
		t.Fatal(err)
	}
	if pool.execCalls != 2 {
		t.Fatalf("execCalls=%d want 2", pool.execCalls)
	}
	if err := store.WriteBatch(t.Context(), []domain.Sample{{Name: "nope", Value: 1}}); err == nil {
		t.Fatal("expected unknown metric error")
	}
	if err := (postgres.Store{}).WriteBatch(t.Context(), nil); err == nil {
		t.Fatal("expected nil pool error")
	}
}

func TestRollupAndPurgeValidation(t *testing.T) {
	pool := &fakeStorePool{rowValue: 4}
	store := postgres.Store{Pool: pool}
	through := time.Date(2026, 11, 9, 6, 30, 0, 0, time.UTC)
	n, err := store.Rollup(t.Context(), through)
	if err != nil || n != 4 {
		t.Fatalf("Rollup=%d %v", n, err)
	}
	pool.execErr = nil
	purged, err := store.Purge(t.Context(), through, 10)
	if err != nil || purged != 3 {
		t.Fatalf("Purge=%d %v", purged, err)
	}
	if _, err := store.Purge(t.Context(), through, 0); err == nil {
		t.Fatal("expected limit error")
	}
	if _, err := (postgres.Store{}).Rollup(t.Context(), through); err == nil {
		t.Fatal("expected nil pool error")
	}
	pool.rowErr = errors.New("db down")
	if _, err := store.Rollup(t.Context(), through); err == nil {
		t.Fatal("expected rollup error")
	}
	pool.rowErr = nil
	pool.execErr = errors.New("delete failed")
	if _, err := store.Purge(t.Context(), through, 1); err == nil {
		t.Fatal("expected purge error")
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
