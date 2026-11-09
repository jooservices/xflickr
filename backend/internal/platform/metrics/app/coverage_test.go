package app_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/metrics/app"
	"github.com/jooservices/xflickr/internal/platform/metrics/domain"
)

func TestNewRunnerAndRetentionValidation(t *testing.T) {
	if _, err := app.NewRunner(nil, nil, fixedClock{}, "api", "n", "v", nil); err == nil {
		t.Fatal("expected store error")
	}
	if _, err := app.NewRunner(&memoryStore{}, nil, nil, "api", "n", "v", nil); err == nil {
		t.Fatal("expected clock error")
	}
	if _, err := app.NewRunner(&memoryStore{}, nil, fixedClock{}, "", "n", "v", nil); err == nil {
		t.Fatal("expected role error")
	}
	if _, err := app.NewRetentionJob(nil, retentionPolicy{days: 1}, fixedClock{}, nil); err == nil {
		t.Fatal("expected store error")
	}
	if _, err := app.NewRetentionJob(&memoryStore{}, nil, fixedClock{}, nil); err == nil {
		t.Fatal("expected policy error")
	}
	if _, err := app.NewRetentionJob(&memoryStore{}, retentionPolicy{days: 1}, nil, nil); err == nil {
		t.Fatal("expected clock error")
	}
}

func TestSystemClockAndRetentionFunc(t *testing.T) {
	now := app.SystemClock{}.Now()
	if now.IsZero() {
		t.Fatal("system clock returned zero")
	}
	fn := app.RetentionFunc(func(context.Context) (int, error) { return 12, nil })
	days, err := fn.RetentionDays(t.Context())
	if err != nil || days != 12 {
		t.Fatalf("days=%d err=%v", days, err)
	}
}

func TestRunnerRunStopsOnCancel(t *testing.T) {
	store := &memoryStore{}
	runner, err := app.NewRunner(store, nil, fixedClock{at: time.Now().UTC()}, "api", "n1", "v", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	runner.Interval = time.Millisecond
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	runner.Run(ctx)
	runner.Run(nil)
}

func TestRetentionRunStopsOnCancel(t *testing.T) {
	job, err := app.NewRetentionJob(&memoryStore{}, retentionPolicy{days: 30}, fixedClock{at: time.Now().UTC()}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	job.Every = time.Millisecond
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	job.Run(ctx)
	job.Every = 0
	job.Run(nil)
}

func TestRetentionTickClampsInvalidDaysAndNilContext(t *testing.T) {
	if app.DefaultPurgeLimit < 4320 {
		t.Fatalf("default purge limit=%d cannot keep up with the documented six-role baseline", app.DefaultPurgeLimit)
	}
	store := &retentionStore{rolled: 1, purged: 1}
	job, err := app.NewRetentionJob(store, retentionPolicy{days: 999}, fixedClock{at: time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC)}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	job.Limit = 0
	if err := job.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if store.limit != app.DefaultPurgeLimit {
		t.Fatalf("limit=%d", store.limit)
	}
	if err := job.Tick(nil); err == nil {
		t.Fatal("expected nil context error")
	}
	job.Every = 0
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	job.Run(ctx)
	failing := retentionPolicyFunc(func(context.Context) (int, error) { return 0, errBoom })
	job2, err := app.NewRetentionJob(store, failing, fixedClock{at: time.Now().UTC()}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := job2.Tick(t.Context()); err == nil {
		t.Fatal("expected retention policy error")
	}
}

type retentionPolicyFunc func(context.Context) (int, error)

func (f retentionPolicyFunc) RetentionDays(ctx context.Context) (int, error) { return f(ctx) }

var errBoom = errString("boom")

type errString string

func (e errString) Error() string { return string(e) }

func TestChunkSamplesEmptyAndDefaultSize(t *testing.T) {
	if got := app.ChunkSamples([]int{}, 0); got != nil {
		t.Fatalf("empty=%v", got)
	}
	items := []int{1, 2, 3}
	chunks := app.ChunkSamples(items, 0)
	if len(chunks) != 1 || len(chunks[0]) != 3 {
		t.Fatalf("chunks=%v", chunks)
	}
	if app.MaxWriteBatch != domain.MaxWriteBatch {
		t.Fatal("batch size mismatch")
	}
}
