package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Window is the length of one hourly quota window.
const Window = time.Hour

// Usage is one key's hourly consumption, as stored in quota_usage and as read
// from the Redis counters.
type Usage struct {
	KeyID       int64
	WindowStart time.Time
	Used        int64
	ByClass     map[string]int64
}

// CallUsage summarizes the calls recorded in api_call_log since a given time.
// First is the time of the earliest such call and is zero when there are none.
type CallUsage struct {
	First   time.Time
	ByClass map[string]int64
}

// UsageStore persists flushed usage.
type UsageStore interface {
	// Save records usage for its window. It never lowers a stored value.
	Save(context.Context, Usage) error
	// Latest returns the newest window starting after since, if any.
	Latest(ctx context.Context, keyID int64, since time.Time) (Usage, bool, error)
	// KeyIDs lists the keys whose usage is tracked.
	KeyIDs(context.Context) ([]int64, error)
}

// CallLog reports calls already made, independent of Redis.
type CallLog interface {
	Usage(ctx context.Context, keyID int64, since time.Time) (CallUsage, error)
}

// CounterStore reads and restores the Redis counters of a key.
type CounterStore interface {
	// Snapshot returns the live counters. ok is false when the counters were
	// lost or have not been rebuilt, so they must not be flushed.
	Snapshot(ctx context.Context, keyID int64) (usage Usage, ok bool, err error)
	// Restore writes the usage back unless the counters are already ready. It
	// reports whether it wrote them.
	Restore(ctx context.Context, usage Usage) (bool, error)
}

// Rebuilder restores a key's Redis counters from quota_usage and api_call_log.
// For each class it takes the larger of the two sources, so a rebuild can only
// over-count the current window, never under-count it.
type Rebuilder struct {
	Store    UsageStore
	Calls    CallLog
	Counters CounterStore
	Now      func() time.Time

	mu       sync.Mutex
	inflight map[int64]*rebuild
}

type rebuild struct {
	done chan struct{}
	err  error
}

// Rebuild restores keyID's counters. Concurrent calls for one key share a
// single rebuild and all wait for it, so Acquire fails closed meanwhile.
func (r *Rebuilder) Rebuild(ctx context.Context, keyID int64) error {
	if r == nil || ctx == nil || keyID <= 0 || r.Store == nil || r.Calls == nil || r.Counters == nil {
		return errors.New("quota: rebuilder is not configured")
	}
	r.mu.Lock()
	if r.inflight == nil {
		r.inflight = map[int64]*rebuild{}
	}
	if current, ok := r.inflight[keyID]; ok {
		r.mu.Unlock()
		select {
		case <-current.done:
			return current.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	current := &rebuild{done: make(chan struct{})}
	r.inflight[keyID] = current
	r.mu.Unlock()

	current.err = r.run(ctx, keyID)
	r.mu.Lock()
	delete(r.inflight, keyID)
	r.mu.Unlock()
	close(current.done)
	return current.err
}

// RebuildAll rebuilds every tracked key; it is meant to run at start.
func (r *Rebuilder) RebuildAll(ctx context.Context) error {
	if r == nil || r.Store == nil {
		return errors.New("quota: rebuilder is not configured")
	}
	ids, err := r.Store.KeyIDs(ctx)
	if err != nil {
		return fmt.Errorf("quota: list keys: %w", err)
	}
	var errs []error
	for _, id := range ids {
		if err := r.Rebuild(ctx, id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (r *Rebuilder) run(ctx context.Context, keyID int64) error {
	now := r.now()
	since := now.Add(-Window)
	stored, hasStored, err := r.Store.Latest(ctx, keyID, since)
	if err != nil {
		return fmt.Errorf("quota: load usage for key %d: %w", keyID, err)
	}
	logSince := since
	if hasStored {
		logSince = stored.WindowStart
	}
	calls, err := r.Calls.Usage(ctx, keyID, logSince)
	if err != nil {
		return fmt.Errorf("quota: load call log for key %d: %w", keyID, err)
	}
	merged := merge(keyID, stored, hasStored, calls)
	if _, err := r.Counters.Restore(ctx, merged); err != nil {
		return fmt.Errorf("quota: restore counters for key %d: %w", keyID, err)
	}
	return nil
}

func merge(keyID int64, stored Usage, hasStored bool, calls CallUsage) Usage {
	out := Usage{KeyID: keyID, ByClass: map[string]int64{}}
	var logged int64
	for class, n := range calls.ByClass {
		out.ByClass[class] = n
		logged += n
	}
	switch {
	case hasStored:
		out.WindowStart = stored.WindowStart
		for class, n := range stored.ByClass {
			out.ByClass[class] = max(out.ByClass[class], n)
		}
		out.Used = max(stored.Used, logged)
	case logged > 0 && !calls.First.IsZero():
		// The real window began at or before the first logged call, so using it
		// keeps the window at least as long as the real one.
		out.WindowStart = calls.First
		out.Used = logged
	}
	return out
}

func (r *Rebuilder) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}
