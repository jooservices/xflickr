package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

type partitionStoreFake struct {
	oldest, through                        time.Time
	rollupFrom, rollupTo                   time.Time
	pruneBefore                            time.Time
	maintainErr, rollupErr, pruneErr       error
	maintainCalls, rollupCalls, pruneCalls int
	maintainSignal                         chan struct{}
}

func (s *partitionStoreFake) MaintainCallLogPartitions(_ context.Context, oldest, through time.Time) error {
	s.maintainCalls++
	s.oldest, s.through = oldest, through
	if s.maintainSignal != nil {
		select {
		case s.maintainSignal <- struct{}{}:
		default:
		}
	}
	return s.maintainErr
}

func (s *partitionStoreFake) RollupCalls(_ context.Context, from, until time.Time) error {
	s.rollupCalls++
	s.rollupFrom, s.rollupTo = from, until
	return s.rollupErr
}

func (s *partitionStoreFake) PruneCallRollups(_ context.Context, before time.Time) error {
	s.pruneCalls++
	s.pruneBefore = before
	return s.pruneErr
}

func TestPartitionsMaintainOnceUsesConfiguredWindows(t *testing.T) {
	store := &partitionStoreFake{}
	now := time.Date(2026, time.October, 28, 12, 45, 0, 0, time.FixedZone("test", -5*60*60))
	manager := Partitions{Store: store, RetentionDays: 14, Now: func() time.Time { return now }}
	if err := manager.MaintainOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	utcToday := time.Date(2026, time.October, 28, 0, 0, 0, 0, time.UTC)
	if !store.oldest.Equal(utcToday.AddDate(0, 0, -14)) || !store.through.Equal(utcToday.AddDate(0, 0, 7)) {
		t.Fatalf("partition range = %s..%s", store.oldest, store.through)
	}
	if !store.rollupFrom.Equal(time.Date(2026, time.October, 26, 17, 0, 0, 0, time.UTC)) || !store.rollupTo.Equal(time.Date(2026, time.October, 28, 17, 0, 0, 0, time.UTC)) {
		t.Fatalf("rollup range = %s..%s", store.rollupFrom, store.rollupTo)
	}
	if !store.pruneBefore.Equal(utcToday.AddDate(0, 0, -CallLogRollupRetentionDays)) {
		t.Fatalf("rollup cutoff = %s", store.pruneBefore)
	}
	if store.maintainCalls != 1 || store.rollupCalls != 1 || store.pruneCalls != 1 {
		t.Fatalf("maintenance calls = partitions %d rollups %d prune %d", store.maintainCalls, store.rollupCalls, store.pruneCalls)
	}
}

func TestPartitionsMaintenanceDefaultsAndErrors(t *testing.T) {
	base := time.Date(2026, time.October, 28, 0, 0, 0, 0, time.UTC)
	store := &partitionStoreFake{rollupErr: errors.New("rollup failed")}
	manager := Partitions{Store: store, Now: func() time.Time { return base }}
	if err := manager.MaintainOnce(context.Background()); err == nil {
		t.Fatal("rollup error was ignored")
	}
	if !store.oldest.Equal(base.AddDate(0, 0, -DefaultCallLogRetentionDays)) || !store.through.Equal(base.AddDate(0, 0, CallLogPartitionHorizonDays)) {
		t.Fatalf("default partition range = %s..%s", store.oldest, store.through)
	}
	if store.pruneCalls != 0 {
		t.Fatal("rollups were pruned after rollup failed")
	}
	if err := (Partitions{}).MaintainOnce(context.Background()); err == nil {
		t.Fatal("unconfigured manager was accepted")
	}
}

func TestPartitionsRunMaintainsAtStartupAndStops(t *testing.T) {
	store := &partitionStoreFake{maintainSignal: make(chan struct{}, 1)}
	manager := Partitions{Store: store, Interval: time.Hour, Now: func() time.Time { return time.Now().UTC() }}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		manager.Run(ctx, nil)
	}()
	select {
	case <-store.maintainSignal:
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("startup maintenance did not run")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("partition manager did not stop")
	}
}
