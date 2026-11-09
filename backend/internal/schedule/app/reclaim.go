package app

import (
	"context"
	"errors"
	"time"
)

// Reclaim returns a bounded batch of expired work leases to pending or marks
// exhausted work failed. The adapter publishes job.dead transactionally.
type Reclaim struct {
	Store     PumpStore
	RetryMax  int
	Backoff   time.Duration
	BatchSize int
	Now       func() time.Time
}

func (r Reclaim) RunOnce(ctx context.Context) (int, error) {
	if ctx == nil || r.Store == nil {
		return 0, errors.New("schedule: reclaim dependencies are required")
	}
	if r.RetryMax < 0 || r.Backoff < 0 || r.BatchSize < 1 {
		return 0, errors.New("schedule: invalid reclaim settings")
	}
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	return r.Store.Reclaim(ctx, r.RetryMax, r.Backoff, r.BatchSize, now())
}
