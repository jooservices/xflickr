package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/quota/domain"
)

var stateNow = time.Date(2026, 10, 25, 12, 30, 0, 0, time.UTC)

type fakeDirectory struct {
	keys   []KeyInfo
	err    error
	gotUID int64
}

func (d *fakeDirectory) Owned(_ context.Context, userID, keyID int64) (KeyInfo, bool, error) {
	d.gotUID = userID
	for _, k := range d.keys {
		if k.ID == keyID && k.UserID == userID {
			return k, true, d.err
		}
	}
	return KeyInfo{}, false, d.err
}

func (d *fakeDirectory) Active(context.Context) ([]KeyInfo, error) { return d.keys, d.err }

type fakeBudgets map[int64]domain.Budget

func (b fakeBudgets) Budget(_ context.Context, id int64) (domain.Budget, error) {
	budget, ok := b[id]
	if !ok {
		return domain.Budget{}, ErrKeyUnavailable
	}
	return budget, nil
}

type fakeLive struct {
	live map[int64]Live
	err  error
}

func (l fakeLive) Live(_ context.Context, id int64) (Live, error) { return l.live[id], l.err }

type fakeHistory struct {
	usage Usage
	has   bool
	err   error
	from  time.Time
	to    time.Time
}

func (h *fakeHistory) Previous(_ context.Context, _ int64, from, to time.Time) (Usage, bool, error) {
	h.from, h.to = from, to
	return h.usage, h.has, h.err
}

func newStates(live map[int64]Live, history *fakeHistory) States {
	return States{
		Keys:    &fakeDirectory{keys: []KeyInfo{{ID: 1, UserID: 7, Label: "main"}, {ID: 2, UserID: 8, Label: "other"}, {ID: 3, UserID: 8, Label: "gone"}}},
		Budgets: fakeBudgets{1: {HourlyLimit: 100, Reserve: 10}, 2: {HourlyLimit: 50}},
		Live:    fakeLive{live: live},
		History: history,
		Now:     func() time.Time { return stateNow },
	}
}

func TestForOwnerMapsLiveCounters(t *testing.T) {
	start := stateNow.Add(-30 * time.Minute)
	history := &fakeHistory{usage: Usage{Used: 100}, has: true}
	states := newStates(map[int64]Live{1: {WindowStart: start, Used: 80, ByClass: map[string]int64{"interactive": 5, "person": 40, "photo": 35}}}, history)
	got, err := states.ForOwner(context.Background(), 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := KeyState{
		Key: KeyInfo{ID: 1, UserID: 7, Label: "main"}, Used: 80, Limit: 100, Reserve: 10,
		ResetAt: start.Add(time.Hour), State: domain.StateOK, Trend: domain.TrendRising,
		Lanes: LaneUsage{Interactive: 5, Person: 40, Photo: 35},
	}
	if got != want {
		t.Fatalf("state = %+v\nwant   %+v", got, want)
	}
	if !history.to.Equal(start) || !history.from.Equal(start.Add(-2*time.Hour)) {
		t.Fatalf("history range = %s..%s", history.from, history.to)
	}
}

func TestStateCoolingExhaustedAndIdle(t *testing.T) {
	start := stateNow.Add(-10 * time.Minute)
	states := newStates(map[int64]Live{
		1: {WindowStart: start, Used: 100, CooldownUntil: start.Add(time.Hour)},
		2: {WindowStart: start, Used: 50},
	}, &fakeHistory{})
	cooling, err := states.ForOwner(context.Background(), 7, 1)
	if err != nil || cooling.State != domain.StateCooling {
		t.Fatalf("cooling = %+v %v", cooling, err)
	}
	exhausted, err := states.ForOwner(context.Background(), 8, 2)
	if err != nil || exhausted.State != domain.StateExhausted {
		t.Fatalf("exhausted = %+v %v", exhausted, err)
	}

	// A window that ended is ignored: counters read as zero and no reset time.
	stale := newStates(map[int64]Live{1: {WindowStart: stateNow.Add(-2 * time.Hour), Used: 100, CooldownUntil: stateNow.Add(-time.Hour), ByClass: map[string]int64{"photo": 9}}}, &fakeHistory{})
	idle, err := stale.ForOwner(context.Background(), 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	if idle.State != domain.StateOK || idle.Used != 0 || !idle.ResetAt.IsZero() || idle.Lanes != (LaneUsage{}) {
		t.Fatalf("idle = %+v", idle)
	}
}

func TestForOwnerErrors(t *testing.T) {
	states := newStates(nil, &fakeHistory{})
	if _, err := states.ForOwner(context.Background(), 8, 1); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("other user's key: %v", err)
	}
	if _, err := states.ForOwner(context.Background(), 8, 3); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("key without budget: %v", err)
	}
	boom := errors.New("boom")
	failing := newStates(nil, &fakeHistory{})
	failing.Keys = &fakeDirectory{err: boom}
	if _, err := failing.ForOwner(context.Background(), 7, 1); !errors.Is(err, boom) {
		t.Fatalf("directory error: %v", err)
	}
	live := newStates(nil, &fakeHistory{})
	live.Live = fakeLive{err: boom}
	if _, err := live.ForOwner(context.Background(), 7, 1); !errors.Is(err, boom) {
		t.Fatalf("live error: %v", err)
	}
	history := newStates(nil, &fakeHistory{err: boom})
	if _, err := history.ForOwner(context.Background(), 7, 1); !errors.Is(err, boom) {
		t.Fatalf("history error: %v", err)
	}
	if _, err := (States{}).ForOwner(context.Background(), 1, 1); err == nil {
		t.Fatal("unconfigured service accepted")
	}
}

func TestAllSkipsUnavailableKeysAndPropagatesErrors(t *testing.T) {
	states := newStates(map[int64]Live{}, &fakeHistory{})
	got, err := states.All(context.Background())
	if err != nil || len(got) != 2 || got[0].Key.ID != 1 || got[1].Key.ID != 2 {
		t.Fatalf("All = %+v %v", got, err)
	}
	boom := errors.New("boom")
	states.Keys = &fakeDirectory{err: boom}
	if _, err := states.All(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("directory error: %v", err)
	}
	states = newStates(nil, &fakeHistory{})
	states.Live = fakeLive{err: boom}
	if _, err := states.All(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("live error: %v", err)
	}
	if _, err := (States{}).All(context.Background()); err == nil {
		t.Fatal("unconfigured service accepted")
	}
}

func TestStatesDefaultsToWallClock(t *testing.T) {
	states := newStates(nil, &fakeHistory{})
	states.Now = nil
	got, err := states.ForOwner(context.Background(), 7, 1)
	if err != nil || !got.ResetAt.IsZero() {
		t.Fatalf("state = %+v %v", got, err)
	}
}
