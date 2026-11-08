// Package process samples CPU, memory, runtime and network metrics for one role.
//
// Network bytes use an in-process ByteCounter updated by HTTP middleware and
// RoundTripper wrappers. /proc/self/net/dev is namespace-wide in containers and
// is not process-attributable, so it is intentionally unused.
package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	rtmetrics "runtime/metrics"
	"strconv"
	"strings"
	"time"

	"github.com/jooservices/xflickr/internal/platform/metrics/domain"
)

// FS reads process status files. Tests supply fixtures; production uses /proc.
type FS interface {
	ReadFile(name string) ([]byte, error)
	ReadDir(name string) ([]os.DirEntry, error)
}

type osFS struct{}

func (osFS) ReadFile(name string) ([]byte, error) {
	// Proc paths are built from a fixed ProcRoot (/proc/self), never user input.
	data, err := os.ReadFile(name) //nolint:gosec // G304: fixed /proc/self paths only
	if err != nil {
		return nil, fmt.Errorf("metrics: read %s: %w", name, err)
	}
	return data, nil
}

func (osFS) ReadDir(name string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(name)
	if err != nil {
		return nil, fmt.Errorf("metrics: readdir %s: %w", name, err)
	}
	return entries, nil
}

// Collector samples process resources once per Collect call.
type Collector struct {
	FS         FS
	Clock      domain.Clock
	Network    *ByteCounter
	Version    string
	StartedAt  time.Time
	ClockTicks float64
	ProcRoot   string

	prevCPU CPUSample
	prevAt  time.Time
}

var _ domain.Collector = (*Collector)(nil)

// NewCollector builds a process collector. clock and startedAt are required.
func NewCollector(clock domain.Clock, version string, startedAt time.Time, network *ByteCounter) (*Collector, error) {
	if clock == nil {
		return nil, errors.New("metrics: process clock is required")
	}
	if startedAt.IsZero() {
		return nil, errors.New("metrics: process start time is required")
	}
	return &Collector{
		FS:         osFS{},
		Clock:      clock,
		Network:    network,
		Version:    version,
		StartedAt:  startedAt,
		ClockTicks: 100,
		ProcRoot:   "/proc/self",
	}, nil
}

// Name identifies the collector in the runner.
func (c *Collector) Name() string { return "process" }

// Collect reads process and runtime samples without calling Flickr.
func (c *Collector) Collect(ctx context.Context) ([]domain.Sample, error) {
	if ctx == nil {
		return nil, errors.New("metrics: process collect context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := c.Clock.Now()
	fs := c.FS
	if fs == nil {
		fs = osFS{}
	}
	root := c.ProcRoot
	if root == "" {
		root = "/proc/self"
	}
	statText, err := fs.ReadFile(filepath.Join(root, "stat"))
	if err != nil {
		return nil, fmt.Errorf("metrics: read stat: %w", err)
	}
	cpu, err := ParseStatCPU(string(statText))
	if err != nil {
		return nil, err
	}
	cpuPercent := 0.0
	if !c.prevAt.IsZero() {
		cpuPercent = CPUPercent(c.prevCPU, cpu, now.Sub(c.prevAt).Seconds(), c.ClockTicks)
	}
	c.prevCPU = cpu
	c.prevAt = now

	rss, err := readRSS(fs, root)
	if err != nil {
		return nil, err
	}
	heap, gcPause, gcCount := readRuntimeMetrics()
	fds, err := countFDs(fs, root)
	if err != nil {
		return nil, err
	}
	var netIn, netOut float64
	if c.Network != nil {
		in, out := c.Network.Snapshot()
		netIn, netOut = float64(in), float64(out)
	}
	uptime := now.Sub(c.StartedAt).Seconds()
	if uptime < 0 {
		uptime = 0
	}
	version := c.Version
	if version == "" {
		version = "unknown"
	}
	return []domain.Sample{
		{Name: domain.MetricProcessCPUPercent, At: now, Value: cpuPercent},
		{Name: domain.MetricProcessRSSBytes, At: now, Value: float64(rss)},
		{Name: domain.MetricProcessHeapBytes, At: now, Value: float64(heap)},
		{Name: domain.MetricProcessGCPauseNS, At: now, Value: float64(gcPause)},
		{Name: domain.MetricProcessGCCount, At: now, Value: float64(gcCount)},
		{Name: domain.MetricProcessGoroutines, At: now, Value: float64(runtime.NumGoroutine())},
		{Name: domain.MetricProcessOpenFDs, At: now, Value: float64(fds)},
		{Name: domain.MetricProcessUptimeSeconds, At: now, Value: uptime},
		{Name: domain.MetricProcessBuildInfo, Labels: map[string]string{"version": version}, At: now, Value: 1},
		{Name: domain.MetricProcessNetworkBytesIn, At: now, Value: netIn},
		{Name: domain.MetricProcessNetworkBytesOut, At: now, Value: netOut},
	}, nil
}

func readRSS(fs FS, root string) (uint64, error) {
	statm, err := fs.ReadFile(filepath.Join(root, "statm"))
	if err != nil {
		return 0, fmt.Errorf("metrics: read statm: %w", err)
	}
	fields := strings.Fields(string(statm))
	if len(fields) < 2 {
		return 0, fmt.Errorf("metrics: invalid statm")
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("metrics: parse rss pages: %w", err)
	}
	pageSize := os.Getpagesize()
	if pageSize <= 0 {
		return 0, fmt.Errorf("metrics: invalid page size %d", pageSize)
	}
	return pages * uint64(pageSize), nil //nolint:gosec // G115: pageSize validated positive
}

func countFDs(fs FS, root string) (int, error) {
	entries, err := fs.ReadDir(filepath.Join(root, "fd"))
	if err != nil {
		return 0, fmt.Errorf("metrics: read fd: %w", err)
	}
	return len(entries), nil
}

func readRuntimeMetrics() (heap, gcPauseNS, gcCount uint64) {
	samples := []rtmetrics.Sample{
		{Name: "/memory/classes/heap/objects:bytes"},
		{Name: "/gc/cycles/total:gc-cycles"},
	}
	rtmetrics.Read(samples)
	for _, sample := range samples {
		switch sample.Name {
		case "/memory/classes/heap/objects:bytes":
			if sample.Value.Kind() == rtmetrics.KindUint64 {
				heap = sample.Value.Uint64()
			}
		case "/gc/cycles/total:gc-cycles":
			if sample.Value.Kind() == rtmetrics.KindUint64 {
				gcCount = sample.Value.Uint64()
			}
		}
	}
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	gcPauseNS = stats.PauseTotalNs
	if heap == 0 {
		heap = stats.HeapAlloc
	}
	if gcCount == 0 {
		gcCount = uint64(stats.NumGC)
	}
	return heap, gcPauseNS, gcCount
}
