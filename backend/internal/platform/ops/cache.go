package ops

import (
	"context"
	"sync"
	"time"
)

// DefaultSnapshotTTL bounds how often API requests may trigger the full-table counts of a Snapshot.
const DefaultSnapshotTTL = 30 * time.Second

// CachedStore serves one Snapshot to every caller inside TTL. The snapshot counts work_items and river_job
// by state, which scans both tables; an admin page that polls must not repeat that scan on every request
// (JOOXF-416). Concurrent callers share one load, and a failed load is never cached.
type CachedStore struct {
	Inner Store
	TTL   time.Duration
	Now   func() time.Time

	mu     sync.Mutex
	loaded time.Time
	value  Snapshot
	valid  bool
}

// Snapshot returns the cached snapshot or loads a fresh one.
func (c *CachedStore) Snapshot(ctx context.Context) (Snapshot, error) {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	ttl := c.TTL
	if ttl <= 0 {
		ttl = DefaultSnapshotTTL
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.valid && now().Sub(c.loaded) < ttl {
		return c.value, nil
	}
	snapshot, err := c.Inner.Snapshot(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	c.value, c.loaded, c.valid = snapshot, now(), true
	return snapshot, nil
}
