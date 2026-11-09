package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/maintenance/domain"
)

type retentionStoreFake struct {
	mu sync.Mutex

	locked                                          bool
	lockErr, listErr, rollupErr, detachErr, dropErr error
	parts                                           []domain.DailyPartition
	rollups                                         []time.Time
	detached                                        []string
	dropped                                         []string
	unlockCalls                                     int
}

func (s *retentionStoreFake) TryLock(context.Context) (func(), bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lockErr != nil {
		return nil, false, s.lockErr
	}
	if s.locked {
		return nil, false, nil
	}
	s.locked = true
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.locked = false
		s.unlockCalls++
	}, true, nil
}

func (s *retentionStoreFake) ListDailyPartitions(context.Context) ([]domain.DailyPartition, error) {
	return s.parts, s.listErr
}

func (s *retentionStoreFake) RollupDay(_ context.Context, day time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rollups = append(s.rollups, day)
	return s.rollupErr
}

func (s *retentionStoreFake) DetachConcurrently(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.detached = append(s.detached, name)
	return s.detachErr
}

func (s *retentionStoreFake) DropTable(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropped = append(s.dropped, name)
	return s.dropErr
}

type retentionSettingsFake struct {
	days      int
	dryRun    bool
	daysErr   error
	dryRunErr error
}

func (s retentionSettingsFake) RetentionDays(context.Context) (int, error) {
	return s.days, s.daysErr
}

func (s retentionSettingsFake) DryRun(context.Context) (bool, error) {
	return s.dryRun, s.dryRunErr
}

func TestDryRunDoesNotDrop(t *testing.T) {
	now := time.Date(2026, time.November, 9, 12, 0, 0, 0, time.UTC)
	old, err := domain.ParseDailyPartitionName("api_call_log_20261001")
	if err != nil {
		t.Fatal(err)
	}
	store := &retentionStoreFake{parts: []domain.DailyPartition{old}}
	job := Retention{
		Store:    store,
		Settings: retentionSettingsFake{days: 30, dryRun: true},
		Now:      func() time.Time { return now },
		Logger:   slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
	}
	if err := job.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.rollups) != 0 || len(store.detached) != 0 || len(store.dropped) != 0 {
		t.Fatalf("dry-run mutated state: rollups=%v detached=%v dropped=%v", store.rollups, store.detached, store.dropped)
	}
	if store.unlockCalls != 1 {
		t.Fatalf("unlock calls = %d", store.unlockCalls)
	}
}

func TestRunOnceDropsOldestExpiredOnly(t *testing.T) {
	now := time.Date(2026, time.November, 9, 12, 0, 0, 0, time.UTC)
	older, _ := domain.ParseDailyPartitionName("api_call_log_20260901")
	old, _ := domain.ParseDailyPartitionName("api_call_log_20261001")
	current, _ := domain.ParseDailyPartitionName("api_call_log_20261109")
	store := &retentionStoreFake{parts: []domain.DailyPartition{old, older, current}}
	job := Retention{
		Store:    store,
		Settings: retentionSettingsFake{days: 30},
		Now:      func() time.Time { return now },
	}
	if err := job.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.dropped) != 1 || store.dropped[0] != older.Name {
		t.Fatalf("dropped = %v, want [%s]", store.dropped, older.Name)
	}
	if len(store.detached) != 1 || store.detached[0] != older.Name {
		t.Fatalf("detached = %v", store.detached)
	}
	if len(store.rollups) != 1 || !store.rollups[0].Equal(older.Day) {
		t.Fatalf("rollups = %v", store.rollups)
	}
}

func TestRunOnceBusyAndErrors(t *testing.T) {
	store := &retentionStoreFake{locked: true}
	job := Retention{Store: store, Settings: retentionSettingsFake{days: 30}}
	if err := job.RunOnce(context.Background()); !errors.Is(err, ErrRetentionBusy) {
		t.Fatalf("busy err = %v", err)
	}
	if err := (Retention{}).RunOnce(context.Background()); err == nil {
		t.Fatal("unconfigured retention accepted")
	}
	boom := errors.New("boom")
	failing := &retentionStoreFake{lockErr: boom}
	if err := (Retention{Store: failing, Settings: retentionSettingsFake{}}).RunOnce(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("lock err = %v", err)
	}
}

func TestRunSkipsBusyAndStops(t *testing.T) {
	store := &retentionStoreFake{locked: true}
	var logged error
	job := Retention{Store: store, Settings: retentionSettingsFake{days: 30}, Interval: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		job.Run(ctx, func(err error) { logged = err })
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retention did not stop")
	}
	if logged != nil {
		t.Fatalf("busy reported as error: %v", logged)
	}
}

func TestRunOnceNothingToDropAndFailurePaths(t *testing.T) {
	now := time.Date(2026, time.November, 9, 12, 0, 0, 0, time.UTC)
	current, _ := domain.ParseDailyPartitionName("api_call_log_20261109")
	store := &retentionStoreFake{parts: []domain.DailyPartition{current}}
	job := Retention{
		Store:    store,
		Settings: retentionSettingsFake{days: 30},
		Now:      func() time.Time { return now },
		Logger:   slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
	}
	if err := job.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.dropped) != 0 {
		t.Fatalf("dropped current partition: %v", store.dropped)
	}

	boom := errors.New("settings down")
	if err := (Retention{Store: &retentionStoreFake{}, Settings: retentionSettingsFake{daysErr: boom}}).RunOnce(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("days err = %v", err)
	}
	if err := (Retention{Store: &retentionStoreFake{}, Settings: retentionSettingsFake{dryRunErr: boom}}).RunOnce(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("dry-run err = %v", err)
	}
	old, _ := domain.ParseDailyPartitionName("api_call_log_20261001")
	for _, tc := range []struct {
		name  string
		store *retentionStoreFake
	}{
		{name: "list", store: &retentionStoreFake{parts: []domain.DailyPartition{old}, listErr: boom}},
		{name: "rollup", store: &retentionStoreFake{parts: []domain.DailyPartition{old}, rollupErr: boom}},
		{name: "detach", store: &retentionStoreFake{parts: []domain.DailyPartition{old}, detachErr: boom}},
		{name: "drop", store: &retentionStoreFake{parts: []domain.DailyPartition{old}, dropErr: boom}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := (Retention{
				Store:    tc.store,
				Settings: retentionSettingsFake{days: 30},
				Now:      func() time.Time { return now },
			}).RunOnce(context.Background())
			if !errors.Is(err, boom) {
				t.Fatalf("err = %v", err)
			}
		})
	}

	var saw error
	failing := &retentionStoreFake{parts: []domain.DailyPartition{old}, dropErr: boom}
	Retention{
		Store:    failing,
		Settings: retentionSettingsFake{days: 30},
		Now:      func() time.Time { return now },
		Interval: time.Hour,
	}.tick(context.Background(), func(err error) { saw = err })
	if !errors.Is(saw, boom) {
		t.Fatalf("onError = %v", saw)
	}
}
