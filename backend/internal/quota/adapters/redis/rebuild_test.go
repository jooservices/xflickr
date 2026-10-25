package redis

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/quota/app"
	"github.com/jooservices/xflickr/internal/quota/domain"
)

type memStore struct {
	usage app.Usage
	has   bool
}

func (m memStore) Save(context.Context, app.Usage) error { return nil }
func (m memStore) Latest(context.Context, int64, time.Time) (app.Usage, bool, error) {
	return m.usage, m.has, nil
}
func (m memStore) KeyIDs(context.Context) ([]int64, error) { return []int64{1}, nil }

type memCalls struct{ usage app.CallUsage }

func (m memCalls) Usage(context.Context, int64, time.Time) (app.CallUsage, error) {
	return m.usage, nil
}

func rebuilderFor(g Governor, store app.UsageStore, calls app.CallLog) *app.Rebuilder {
	return &app.Rebuilder{Store: store, Calls: calls, Counters: Counters{Client: g.Client, Prefix: g.Prefix}}
}

func TestAcquireFailsClosedWithoutRebuilderMarkerThenRebuilds(t *testing.T) {
	budget := generousBudget()
	budget.HourlyLimit = 10
	budget.Reserve = 0
	g, server, _ := setupGovernor(t, budget)
	g.Rebuilder = rebuilderFor(g, memStore{has: true, usage: app.Usage{WindowStart: time.Now().Add(-5 * time.Minute), Used: 9, ByClass: map[string]int64{"interactive": 9}}}, memCalls{})

	// Redis keys are missing: Acquire rebuilds first, then enforces the stored usage.
	permit, _, err := acquire(t, g, domain.Interactive)
	if err != nil || permit == nil {
		t.Fatalf("first acquire after rebuild = %v %v", permit, err)
	}
	if permit, wait, err := acquire(t, g, domain.Interactive); err != nil || permit != nil || wait <= 0 {
		t.Fatalf("overspend after rebuild: %v %s %v", permit, wait, err)
	}
	used, _ := server.Get(testPrefix + "quota:key:1:hourly")
	if used != "10" {
		t.Fatalf("hourly = %s", used)
	}

	// Losing the keys again denies nothing extra: the rebuild runs once more.
	server.FlushAll()
	if permit, _, err := acquire(t, g, domain.Interactive); err != nil || permit == nil {
		t.Fatalf("acquire after second loss = %v %v", permit, err)
	}
}

func TestAcquireWaitsWhenMarkerMissingAndRebuildFails(t *testing.T) {
	g, _, _ := setupGovernor(t, generousBudget())
	g.Rebuilder = failingRebuilder{}
	if permit, _, err := acquire(t, g, domain.Interactive); !errors.Is(err, domain.ErrGovernorOffline) || permit != nil {
		t.Fatalf("acquire = %v %v", permit, err)
	}
}

type failingRebuilder struct{}

func (failingRebuilder) Rebuild(context.Context, int64) error { return errors.New("down") }

type noopRebuilder struct{}

func (noopRebuilder) Rebuild(context.Context, int64) error { return nil }

func TestAcquireDeniesWhenRebuildLeavesMarkerMissing(t *testing.T) {
	g, _, _ := setupGovernor(t, generousBudget())
	g.Rebuilder = noopRebuilder{}
	permit, wait, err := acquire(t, g, domain.Interactive)
	if err != nil || permit != nil || wait <= 0 {
		t.Fatalf("acquire = %v %s %v", permit, wait, err)
	}
}

func TestRebuildFromCallLogOnly(t *testing.T) {
	budget := generousBudget()
	budget.HourlyLimit = 5
	budget.Reserve = 0
	g, _, _ := setupGovernor(t, budget)
	g.Rebuilder = rebuilderFor(g, memStore{}, memCalls{usage: app.CallUsage{First: time.Now().Add(-time.Minute), ByClass: map[string]int64{"person": 5}}})
	if permit, wait, err := acquire(t, g, domain.Interactive); err != nil || permit != nil || wait <= 0 {
		t.Fatalf("call log not counted: %v %s %v", permit, wait, err)
	}
}

func TestSnapshotAndRestore(t *testing.T) {
	g, server, client := setupGovernor(t, generousBudget())
	c := Counters{Client: client, Prefix: testPrefix}
	if _, ok, err := c.Snapshot(context.Background(), 1); err != nil || ok {
		t.Fatalf("empty snapshot = %v %v", ok, err)
	}
	if permit, _, err := acquire(t, g, domain.Person); err != nil || permit == nil {
		t.Fatal("acquire failed")
	}
	if permit, _, err := acquire(t, g, domain.Photo); err != nil || permit == nil {
		t.Fatal("acquire failed")
	}
	usage, ok, err := c.Snapshot(context.Background(), 1)
	if err != nil || !ok || usage.Used != 2 || usage.ByClass["person"] != 1 || usage.ByClass["photo"] != 1 || usage.WindowStart.IsZero() {
		t.Fatalf("snapshot = %+v %v %v", usage, ok, err)
	}

	server.FlushAll()
	wrote, err := c.Restore(context.Background(), usage)
	if err != nil || !wrote {
		t.Fatalf("restore = %v %v", wrote, err)
	}
	again, ok, _ := c.Snapshot(context.Background(), 1)
	if !ok || again.Used != 2 || again.ByClass["person"] != 1 {
		t.Fatalf("restored = %+v", again)
	}
	if wrote, err := c.Restore(context.Background(), app.Usage{KeyID: 1}); err != nil || wrote {
		t.Fatalf("restore over ready counters = %v %v", wrote, err)
	}
}

func TestRestoreIgnoresExpiredWindowAndRejectsBadInput(t *testing.T) {
	_, server, client := setupGovernor(t, generousBudget())
	c := Counters{Client: client, Prefix: testPrefix}
	wrote, err := c.Restore(context.Background(), app.Usage{KeyID: 1, WindowStart: time.Now().Add(-2 * time.Hour), Used: 99})
	if err != nil || !wrote {
		t.Fatalf("restore = %v %v", wrote, err)
	}
	if server.Exists(testPrefix + "quota:key:1:hourly") {
		t.Fatal("expired window restored")
	}
	if _, err := (Counters{}).Restore(context.Background(), app.Usage{KeyID: 1}); err == nil {
		t.Fatal("nil client accepted")
	}
	if _, _, err := (Counters{}).Snapshot(context.Background(), 1); err == nil {
		t.Fatal("nil client accepted")
	}
}

func TestSnapshotRejectsCorruptCounts(t *testing.T) {
	_, server, client := setupGovernor(t, generousBudget())
	c := Counters{Client: client, Prefix: testPrefix}
	server.Set(testPrefix+"quota:key:1:window", "1700000000000")
	server.Set(testPrefix+"quota:key:1:hourly", "x")
	if _, _, err := c.Snapshot(context.Background(), 1); err == nil {
		t.Fatal("corrupt hourly accepted")
	}
	server.Set(testPrefix+"quota:key:1:hourly", "3")
	server.HSet(testPrefix+"quota:key:1:byclass", "person", "x")
	if _, _, err := c.Snapshot(context.Background(), 1); err == nil {
		t.Fatal("corrupt class count accepted")
	}
	server.Set(testPrefix+"quota:key:1:window", "bad")
	if _, _, err := c.Snapshot(context.Background(), 1); err == nil {
		t.Fatal("corrupt window accepted")
	}
}
