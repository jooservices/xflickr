package process_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/metrics/adapters/process"
	"github.com/jooservices/xflickr/internal/platform/metrics/domain"
)

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

type mapFS struct {
	files map[string]string
	dirs  map[string][]string
}

func (m mapFS) ReadFile(name string) ([]byte, error) {
	content, ok := m.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return []byte(content), nil
}

func (m mapFS) ReadDir(name string) ([]os.DirEntry, error) {
	names, ok := m.dirs[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	entries := make([]os.DirEntry, 0, len(names))
	for _, n := range names {
		entries = append(entries, fakeDirEntry(n))
	}
	return entries, nil
}

type fakeDirEntry string

func (e fakeDirEntry) Name() string               { return string(e) }
func (e fakeDirEntry) IsDir() bool                { return false }
func (e fakeDirEntry) Type() fs.FileMode          { return 0 }
func (e fakeDirEntry) Info() (fs.FileInfo, error) { return nil, nil }

func TestCollectorReadsFixturesWithoutRealProc(t *testing.T) {
	root := t.TempDir()
	proc := filepath.Join(root, "self")
	if err := os.MkdirAll(filepath.Join(proc, "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	stat := "1 (xflickr) R 0 0 0 0 -1 0 0 0 0 0 50 25 0 0 20 0 1 0 1 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0"
	if err := os.WriteFile(filepath.Join(proc, "stat"), []byte(stat), 0o644); err != nil {
		t.Fatal(err)
	}
	// rss pages = 2 → 2 * pagesize
	if err := os.WriteFile(filepath.Join(proc, "statm"), []byte("10 2 0 0 0 0 0"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0", "1", "2"} {
		if err := os.WriteFile(filepath.Join(proc, "fd", name), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC)
	started := now.Add(-2 * time.Minute)
	counter := &process.ByteCounter{}
	counter.AddIn(11)
	counter.AddOut(22)
	collector, err := process.NewCollector(fixedClock{at: now}, "1.2.3", started, counter)
	if err != nil {
		t.Fatal(err)
	}
	collector.ProcRoot = proc
	collector.FS = nil // use os filesystem against the fixture tree
	samples, err := collector.Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := domain.ValidateSamples(samples); err != nil {
		t.Fatal(err)
	}
	byName := map[string]domain.Sample{}
	for _, sample := range samples {
		byName[sample.Name] = sample
	}
	if byName[domain.MetricProcessOpenFDs].Value != 3 {
		t.Fatalf("fds=%v", byName[domain.MetricProcessOpenFDs].Value)
	}
	if byName[domain.MetricProcessNetworkBytesIn].Value != 11 || byName[domain.MetricProcessNetworkBytesOut].Value != 22 {
		t.Fatalf("network in/out=%v/%v", byName[domain.MetricProcessNetworkBytesIn].Value, byName[domain.MetricProcessNetworkBytesOut].Value)
	}
	if byName[domain.MetricProcessBuildInfo].Labels["version"] != "1.2.3" {
		t.Fatalf("version label=%v", byName[domain.MetricProcessBuildInfo].Labels)
	}
	if byName[domain.MetricProcessUptimeSeconds].Value != 120 {
		t.Fatalf("uptime=%v", byName[domain.MetricProcessUptimeSeconds].Value)
	}
	// Second sample establishes a CPU delta.
	collector.Clock = fixedClock{at: now.Add(time.Second)}
	stat2 := "1 (xflickr) R 0 0 0 0 -1 0 0 0 0 0 150 25 0 0 20 0 1 0 1 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0"
	if err := os.WriteFile(filepath.Join(proc, "stat"), []byte(stat2), 0o644); err != nil {
		t.Fatal(err)
	}
	samples, err = collector.Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range samples {
		if sample.Name == domain.MetricProcessCPUPercent && sample.Value != 100 {
			t.Fatalf("cpu percent=%v want 100", sample.Value)
		}
	}
}
