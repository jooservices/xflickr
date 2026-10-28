package app

import (
	"context"
	"fmt"
	"time"
)

const (
	DefaultCallLogRetentionDays = 30
	CallLogPartitionHorizonDays = 7
	CallLogRollupRetentionDays  = 400
	CallLogMaintenanceInterval  = time.Hour
	callLogRollupLookback       = 48 * time.Hour
)

// PartitionMaintenanceStore manages partitions and aggregates completed
// hourly buckets. Times passed to it are UTC and aligned to UTC day/hour edges.
type PartitionMaintenanceStore interface {
	MaintainCallLogPartitions(context.Context, time.Time, time.Time) error
	RollupCalls(context.Context, time.Time, time.Time) error
	PruneCallRollups(context.Context, time.Time) error
}

// Partitions maintains the call log and its rollups. The manager is safe to
// run in one scheduler role; each operation is idempotent and can be retried.
type Partitions struct {
	Store         PartitionMaintenanceStore
	RetentionDays int
	Interval      time.Duration
	Now           func() time.Time
}

// MaintainOnce creates the current and future partitions, drops expired call
// log partitions, refreshes the recent completed hourly rollups, and removes
// rollups past their retention period.
func (p Partitions) MaintainOnce(ctx context.Context) error {
	if ctx == nil || p.Store == nil {
		return fmt.Errorf("quota: partition maintenance is not configured")
	}
	retention := p.RetentionDays
	if retention <= 0 {
		retention = DefaultCallLogRetentionDays
	}
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	current := now().UTC()
	today := time.Date(current.Year(), current.Month(), current.Day(), 0, 0, 0, 0, time.UTC)
	oldest := today.AddDate(0, 0, -retention)
	through := today.AddDate(0, 0, CallLogPartitionHorizonDays)
	if err := p.Store.MaintainCallLogPartitions(ctx, oldest, through); err != nil {
		return fmt.Errorf("quota: maintain call log partitions: %w", err)
	}
	rollupEnd := current.Truncate(time.Hour)
	rollupStart := rollupEnd.Add(-callLogRollupLookback)
	if err := p.Store.RollupCalls(ctx, rollupStart, rollupEnd); err != nil {
		return fmt.Errorf("quota: roll up calls: %w", err)
	}
	if err := p.Store.PruneCallRollups(ctx, today.AddDate(0, 0, -CallLogRollupRetentionDays)); err != nil {
		return fmt.Errorf("quota: prune call rollups: %w", err)
	}
	return nil
}

// Run performs maintenance at startup and then at the configured interval.
// Errors are reported and retried on the next tick.
func (p Partitions) Run(ctx context.Context, onError func(error)) {
	if ctx == nil || p.Store == nil {
		return
	}
	if err := p.MaintainOnce(ctx); err != nil && onError != nil {
		onError(err)
	}
	interval := p.Interval
	if interval <= 0 {
		interval = CallLogMaintenanceInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.MaintainOnce(ctx); err != nil && onError != nil {
				onError(err)
			}
		}
	}
}
