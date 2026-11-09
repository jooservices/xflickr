package redis

import (
	"context"
	"fmt"
	"testing"
	"time"

	redisv9 "github.com/redis/go-redis/v9"

	"github.com/jooservices/xflickr/internal/quota/app"
	"github.com/jooservices/xflickr/internal/quota/domain"
	flickr "github.com/jooservices/xflickr/pkg/flickr"
)

func TestRollingBoundaryRetainsLateGrants(t *testing.T) {
	b := generousBudget()
	b.HourlyLimit = 10
	b.Reserve = 0
	g, s, _ := setupGovernor(t, b)
	start := time.Unix(1800000000, 0)
	s.SetTime(start)
	if p, _, e := acquire(t, g, domain.Interactive); e != nil || p == nil {
		t.Fatalf("initial: %v", e)
	}
	s.SetTime(start.Add(59 * time.Minute))
	for i := 0; i < 9; i++ {
		if p, _, e := acquire(t, g, domain.Interactive); e != nil || p == nil {
			t.Fatalf("late %d: %v", i, e)
		}
	}
	s.SetTime(start.Add(time.Hour))
	if p, _, e := acquire(t, g, domain.Interactive); e != nil || p == nil {
		t.Fatalf("oldest expiry: %v", e)
	}
	if p, w, e := acquire(t, g, domain.Interactive); e != nil || p != nil || w != 59*time.Minute {
		t.Fatalf("rolling overspend: %v %s %v", p, w, e)
	}
}

// seedGrants models timestamped traffic without spending test time on permits.
func seedGrants(t *testing.T, g Governor, class domain.Class, n int, at time.Time) {
	t.Helper()
	lane := g.keys(1)[1]
	switch class {
	case domain.Person:
		lane = g.keys(1)[2]
	case domain.Photo:
		lane = g.keys(1)[4]
	}
	members := make([]redisv9.Z, n)
	for i := range members {
		members[i] = redisv9.Z{Score: float64(at.UnixMilli()), Member: fmt.Sprintf("seed:%s:%d:%d", class, at.UnixMilli(), i)}
	}
	if err := g.Client.ZAdd(t.Context(), lane, members...).Err(); err != nil {
		t.Fatal(err)
	}
}

func TestRollingDayUtilization(t *testing.T) {
	// A smaller configured allowance exercises 24 complete rolling windows
	// without changing the algorithm or allowing the second gate to mask it.
	b := generousBudget()
	b.HourlyLimit = 100
	b.Reserve = 0
	b.PerSecondRate = 4
	b.Burst = 5
	g, s, _ := setupGovernor(t, b)
	start := time.Unix(1800000000, 0)
	now := start
	var grants []time.Time
	maxUsed := 0
	for now.Before(start.Add(24 * time.Hour)) {
		s.SetTime(now)
		p, wait, err := acquire(t, g, domain.Interactive)
		if err != nil {
			t.Fatal(err)
		}
		if p == nil {
			if wait <= 0 {
				t.Fatal("nonpositive retry")
			}
			now = now.Add(wait)
			continue
		}
		grants = append(grants, now)
		first := 0
		for first < len(grants) && !grants[first].After(now.Add(-time.Hour)) {
			first++
		}
		used := len(grants) - first
		if used > 100 {
			t.Fatalf("rolling overspend at %s: %d", now, used)
		}
		maxUsed = max(maxUsed, used)
	}
	allowance := 24 * int(b.HourlyLimit)
	if float64(len(grants)) < 0.95*float64(allowance) {
		t.Fatalf("utilization %d/%d", len(grants), allowance)
	}
	t.Logf("24h: %d/%d grants (%.2f%%); maximum rolling usage %d/%d", len(grants), allowance, 100*float64(len(grants))/float64(allowance), maxUsed, b.HourlyLimit)
}

func TestRollingPhotoShareRetryUsesOldestCountedGrant(t *testing.T) {
	b := generousBudget()
	b.HourlyLimit = 10
	b.Reserve = 0
	b.PersonLaneShare = 0.5
	g, s, _ := setupGovernor(t, b)
	start := time.Unix(1800000000, 0)
	s.SetTime(start)
	seedGrants(t, g, domain.Interactive, 1, start.Add(-30*time.Minute))
	seedGrants(t, g, domain.Photo, 5, start.Add(-10*time.Minute))
	// Live seeds must keep the ready marker so Acquire does not rebuild and
	// replace them from an empty call log.
	s.Set(g.keys(1)[5], "1")
	if p, w, e := acquire(t, g, domain.Photo); e != nil || p != nil || w != 30*time.Minute {
		t.Fatalf("photo reset: %v %s %v", p, w, e)
	}
}

func TestRollingRebuildReplacesLanesFromCallLog(t *testing.T) {
	g, s, c := setupGovernor(t, generousBudget())
	now := time.Now().Truncate(time.Millisecond)
	s.SetTime(now)
	// Orphan live members must not survive restore: the call log is authoritative.
	seedGrants(t, g, domain.Photo, 2, now.Add(-10*time.Minute))
	counters := Counters{Client: c, Prefix: g.Prefix}
	grants := []app.Grant{
		{Class: "person", At: now.Add(-59 * time.Minute), Member: "log:1"},
		{Class: "interactive", At: now.Add(-61 * time.Minute), Member: "log:2"},
		{Class: "photo", At: now.Add(-5 * time.Minute), Member: "log:3"},
	}
	if _, e := counters.Restore(t.Context(), app.Usage{KeyID: 1, Grants: grants}); e != nil {
		t.Fatal(e)
	}
	live, e := counters.Live(t.Context(), 1)
	if e != nil || live.Used != 2 || live.ByClass["person"] != 1 || live.ByClass["photo"] != 1 || live.ByClass["interactive"] != 0 || !live.ResetAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("restored: %+v %v", live, e)
	}
	s.SetTime(now.Add(time.Minute))
	live, e = counters.Live(t.Context(), 1)
	if e != nil || live.Used != 1 || live.ByClass["person"] != 0 || live.ByClass["photo"] != 1 {
		t.Fatalf("exact expiry: %+v %v", live, e)
	}
}

func TestRollingCalibrationKeepsSyntheticGrantsUntilTheirExpiry(t *testing.T) {
	b := generousBudget()
	b.HourlyLimit = 10
	b.Reserve = 0
	g, s, c := setupGovernor(t, b)
	start := time.Unix(1800000000, 0)
	s.SetTime(start)
	p, _, err := acquire(t, g, domain.Interactive)
	if err != nil || p == nil {
		t.Fatal(err)
	}
	s.SetTime(start.Add(30 * time.Minute))
	if err := p.Done(domain.Meta{Err: flickr.ErrQuotaExceeded}); err != nil {
		t.Fatal(err)
	}
	counters := Counters{Client: c, Prefix: g.Prefix}
	live, err := counters.Live(t.Context(), 1)
	if err != nil || live.Used != 10 || !live.CooldownUntil.Equal(start.Add(time.Hour)) {
		t.Fatalf("calibration %+v %v", live, err)
	}
	s.SetTime(start.Add(time.Hour))
	if p, _, e := acquire(t, g, domain.Interactive); e != nil || p == nil {
		t.Fatalf("oldest expired %v", e)
	}
	if p, w, e := acquire(t, g, domain.Interactive); e != nil || p != nil || w != 30*time.Minute {
		t.Fatalf("synthetics released early: %v %s %v", p, w, e)
	}
	s.SetTime(start.Add(90 * time.Minute))
	if p, _, e := acquire(t, g, domain.Interactive); e != nil || p == nil {
		t.Fatalf("synthetic expiry %v", e)
	}
}

func TestGrantRecorderUsesRedisClock(t *testing.T) {
	g, s, _ := setupGovernor(t, generousBudget())
	now := time.UnixMilli(1800000000123)
	s.SetTime(now)
	var recorded time.Time
	g.Recorder = recorderFn(func(_ context.Context, _ int64, _ string, at time.Time) error { recorded = at; return nil })
	if p, _, e := acquire(t, g, domain.Interactive); e != nil || p == nil || !recorded.Equal(now) {
		t.Fatalf("record time %s, want %s: %v", recorded, now, e)
	}
}

func TestSnapshotUsesRollingIntervalStart(t *testing.T) {
	g, s, c := setupGovernor(t, generousBudget())
	now := time.UnixMilli(1800000000123)
	s.SetTime(now)
	if p, _, e := acquire(t, g, domain.Interactive); e != nil || p == nil {
		t.Fatal(e)
	}
	usage, ok, e := (Counters{Client: c, Prefix: g.Prefix}).Snapshot(t.Context(), 1)
	if e != nil || !ok || usage.Used != 1 || !usage.WindowStart.Equal(now.Add(-time.Hour)) {
		t.Fatalf("snapshot %+v %t %v", usage, ok, e)
	}
}
