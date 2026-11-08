package app_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/metrics/app"
	"github.com/jooservices/xflickr/internal/platform/metrics/domain"
)

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

type memoryStore struct {
	mu      sync.Mutex
	samples []domain.Sample
	err     error
}

func (s *memoryStore) WriteBatch(_ context.Context, samples []domain.Sample) error {
	if s.err != nil {
		return s.err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples = append(s.samples, samples...)
	return nil
}

func (s *memoryStore) Rollup(context.Context, time.Time) (int64, error) { return 0, nil }
func (s *memoryStore) Purge(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}

type stubCollector struct {
	name    string
	samples []domain.Sample
	err     error
	panic   bool
	calls   int
}

func (c *stubCollector) Name() string { return c.name }
func (c *stubCollector) Collect(context.Context) ([]domain.Sample, error) {
	c.calls++
	if c.panic {
		panic("boom")
	}
	if c.err != nil {
		return nil, c.err
	}
	return c.samples, nil
}

func TestRunnerIsolatesFailingCollector(t *testing.T) {
	now := time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC)
	ok := &stubCollector{
		name: "ok",
		samples: []domain.Sample{{
			Name:  domain.MetricProcessGoroutines,
			Value: 3,
		}},
	}
	failing := &stubCollector{name: "failing", err: errors.New("disk gone")}
	panicking := &stubCollector{name: "panicking", panic: true}
	store := &memoryStore{}
	runner, err := app.NewRunner(store, []app.ScheduledCollector{
		{Collector: ok, Interval: time.Minute},
		{Collector: failing, Interval: time.Minute},
		{Collector: panicking, Interval: time.Minute},
	}, fixedClock{at: now}, "api", "node-a", "v1", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Tick(t.Context()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if ok.calls != 1 || failing.calls != 1 || panicking.calls != 1 {
		t.Fatalf("calls ok=%d failing=%d panicking=%d", ok.calls, failing.calls, panicking.calls)
	}
	var sawOK, sawErrors bool
	var errorCount float64
	for _, sample := range store.samples {
		switch sample.Name {
		case domain.MetricProcessGoroutines:
			sawOK = true
			if sample.Labels["role"] != "api" || sample.Labels["node"] != "node-a" {
				t.Fatalf("labels=%v", sample.Labels)
			}
		case domain.MetricCollectorErrors:
			sawErrors = true
			errorCount = sample.Value
		}
	}
	if !sawOK || !sawErrors {
		t.Fatalf("samples=%v", store.samples)
	}
	if errorCount != 2 {
		t.Fatalf("collector_errors=%v want 2", errorCount)
	}
}

func TestChunkSamplesCapsAt500(t *testing.T) {
	items := make([]int, 1200)
	chunks := app.ChunkSamples(items, app.MaxWriteBatch)
	if len(chunks) != 3 {
		t.Fatalf("chunks=%d want 3", len(chunks))
	}
	if len(chunks[0]) != 500 || len(chunks[1]) != 500 || len(chunks[2]) != 200 {
		t.Fatalf("chunk sizes=%d,%d,%d", len(chunks[0]), len(chunks[1]), len(chunks[2]))
	}
}
