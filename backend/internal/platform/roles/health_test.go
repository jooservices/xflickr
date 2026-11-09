package roles

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/ops"
	opspostgres "github.com/jooservices/xflickr/internal/platform/ops/adapters/postgres"
)

type healthStore struct{}

func (healthStore) Snapshot(context.Context) (ops.Snapshot, error) { return ops.Snapshot{}, nil }

type lockedBuffer struct {
	bytes.Buffer
	ch chan struct{}
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	n, err := b.Buffer.Write(p)
	select {
	case b.ch <- struct{}{}:
	default:
	}
	return n, err
}

func TestNewHealthSinkNeedsCredentials(t *testing.T) {
	if newHealthSink(config.Config{}) != nil {
		t.Fatal("sink without credentials")
	}
	if newHealthSink(config.Config{Obs: config.ObsConfig{URL: "http://oo"}}) != nil {
		t.Fatal("username required")
	}
	cfg := config.Config{Obs: config.ObsConfig{URL: "http://openobserve:5080", Username: "u", Password: "p", Org: "default", Stream: "xflickr"}}
	if newHealthSink(cfg) == nil {
		t.Fatal("expected sink")
	}
}

func TestStartCrawlHealthLogsAtStartAndStops(t *testing.T) {
	buffer := &lockedBuffer{ch: make(chan struct{}, 1)}
	stop := startCrawlHealth(t.Context(), healthStore{}, config.Config{}, slog.New(slog.NewTextHandler(buffer, nil)), time.Minute)
	select {
	case <-buffer.ch:
	case <-time.After(5 * time.Second):
		t.Fatal("no health line at start")
	}
	stop()
	stop()
	if !strings.Contains(buffer.String(), "crawl health") {
		t.Fatalf("logs=%s", buffer.String())
	}
}

func TestStartCrawlHealthDefaultInterval(t *testing.T) {
	buffer := &lockedBuffer{ch: make(chan struct{}, 1)}
	stop := startCrawlHealth(t.Context(), healthStore{}, config.Config{}, slog.New(slog.NewTextHandler(buffer, nil)), 0)
	select {
	case <-buffer.ch:
	case <-time.After(5 * time.Second):
		t.Fatal("no health line")
	}
	stop()
}

func TestCrawlHealthDepsUsesDefaultsWithoutPool(t *testing.T) {
	buffer := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buffer, nil))
	store, interval := crawlHealthDeps(t.Context(), nil, logger)
	if interval != opspostgres.DefaultHealthSettings().Interval {
		t.Fatalf("interval=%s", interval)
	}
	exclusive, ok := store.(opspostgres.ExclusiveStore)
	if !ok {
		t.Fatalf("store=%T", store)
	}
	if _, err := exclusive.Snapshot(t.Context()); err == nil {
		t.Fatal("exclusive store without pool must fail snapshot")
	}
	if !strings.Contains(buffer.String(), "load settings failed") {
		t.Fatalf("logs=%s", buffer.String())
	}
	if _, err := (healthLockPool{}).Acquire(t.Context()); err == nil {
		t.Fatal("expected nil pool acquire error")
	}
	store, _ = crawlHealthDeps(t.Context(), nil, nil)
	if _, ok := store.(opspostgres.ExclusiveStore); !ok {
		t.Fatalf("store=%T", store)
	}
}

func TestCrawlHealthDepsUsesLoadedSettings(t *testing.T) {
	original := loadHealthSettings
	t.Cleanup(func() { loadHealthSettings = original })
	loadHealthSettings = func(context.Context, opspostgres.Querier) (opspostgres.HealthSettings, error) {
		return opspostgres.HealthSettings{Interval: 30 * time.Second, StatementTimeout: 3 * time.Second}, nil
	}
	store, interval := crawlHealthDeps(t.Context(), nil, nil)
	if interval != 30*time.Second {
		t.Fatalf("interval=%s", interval)
	}
	exclusive := store.(opspostgres.ExclusiveStore)
	inner := exclusive.Inner.(opspostgres.Store)
	if inner.StatementTimeout != 3*time.Second {
		t.Fatalf("timeout=%s", inner.StatementTimeout)
	}
}

func TestStartCrawlHealthMonitorWithoutPool(t *testing.T) {
	buffer := &lockedBuffer{ch: make(chan struct{}, 4)}
	logger := slog.New(slog.NewTextHandler(buffer, &slog.HandlerOptions{Level: slog.LevelDebug}))
	stop := startCrawlHealthMonitor(t.Context(), nil, config.Config{}, logger)
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-buffer.ch:
			if strings.Contains(buffer.String(), "report failed") || strings.Contains(buffer.String(), "load settings failed") {
				stop()
				return
			}
		case <-deadline:
			stop()
			t.Fatalf("logs=%s", buffer.String())
		}
	}
}
