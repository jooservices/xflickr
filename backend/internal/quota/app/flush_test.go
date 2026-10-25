package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFlushOnceSavesLiveCountersAndSkipsLostOnes(t *testing.T) {
	store := &fakeStore{ids: []int64{1}}
	counters := &fakeCounters{snapOK: true, snap: map[int64]Usage{1: {KeyID: 1, WindowStart: base, Used: 9}}}
	f := Flusher{Store: store, Counters: counters}
	if err := f.FlushOnce(context.Background()); err != nil || len(store.saved) != 1 || store.saved[0].Used != 9 {
		t.Fatalf("flush = %v saved %+v", err, store.saved)
	}
	counters.snapOK = false
	store.saved = nil
	if err := f.FlushOnce(context.Background()); err != nil || len(store.saved) != 0 {
		t.Fatalf("lost counters flushed: %v %+v", err, store.saved)
	}
	counters.snapOK = true
	counters.snap = map[int64]Usage{1: {KeyID: 1}}
	if err := f.FlushOnce(context.Background()); err != nil || len(store.saved) != 0 {
		t.Fatalf("empty window flushed: %v %+v", err, store.saved)
	}
}

func TestFlushOnceJoinsErrors(t *testing.T) {
	boom := errors.New("boom")
	counters := &fakeCounters{snapOK: true, snap: map[int64]Usage{2: {KeyID: 2, WindowStart: base}}}
	store := &fakeStore{ids: []int64{1, 2}, saveErr: boom}
	f := Flusher{Store: store, Counters: counters}
	if err := f.FlushOnce(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("save error = %v", err)
	}
	counters.snapErr = boom
	if err := f.FlushOnce(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("snapshot error = %v", err)
	}
	if err := (Flusher{Store: &fakeStore{err: boom}, Counters: counters}).FlushOnce(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("list error = %v", err)
	}
	if err := (Flusher{}).FlushOnce(context.Background()); err == nil {
		t.Fatal("unconfigured flusher accepted")
	}
}

func TestRunFlushesOnTickAndOnShutdown(t *testing.T) {
	boom := errors.New("boom")
	store := &fakeStore{ids: []int64{1}, saveErr: boom}
	counters := &fakeCounters{snapOK: true, snap: map[int64]Usage{1: {KeyID: 1, WindowStart: base, Used: 1}}}
	reported := make(chan error, 8)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Flusher{Store: store, Counters: counters, Interval: 5 * time.Millisecond}.Run(ctx, func(err error) { reported <- err })
		close(done)
	}()
	select {
	case err := <-reported:
		if !errors.Is(err, boom) {
			t.Fatalf("reported %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no flush on tick")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
}

func TestRunDefaultsInterval(_ *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	Flusher{Store: &fakeStore{}, Counters: &fakeCounters{}}.Run(ctx, nil)
}

type fakePruner struct {
	before time.Time
	err    error
}

func (p *fakePruner) Prune(_ context.Context, before time.Time) error {
	p.before = before
	return p.err
}

func TestFlushOncePrunesCallLog(t *testing.T) {
	pruner := &fakePruner{}
	f := Flusher{Store: &fakeStore{}, Counters: &fakeCounters{}, Pruner: pruner, Now: func() time.Time { return base }}
	if err := f.FlushOnce(context.Background()); err != nil || !pruner.before.Equal(base.Add(-2*Window)) {
		t.Fatalf("prune = %v before %v", err, pruner.before)
	}
	boom := errors.New("boom")
	pruner.err = boom
	f.Now = nil
	if err := f.FlushOnce(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("prune error = %v", err)
	}
}
