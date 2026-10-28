package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/crawl/domain"
	settingsdomain "github.com/jooservices/xflickr/internal/settings/domain"
)

type cycleStoreFake struct {
	userID            int64
	now               time.Time
	checkInterval     time.Duration
	reconcileInterval time.Duration
	called            bool
	result            bool
	err               error
}

type cycleUsersFake struct {
	users []int64
	err   error
}

func (f cycleUsersFake) UsersWithInterests(context.Context) ([]int64, error) { return f.users, f.err }

func (f *cycleStoreFake) ScheduleNext(_ context.Context, userID int64, now time.Time, checkInterval, reconcileInterval time.Duration) (bool, error) {
	f.called, f.userID, f.now, f.checkInterval, f.reconcileInterval = true, userID, now, checkInterval, reconcileInterval
	return f.result, f.err
}

type cycleSettingsFake struct {
	effective settingsdomain.Effective
	err       error
	errByUser map[int64]error
}

func (f cycleSettingsFake) Effective(_ context.Context, userID int64) (settingsdomain.Effective, error) {
	if err := f.errByUser[userID]; err != nil {
		return settingsdomain.Effective{}, err
	}
	return f.effective, f.err
}

func TestCycleRunUserLoadsSettingsAndSchedulesNextPerson(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	store := &cycleStoreFake{result: true}
	cycle := Cycle{Store: store, Settings: cycleSettingsFake{effective: settingsdomain.Effective{ChangeCheckInterval: 3 * time.Hour}}, Now: func() time.Time { return now }}
	got, err := cycle.RunUser(context.Background(), 12)
	if err != nil || !got || !store.called || store.userID != 12 || !store.now.Equal(now) || store.checkInterval != 3*time.Hour || store.reconcileInterval != defaultReconcileInterval {
		t.Fatalf("RunUser() = %v, %v; store=%+v", got, err, store)
	}
}

func TestCycleRunUserValidatesDependenciesAndErrors(t *testing.T) {
	if _, err := (Cycle{}).RunUser(context.Background(), 1); !errors.Is(err, errCycleIncomplete) {
		t.Fatalf("missing dependencies error = %v", err)
	}
	store := &cycleStoreFake{}
	if _, err := (Cycle{Store: store, Settings: cycleSettingsFake{}}).RunUser(nil, 1); !errors.Is(err, errCycleIncomplete) {
		t.Fatalf("nil context error = %v", err)
	}
	if _, err := (Cycle{Store: store, Settings: cycleSettingsFake{}}).RunUser(context.Background(), 0); !errors.Is(err, errCycleIncomplete) {
		t.Fatalf("invalid user error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Cycle{Store: store, Settings: cycleSettingsFake{}}).RunUser(canceled, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v", err)
	}
	settingsErr := errors.New("settings unavailable")
	if _, err := (Cycle{Store: store, Settings: cycleSettingsFake{err: settingsErr}}).RunUser(context.Background(), 1); !errors.Is(err, settingsErr) || store.called {
		t.Fatalf("settings error = %v, store called=%v", err, store.called)
	}
	store.err = errors.New("database unavailable")
	if _, err := (Cycle{Store: store, Settings: cycleSettingsFake{}}).RunUser(context.Background(), 1); !errors.Is(err, store.err) {
		t.Fatalf("store error = %v", err)
	}
}

func TestCycleRunOnceSchedulesEveryUser(t *testing.T) {
	store := &cycleStoreFake{result: true}
	checkSettings := &cycleSettingsTracker{errors: map[int64]error{7: errors.New("settings unavailable")}}
	cycle := Cycle{Store: store, Users: cycleUsersFake{users: []int64{7, 8}}, Settings: checkSettings}
	if err := cycle.RunOnce(context.Background()); err != nil || !store.called || store.userID != 8 {
		t.Fatalf("RunOnce() = %v, store=%+v", err, store)
	}
	if !reflect.DeepEqual(checkSettings.visited, []int64{7, 8}) || store.userID != 8 {
		t.Fatalf("RunOnce did not continue after user failure: users=%v final_user=%d", checkSettings.visited, store.userID)
	}
	if err := (Cycle{Store: store, Settings: cycleSettingsFake{}}).RunOnce(context.Background()); !errors.Is(err, errCycleIncomplete) {
		t.Fatalf("missing user reader error = %v", err)
	}
	usersErr := errors.New("user query failed")
	cycle.Users = cycleUsersFake{err: usersErr}
	if err := cycle.RunOnce(context.Background()); !errors.Is(err, usersErr) {
		t.Fatalf("user query error = %v", err)
	}
}

type cycleSettingsTracker struct {
	visited []int64
	errors  map[int64]error
}

func (f *cycleSettingsTracker) Effective(_ context.Context, userID int64) (settingsdomain.Effective, error) {
	f.visited = append(f.visited, userID)
	return settingsdomain.Effective{}, f.errors[userID]
}

func TestSelectCyclePersonWalksInOrderAndWrapsAfterLast(t *testing.T) {
	people := []domain.Person{{NSID: "c"}, {NSID: "a"}, {NSID: "b"}}
	first := SelectCyclePerson(people, "")
	if !first.Found || first.Person.NSID != "a" || first.Cursor != "a" || first.Wrapped {
		t.Fatalf("first cycle selection = %#v", first)
	}
	second := SelectCyclePerson(people, first.Cursor)
	if second.Person.NSID != "b" || second.Wrapped {
		t.Fatalf("second cycle selection = %#v", second)
	}
	third := SelectCyclePerson(people, second.Cursor)
	if third.Person.NSID != "c" || third.Wrapped {
		t.Fatalf("third cycle selection = %#v", third)
	}
	wrapped := SelectCyclePerson(people, third.Cursor)
	if wrapped.Person.NSID != "a" || !wrapped.Wrapped {
		t.Fatalf("wrapped cycle selection = %#v", wrapped)
	}
	if empty := SelectCyclePerson(nil, ""); empty.Found || empty.Cursor != "" {
		t.Fatalf("empty selection = %#v", empty)
	}
}

func TestSelectCyclePersonDoesNotMutateInput(t *testing.T) {
	people := []domain.Person{{NSID: "b"}, {NSID: "a"}}
	want := append([]domain.Person(nil), people...)
	_ = SelectCyclePerson(people, "")
	if !reflect.DeepEqual(people, want) {
		t.Fatalf("input reordered: got %#v want %#v", people, want)
	}
}

func TestEffectiveCheckIntervalUsesConfiguredOrDefault(t *testing.T) {
	if got := EffectiveCheckInterval(settingsdomain.Effective{ChangeCheckInterval: 2 * time.Hour}); got != 2*time.Hour {
		t.Fatalf("configured interval = %s", got)
	}
	if got := EffectiveCheckInterval(settingsdomain.Effective{}); got != 24*time.Hour {
		t.Fatalf("default interval = %s", got)
	}
	now := time.Now()
	if CycleDue(now.Add(-time.Hour), now, time.Hour) != true || CycleDue(now, now, time.Hour) {
		t.Fatal("cycle due check did not follow interval")
	}
}

func TestEffectiveReconcileIntervalUsesConfiguredOrThirtyDayDefault(t *testing.T) {
	if got := EffectiveReconcileInterval(settingsdomain.Effective{ReconcileInterval: 12 * time.Hour}); got != 12*time.Hour {
		t.Fatalf("configured interval = %s", got)
	}
	if got := EffectiveReconcileInterval(settingsdomain.Effective{}); got != 30*24*time.Hour {
		t.Fatalf("default interval = %s", got)
	}
}
