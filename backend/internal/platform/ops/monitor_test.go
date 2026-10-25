package ops

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type recordSink struct {
	events [][]Event
	err    error
}

func (r *recordSink) Send(_ context.Context, events []Event) error {
	r.events = append(r.events, events)
	return r.err
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func newMonitor(snapshot Snapshot, sink Sink) (*Monitor, *bytes.Buffer, *clock) {
	buffer := &bytes.Buffer{}
	c := &clock{now: base}
	return &Monitor{
		Service: Service{Store: fakeStore{snapshot: snapshot}, Now: c.Now},
		Sink:    sink,
		Logger:  slog.New(slog.NewTextHandler(buffer, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Node:    "node-1",
	}, buffer, c
}

func TestTickLogsHealthLine(t *testing.T) {
	sink := &recordSink{}
	monitor, logs, _ := newMonitor(Snapshot{
		Counts:                       Counts{People: 7, Photos: 9},
		WorkItems:                    map[string]int64{"pending": 1, "ready": 2},
		RiverJobs:                    map[string]int64{"available": 4},
		PhotoSizesJobs:               LaneJobs{Pending: 5, Ready: 2, Running: 1},
		Keys:                         []KeyStatus{{KeyID: 3, Label: "lbl-secret", Status: "valid", CallsLastHour: 10, Limit: 100}},
		Tokens:                       map[string]int64{"ok": 1},
		CredentialDecryptionFailures: 2,
	}, sink)
	if err := monitor.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	line := logs.String()
	for _, want := range []string{"crawl health", "people=7", "photos=9", "photo_sizes_pending=5", "photo_sizes_ready=2", "photo_sizes_running=1", "work_items.ready=2", "river_jobs.available=4", "keys.key_3.remaining=90", "token_status=ok", "ready_jobs=2", "calls_last_10min=0", "credential_decryption_failures=2"} {
		if !strings.Contains(line, want) {
			t.Fatalf("log line misses %q: %s", want, line)
		}
	}
	if strings.Contains(line, "lbl-secret") {
		t.Fatalf("log line must not carry key labels: %s", line)
	}
	if len(sink.events) != 1 || len(sink.events[0]) != 2 {
		t.Fatalf("events=%v", sink.events)
	}
	health := sink.events[0][0]
	if health["event"] != HealthEvent || health["work_items_ready"] != int64(2) || health["river_jobs_available"] != int64(4) || health["photo_sizes_pending"] != int64(5) || health["photo_sizes_ready"] != int64(2) || health["photo_sizes_running"] != int64(1) || health["failed_total"] != int64(0) || health["credential_decryption_failures"] != int64(2) || health["node"] != "node-1" {
		t.Fatalf("health event=%v", health)
	}
	if key := sink.events[0][1]; key["event"] != HealthKeyEvent || key["remaining"] != int64(90) || key["key_id"] != int64(3) {
		t.Fatalf("key event=%v", key)
	}
}

func TestIdleWarningAfterFiveMinutes(t *testing.T) {
	monitor, logs, c := newMonitor(Snapshot{
		Pending: Pending{Total: 3, WithoutKey: 3, Oldest: base.Add(-time.Hour)},
	}, nil)
	for _, offset := range []time.Duration{0, 4 * time.Minute} {
		c.now = base.Add(offset)
		if err := monitor.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("warned too early: %s", logs.String())
	}
	c.now = base.Add(5 * time.Minute)
	if err := monitor.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if out := logs.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "reason=no_key") || !strings.Contains(out, "pending_items=3") {
		t.Fatalf("missing idle warning: %s", out)
	}
}

func TestIdleWarningResetsWhenWorkIsReady(t *testing.T) {
	monitor, logs, c := newMonitor(Snapshot{Pending: Pending{Total: 1, WithoutKey: 1}}, nil)
	if err := monitor.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	monitor.Service.Store = fakeStore{snapshot: Snapshot{Pending: Pending{Total: 1}, WorkItems: map[string]int64{"ready": 1}}}
	c.now = base.Add(time.Minute)
	if err := monitor.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !monitor.idleSince.IsZero() {
		t.Fatal("idle timer was not reset")
	}
	monitor.IdleAfter = time.Minute
	monitor.Service.Store = fakeStore{snapshot: Snapshot{Pending: Pending{Total: 1, WithoutKey: 1}}}
	c.now = base.Add(2 * time.Minute)
	_ = monitor.Tick(t.Context())
	if strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("idle timer should restart: %s", logs.String())
	}
	c.now = base.Add(3 * time.Minute)
	_ = monitor.Tick(t.Context())
	if !strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("custom IdleAfter ignored: %s", logs.String())
	}
}

func TestTickSinkFailureIsLoggedOnly(t *testing.T) {
	monitor, logs, _ := newMonitor(Snapshot{}, &recordSink{err: errors.New("down")})
	if err := monitor.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "ship to observability failed") {
		t.Fatalf("logs=%s", logs.String())
	}
}

func TestTickErrors(t *testing.T) {
	monitor, _, _ := newMonitor(Snapshot{}, nil)
	if err := monitor.Tick(nil); err == nil { //nolint:staticcheck // nil context is the case under test
		t.Fatal("expected error for nil context")
	}
	monitor.Service.Store = fakeStore{err: errors.New("db down")}
	if err := monitor.Tick(t.Context()); err == nil {
		t.Fatal("expected store error")
	}
}

func TestRunTicksUntilCancelled(t *testing.T) {
	monitor, logs, _ := newMonitor(Snapshot{}, nil)
	monitor.Interval = 5 * time.Millisecond
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Millisecond)
	defer cancel()
	monitor.Run(ctx)
	if strings.Count(logs.String(), "crawl health") < 2 {
		t.Fatalf("expected repeated ticks: %s", logs.String())
	}
}

func TestRunLogsFailures(t *testing.T) {
	monitor, logs, _ := newMonitor(Snapshot{}, nil)
	monitor.Service.Store = fakeStore{err: errors.New("db down")}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	monitor.Run(ctx)
	if !strings.Contains(logs.String(), "report failed") {
		t.Fatalf("logs=%s", logs.String())
	}
}

func TestMonitorDefaultLogger(t *testing.T) {
	if (&Monitor{}).logger() == nil {
		t.Fatal("nil logger")
	}
}
