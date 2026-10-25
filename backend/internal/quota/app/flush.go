package app

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// DefaultFlushInterval is how often per-key usage is persisted.
const DefaultFlushInterval = time.Minute

// Flusher periodically copies each key's Redis usage into quota_usage.
type Flusher struct {
	Store    UsageStore
	Counters CounterStore
	Interval time.Duration
}

// FlushOnce persists every key's live counters. Keys whose counters were lost
// or are not rebuilt yet are skipped so empty counters never replace real data.
// It attempts every key and joins the failures.
func (f Flusher) FlushOnce(ctx context.Context) error {
	if ctx == nil || f.Store == nil || f.Counters == nil {
		return errors.New("quota: flusher is not configured")
	}
	ids, err := f.Store.KeyIDs(ctx)
	if err != nil {
		return fmt.Errorf("quota: list keys: %w", err)
	}
	var errs []error
	for _, id := range ids {
		usage, ok, err := f.Counters.Snapshot(ctx, id)
		if err != nil {
			errs = append(errs, fmt.Errorf("quota: read counters for key %d: %w", id, err))
			continue
		}
		if !ok || usage.WindowStart.IsZero() {
			continue
		}
		if err := f.Store.Save(ctx, usage); err != nil {
			errs = append(errs, fmt.Errorf("quota: save usage for key %d: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

// Run flushes every interval until ctx ends. A failed flush is passed to
// onError (when set) and the loop continues, then a final flush is attempted.
func (f Flusher) Run(ctx context.Context, onError func(error)) {
	interval := f.Interval
	if interval <= 0 {
		interval = DefaultFlushInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if err := f.FlushOnce(final); err != nil && onError != nil {
				onError(err)
			}
			return
		case <-ticker.C:
			if err := f.FlushOnce(ctx); err != nil && onError != nil {
				onError(err)
			}
		}
	}
}
