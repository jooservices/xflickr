package ops

import (
	"context"
	"errors"
	"testing"
	"time"
)

type countingStore struct {
	calls int
	err   error
}

func (s *countingStore) Snapshot(context.Context) (Snapshot, error) {
	s.calls++
	if s.err != nil {
		return Snapshot{}, s.err
	}
	return Snapshot{FailedWorkItems: int64(s.calls)}, nil
}

func TestCachedStoreLoadsOncePerTTLAndNeverCachesFailures(t *testing.T) {
	now := time.Unix(1000, 0)
	inner := &countingStore{}
	cache := &CachedStore{Inner: inner, TTL: time.Minute, Now: func() time.Time { return now }}
	for range 3 {
		snapshot, err := cache.Snapshot(t.Context())
		if err != nil || snapshot.FailedWorkItems != 1 {
			t.Fatalf("snapshot = %+v err=%v", snapshot, err)
		}
	}
	if inner.calls != 1 {
		t.Fatalf("inner loads = %d, want 1", inner.calls)
	}
	now = now.Add(2 * time.Minute)
	if snapshot, _ := cache.Snapshot(t.Context()); snapshot.FailedWorkItems != 2 {
		t.Fatalf("expired snapshot = %+v", snapshot)
	}
	now = now.Add(2 * time.Minute)
	inner.err = errors.New("down")
	if _, err := cache.Snapshot(t.Context()); err == nil {
		t.Fatal("a failed load must surface")
	}
	inner.err = nil
	if snapshot, err := cache.Snapshot(t.Context()); err != nil || snapshot.FailedWorkItems != 4 {
		t.Fatalf("after failure snapshot = %+v err=%v", snapshot, err)
	}
	zero := &CachedStore{Inner: &countingStore{}}
	_, _ = zero.Snapshot(t.Context())
	_, _ = zero.Snapshot(t.Context())
	if zero.Inner.(*countingStore).calls != 1 {
		t.Fatal("default TTL must apply")
	}
}
