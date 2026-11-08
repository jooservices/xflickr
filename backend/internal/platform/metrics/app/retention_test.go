package app_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/metrics/app"
	"github.com/jooservices/xflickr/internal/platform/metrics/domain"
)

type retentionStore struct {
	through time.Time
	before  time.Time
	limit   int
	rolled  int64
	purged  int64
}

func (s *retentionStore) WriteBatch(context.Context, []domain.Sample) error { return nil }
func (s *retentionStore) Rollup(_ context.Context, through time.Time) (int64, error) {
	s.through = through
	return s.rolled, nil
}
func (s *retentionStore) Purge(_ context.Context, before time.Time, limit int) (int64, error) {
	s.before = before
	s.limit = limit
	return s.purged, nil
}

type retentionPolicy struct{ days int }

func (p retentionPolicy) RetentionDays(context.Context) (int, error) { return p.days, nil }

func TestRetentionJobRollsUpThenPurges(t *testing.T) {
	now := time.Date(2026, 11, 9, 6, 30, 0, 0, time.UTC)
	store := &retentionStore{rolled: 2, purged: 5}
	job, err := app.NewRetentionJob(store, retentionPolicy{days: 30}, fixedClock{at: now}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	job.Limit = 100
	if err := job.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !store.through.Equal(time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC)) {
		t.Fatalf("through=%v", store.through)
	}
	wantBefore := now.AddDate(0, 0, -30)
	if !store.before.Equal(wantBefore) || store.limit != 100 {
		t.Fatalf("before=%v limit=%d", store.before, store.limit)
	}
}
