package process_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/metrics/adapters/process"
)

func TestProcessConstructorAndName(t *testing.T) {
	if _, err := process.NewCollector(nil, "v", time.Now(), nil); err == nil {
		t.Fatal("expected clock error")
	}
	if _, err := process.NewCollector(fixedClock{at: time.Now()}, "v", time.Time{}, nil); err == nil {
		t.Fatal("expected start time error")
	}
	collector, err := process.NewCollector(fixedClock{at: time.Now().UTC()}, "", time.Now().UTC(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if collector.Name() != "process" {
		t.Fatalf("name=%q", collector.Name())
	}
	if _, err := collector.Collect(nil); err == nil {
		t.Fatal("expected nil context")
	}
}

func TestMiddlewareNilCounterAndZeroContentLength(t *testing.T) {
	handler := process.Middleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	counter := &process.ByteCounter{}
	handler = process.Middleware(counter)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(nil)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestCPUPercentEdgeCases(t *testing.T) {
	if process.CPUPercent(process.CPUSample{Jiffies: 10}, process.CPUSample{Jiffies: 5}, 1, 100) != 0 {
		t.Fatal("expected zero for decreasing jiffies")
	}
	if process.CPUPercent(process.CPUSample{Jiffies: 1}, process.CPUSample{Jiffies: 2}, 0, 100) != 0 {
		t.Fatal("expected zero for zero elapsed")
	}
}

func TestCollectorUsesInjectedFS(t *testing.T) {
	now := time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC)
	collector, err := process.NewCollector(fixedClock{at: now}, "v", now.Add(-time.Second), nil)
	if err != nil {
		t.Fatal(err)
	}
	root := "/proc/self"
	collector.ProcRoot = root
	collector.FS = mapFS{
		files: map[string]string{
			root + "/stat":  "1 (xflickr) R 0 0 0 0 -1 0 0 0 0 0 10 5 0 0 20 0 1 0 1 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0",
			root + "/statm": "10 3 0 0 0 0 0",
		},
		dirs: map[string][]string{root + "/fd": {"0", "1"}},
	}
	samples, err := collector.Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) < 10 {
		t.Fatalf("samples=%d", len(samples))
	}
	collector.FS = mapFS{files: map[string]string{}, dirs: map[string][]string{}}
	if _, err := collector.Collect(t.Context()); err == nil {
		t.Fatal("expected missing stat error")
	}
	collector.FS = mapFS{
		files: map[string]string{root + "/stat": "1 (xflickr) R 0 0 0 0 -1 0 0 0 0 0 10 5 0 0 20 0 1 0 1 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0"},
		dirs:  map[string][]string{},
	}
	if _, err := collector.Collect(t.Context()); err == nil {
		t.Fatal("expected missing statm error")
	}
	collector.FS = mapFS{
		files: map[string]string{
			root + "/stat":  "1 (xflickr) R 0 0 0 0 -1 0 0 0 0 0 10 5 0 0 20 0 1 0 1 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0",
			root + "/statm": "bad",
		},
		dirs: map[string][]string{root + "/fd": {}},
	}
	if _, err := collector.Collect(t.Context()); err == nil {
		t.Fatal("expected invalid statm error")
	}
	collector.FS = mapFS{
		files: map[string]string{
			root + "/stat":  "1 (xflickr) R 0 0 0 0 -1 0 0 0 0 0 10 5 0 0 20 0 1 0 1 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0",
			root + "/statm": "10 3 0 0 0 0 0",
		},
		dirs: map[string][]string{},
	}
	if _, err := collector.Collect(t.Context()); err == nil {
		t.Fatal("expected missing fd error")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	collector.FS = mapFS{
		files: map[string]string{
			root + "/stat":  "1 (xflickr) R 0 0 0 0 -1 0 0 0 0 0 10 5 0 0 20 0 1 0 1 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0",
			root + "/statm": "10 3 0 0 0 0 0",
		},
		dirs: map[string][]string{root + "/fd": {"0"}},
	}
	if _, err := collector.Collect(ctx); err == nil {
		t.Fatal("expected canceled context")
	}
}
