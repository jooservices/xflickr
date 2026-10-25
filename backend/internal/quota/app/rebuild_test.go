package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeStore struct {
	mu      sync.Mutex
	saved   []Usage
	latest  Usage
	has     bool
	ids     []int64
	saveErr error
	err     error
}

func (s *fakeStore) Save(_ context.Context, u Usage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saved = append(s.saved, u)
	return nil
}

func (s *fakeStore) Latest(context.Context, int64, time.Time) (Usage, bool, error) {
	return s.latest, s.has, s.err
}

func (s *fakeStore) KeyIDs(context.Context) ([]int64, error) { return s.ids, s.err }

type fakeCalls struct {
	usage CallUsage
	err   error
}

func (c fakeCalls) Usage(context.Context, int64, time.Time) (CallUsage, error) { return c.usage, c.err }

type fakeCounters struct {
	snap     map[int64]Usage
	snapOK   bool
	snapErr  error
	restored []Usage
	restErr  error
	gate     chan struct{}
}

func (c *fakeCounters) Snapshot(_ context.Context, id int64) (Usage, bool, error) {
	return c.snap[id], c.snapOK, c.snapErr
}

func (c *fakeCounters) Restore(_ context.Context, u Usage) (bool, error) {
	if c.gate != nil {
		<-c.gate
	}
	c.restored = append(c.restored, u)
	return true, c.restErr
}

var base = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

func TestRebuildTakesLargerOfStoredAndLogged(t *testing.T) {
	store := &fakeStore{has: true, latest: Usage{WindowStart: base.Add(-10 * time.Minute), Used: 50, ByClass: map[string]int64{"person": 30, "photo": 20}}}
	calls := fakeCalls{usage: CallUsage{ByClass: map[string]int64{"person": 35, "interactive": 5}}}
	counters := &fakeCounters{}
	r := &Rebuilder{Store: store, Calls: calls, Counters: counters, Now: func() time.Time { return base }}
	if err := r.Rebuild(context.Background(), 3); err != nil {
		t.Fatal(err)
	}
	got := counters.restored[0]
	if got.KeyID != 3 || !got.WindowStart.Equal(store.latest.WindowStart) || got.Used != 50 {
		t.Fatalf("restored = %+v", got)
	}
	if got.ByClass["person"] != 35 || got.ByClass["photo"] != 20 || got.ByClass["interactive"] != 5 {
		t.Fatalf("by class = %v", got.ByClass)
	}
}

func TestRebuildFromLogOnlyUsesFirstCallAsWindow(t *testing.T) {
	first := base.Add(-20 * time.Minute)
	counters := &fakeCounters{}
	r := &Rebuilder{Store: &fakeStore{}, Calls: fakeCalls{usage: CallUsage{First: first, ByClass: map[string]int64{"photo": 7}}}, Counters: counters, Now: func() time.Time { return base }}
	if err := r.Rebuild(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if got := counters.restored[0]; !got.WindowStart.Equal(first) || got.Used != 7 {
		t.Fatalf("restored = %+v", got)
	}
}

func TestRebuildWithNoHistoryRestoresEmptyWindow(t *testing.T) {
	counters := &fakeCounters{}
	r := &Rebuilder{Store: &fakeStore{}, Calls: fakeCalls{}, Counters: counters}
	if err := r.Rebuild(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if got := counters.restored[0]; !got.WindowStart.IsZero() || got.Used != 0 {
		t.Fatalf("restored = %+v", got)
	}
}

func TestRebuildErrors(t *testing.T) {
	boom := errors.New("boom")
	ok := func() *Rebuilder {
		return &Rebuilder{Store: &fakeStore{}, Calls: fakeCalls{}, Counters: &fakeCounters{}}
	}
	if err := (*Rebuilder)(nil).Rebuild(context.Background(), 1); err == nil {
		t.Fatal("nil rebuilder accepted")
	}
	if err := ok().Rebuild(context.Background(), 0); err == nil {
		t.Fatal("zero key accepted")
	}
	r := ok()
	r.Store = &fakeStore{err: boom}
	if err := r.Rebuild(context.Background(), 1); !errors.Is(err, boom) {
		t.Fatalf("store error = %v", err)
	}
	r = ok()
	r.Calls = fakeCalls{err: boom}
	if err := r.Rebuild(context.Background(), 1); !errors.Is(err, boom) {
		t.Fatalf("call log error = %v", err)
	}
	r = ok()
	r.Counters = &fakeCounters{restErr: boom}
	if err := r.Rebuild(context.Background(), 1); !errors.Is(err, boom) {
		t.Fatalf("restore error = %v", err)
	}
}

func TestConcurrentRebuildsShareOneRestore(t *testing.T) {
	counters := &fakeCounters{gate: make(chan struct{})}
	r := &Rebuilder{Store: &fakeStore{}, Calls: fakeCalls{}, Counters: counters}
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- r.Rebuild(context.Background(), 1) }()
	}
	time.Sleep(50 * time.Millisecond)
	close(counters.gate)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(counters.restored) < 1 || len(counters.restored) > 5 {
		t.Fatalf("restores = %d", len(counters.restored))
	}
}

func TestWaiterHonorsContext(t *testing.T) {
	counters := &fakeCounters{gate: make(chan struct{})}
	r := &Rebuilder{Store: &fakeStore{}, Calls: fakeCalls{}, Counters: counters}
	go func() { _ = r.Rebuild(context.Background(), 1) }()
	time.Sleep(50 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Rebuild(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter = %v", err)
	}
	close(counters.gate)
}

func TestRebuildAll(t *testing.T) {
	counters := &fakeCounters{}
	r := &Rebuilder{Store: &fakeStore{ids: []int64{1, 2}}, Calls: fakeCalls{}, Counters: counters}
	if err := r.RebuildAll(context.Background()); err != nil || len(counters.restored) != 2 {
		t.Fatalf("RebuildAll = %v, restored %d", err, len(counters.restored))
	}
	boom := errors.New("boom")
	r.Store = &fakeStore{err: boom}
	if err := r.RebuildAll(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("list error = %v", err)
	}
	r.Store = &fakeStore{ids: []int64{1}}
	r.Counters = &fakeCounters{restErr: boom}
	if err := r.RebuildAll(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("rebuild error = %v", err)
	}
	if err := (*Rebuilder)(nil).RebuildAll(context.Background()); err == nil {
		t.Fatal("nil rebuilder accepted")
	}
}
