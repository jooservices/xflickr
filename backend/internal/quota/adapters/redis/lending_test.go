package redis

import (
	"strconv"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/quota/domain"
)

func TestIdlePhotoLaneLendsUnusedShare(t *testing.T) {
	budget := generousBudget()
	g, _, _ := setupGovernor(t, budget)
	got := drainLane(t, g, domain.Person)
	want := int(budget.HourlyLimit-budget.Reserve) - int(float64(budget.HourlyLimit)*budget.PhotoLaneFloorShare)
	if got != want {
		t.Fatalf("idle person grants = %d, want %d", got, want)
	}
}

func drainLane(t *testing.T, g Governor, class domain.Class) int {
	t.Helper()
	for n := 0; ; n++ {
		permit, _, err := acquire(t, g, class)
		if err != nil {
			t.Fatal(err)
		}
		if permit == nil {
			return n
		}
	}
}

func TestPhotoDemandTransitionsPreserveSpentCallsAndFloor(t *testing.T) {
	budget := generousBudget()
	g, server, _ := setupGovernor(t, budget)
	start := time.Unix(1800000000, 0)
	server.SetTime(start)
	if permit, _, err := acquire(t, g, domain.Photo); err != nil || permit == nil {
		t.Fatalf("photo: %v %v", permit, err)
	}
	full := int(float64(budget.HourlyLimit) * (1 - budget.PersonLaneShare))
	if got := drainLane(t, g, domain.Person); got != int(budget.HourlyLimit-budget.Reserve)-full {
		t.Fatalf("busy person grants = %d", got)
	}
	// A denied photo attempt still renews demand, here denied by the rate gate.
	server.HSet(g.keys(1)[0], "granted", "10000")
	server.FastForward(59 * time.Second)
	server.SetTime(start.Add(59 * time.Second))
	server.HSet(g.keys(1)[0], "second", strconv.FormatInt(start.Add(59*time.Second).Unix(), 10))
	if permit, _, err := acquire(t, g, domain.Photo); err != nil || permit != nil {
		t.Fatalf("rate-denied photo = %v %v", permit, err)
	}
	server.FastForward(time.Second)
	server.SetTime(start.Add(60 * time.Second))
	if permit, _, err := acquire(t, g, domain.Person); err != nil || permit != nil {
		t.Fatalf("demand expired too soon: %v %v", permit, err)
	}
	server.FastForward(59 * time.Second)
	server.SetTime(start.Add(119 * time.Second))
	floor := int(float64(budget.HourlyLimit) * budget.PhotoLaneFloorShare)
	if got := drainLane(t, g, domain.Person); got != full-floor {
		t.Fatalf("lent grants = %d, want %d", got, full-floor)
	}
	// Returning photo work can immediately spend its unspent floor; person work
	// waits again without revoking any calls that were already granted.
	if permit, _, err := acquire(t, g, domain.Photo); err != nil || permit == nil {
		t.Fatalf("returning photo: %v %v", permit, err)
	}
	if permit, _, err := acquire(t, g, domain.Person); err != nil || permit != nil {
		t.Fatalf("busy person after lending: %v %v", permit, err)
	}
	if got := drainLane(t, g, domain.Photo); got != floor-2 {
		t.Fatalf("remaining floor = %d, want %d", got, floor-2)
	}
	if got := drainLane(t, g, domain.Interactive); got != int(budget.Reserve) {
		t.Fatalf("interactive reserve = %d", got)
	}
}

func TestIdlePhotoLaneSimulatedHour(t *testing.T) {
	budget := generousBudget()
	budget.PerSecondRate, budget.Burst = 4, 5
	g, server, _ := setupGovernor(t, budget)
	start := time.Unix(1800000000, 0)
	total := 0
	for sec := 0; sec < 3600; sec++ {
		server.SetTime(start.Add(time.Duration(sec) * time.Second))
		grants := drainLane(t, g, domain.Person)
		if grants > int(budget.Burst) {
			t.Fatalf("second %d granted %d above burst %d", sec, grants, budget.Burst)
		}
		total += grants
	}
	ceiling := int(budget.HourlyLimit-budget.Reserve) - int(float64(budget.HourlyLimit)*budget.PhotoLaneFloorShare)
	if total > ceiling || float64(total) < 0.95*float64(ceiling) {
		t.Fatalf("hour granted %d; ceiling %d", total, ceiling)
	}
	t.Logf("person grants %d / ceiling %d, no second above burst %d", total, ceiling, budget.Burst)
}

func TestPersonRetriesWhenPhotoDemandExpires(t *testing.T) {
	budget := generousBudget()
	g, _, _ := setupGovernor(t, budget)
	if permit, _, err := acquire(t, g, domain.Photo); err != nil || permit == nil {
		t.Fatalf("photo %v %v", permit, err)
	}
	drainLane(t, g, domain.Person)
	permit, wait, err := acquire(t, g, domain.Person)
	if err != nil || permit != nil || wait > 60*time.Second || wait <= 0 {
		t.Fatalf("demand retry = %v %s %v", permit, wait, err)
	}
}

func TestRefusedPhotoAttemptsRefreshDemand(t *testing.T) {
	for _, reason := range []string{"hourly", "cooldown", "rebuild"} {
		t.Run(reason, func(t *testing.T) {
			budget := generousBudget()
			g, server, _ := setupGovernor(t, budget)
			now := time.Unix(1800000000, 0)
			server.SetTime(now)
			keys := g.keys(1)
			switch reason {
			case "hourly":
				seedGrants(t, g, domain.Interactive, int(budget.HourlyLimit), now)
			case "cooldown":
				server.Set(keys[3], strconv.FormatInt(now.Add(time.Hour).UnixMilli(), 10))
			case "rebuild":
				// Calling the script directly isolates readiness rejection from the Go rebuild retry.
				result, err := acquireScript.Run(t.Context(), g.Client, keys, budget.PerSecondRate, budget.Burst, budget.HourlyLimit, budget.Reserve, budget.PersonLaneShare, "photo", 1, budget.IdlePhotoProtection()).Slice()
				if err != nil || result[2] != "rebuild" {
					t.Fatalf("rebuild result %v %v", result, err)
				}
			}
			if reason != "rebuild" {
				if permit, _, err := acquire(t, g, domain.Photo); err != nil || permit != nil {
					t.Fatalf("refused photo %v %v", permit, err)
				}
			}
			if ttl := server.TTL(keys[6]); ttl != 60*time.Second {
				t.Fatalf("demand TTL %s", ttl)
			}
		})
	}
}

func TestIdleProtectionCannotExceedPhotoAllocation(t *testing.T) {
	budget := generousBudget()
	budget.PersonLaneShare, budget.PhotoLaneFloorShare = 0.75, 0.5
	g, _, _ := setupGovernor(t, budget)
	if got := drainLane(t, g, domain.Person); got != 2600 {
		t.Fatalf("idle person grants = %d, want 2600", got)
	}
}
