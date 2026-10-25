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
	cfg := config.Config{Obs: config.ObsConfig{URL: "http://openobserve:5080", Username: "u", Password: "p", Org: "default", Stream: "xflickr"}}
	if newHealthSink(cfg) == nil {
		t.Fatal("expected sink")
	}
}

func TestStartCrawlHealthLogsAtStartAndStops(t *testing.T) {
	buffer := &lockedBuffer{ch: make(chan struct{}, 1)}
	stop := startCrawlHealth(t.Context(), healthStore{}, config.Config{}, slog.New(slog.NewTextHandler(buffer, nil)))
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
