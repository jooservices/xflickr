// Package app coordinates api_call_log partition retention use cases.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jooservices/xflickr/internal/platform/maintenance/domain"
)

// ErrRetentionBusy means another process already holds the retention lock.
var ErrRetentionBusy = errors.New("maintenance: retention advisory lock held")

// PartitionStore lists and removes expired daily call-log partitions.
type PartitionStore interface {
	TryLock(ctx context.Context) (unlock func(), acquired bool, err error)
	ListDailyPartitions(ctx context.Context) ([]domain.DailyPartition, error)
	RollupDay(ctx context.Context, day time.Time) error
	DetachConcurrently(ctx context.Context, name string) error
	DropTable(ctx context.Context, name string) error
}

// Settings reads the admin-tunable retention controls.
type Settings interface {
	RetentionDays(ctx context.Context) (int, error)
	DryRun(ctx context.Context) (bool, error)
}

// Retention drops at most one expired daily api_call_log partition per run.
type Retention struct {
	Store    PartitionStore
	Settings Settings
	Now      func() time.Time
	Logger   *slog.Logger
	Interval time.Duration
}

const defaultInterval = time.Hour

// RunOnce acquires the cluster lock, selects the oldest expired partition,
// optionally rolls it up, then DETACH CONCURRENTLY + DROP. Dry-run only logs.
func (r Retention) RunOnce(ctx context.Context) error {
	if ctx == nil || r.Store == nil || r.Settings == nil {
		return errors.New("maintenance: retention requires context, store and settings")
	}
	unlock, acquired, err := r.Store.TryLock(ctx)
	if err != nil {
		return fmt.Errorf("maintenance: retention lock: %w", err)
	}
	if !acquired {
		return ErrRetentionBusy
	}
	defer unlock()

	days, err := r.Settings.RetentionDays(ctx)
	if err != nil {
		return fmt.Errorf("maintenance: read retention days: %w", err)
	}
	dryRun, err := r.Settings.DryRun(ctx)
	if err != nil {
		return fmt.Errorf("maintenance: read dry-run: %w", err)
	}
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	cutoff := domain.RetentionCutoff(now(), days)
	parts, err := r.Store.ListDailyPartitions(ctx)
	if err != nil {
		return fmt.Errorf("maintenance: list partitions: %w", err)
	}
	expired := domain.ChooseExpired(parts, cutoff)
	if len(expired) == 0 {
		r.logger().InfoContext(ctx, "api_call_log retention: nothing to drop",
			"cutoff", cutoff.Format(time.RFC3339),
			"retention_days", domain.ClampRetentionDays(days),
			"dry_run", dryRun,
		)
		return nil
	}
	target := expired[0]
	r.logger().InfoContext(ctx, "api_call_log retention: selected partition",
		"partition", target.Name,
		"day", target.Day.Format("2006-01-02"),
		"cutoff", cutoff.Format(time.RFC3339),
		"expired_count", len(expired),
		"dry_run", dryRun,
	)
	if dryRun {
		return nil
	}
	if err := r.Store.RollupDay(ctx, target.Day); err != nil {
		return fmt.Errorf("maintenance: roll up %s before drop: %w", target.Name, err)
	}
	if err := r.Store.DetachConcurrently(ctx, target.Name); err != nil {
		return fmt.Errorf("maintenance: detach %s: %w", target.Name, err)
	}
	if err := r.Store.DropTable(ctx, target.Name); err != nil {
		return fmt.Errorf("maintenance: drop %s: %w", target.Name, err)
	}
	r.logger().InfoContext(ctx, "api_call_log retention: dropped partition",
		"partition", target.Name,
		"day", target.Day.Format("2006-01-02"),
	)
	return nil
}

// Run performs retention at startup and then on Interval. A failure never stops
// the next tick; ErrRetentionBusy is logged at debug and ignored.
func (r Retention) Run(ctx context.Context, onError func(error)) {
	if ctx == nil || r.Store == nil || r.Settings == nil {
		return
	}
	r.tick(ctx, onError)
	interval := r.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(ctx, onError)
		}
	}
}

func (r Retention) tick(ctx context.Context, onError func(error)) {
	err := r.RunOnce(ctx)
	if err == nil {
		return
	}
	if errors.Is(err, ErrRetentionBusy) {
		r.logger().DebugContext(ctx, "api_call_log retention: skipped; advisory lock held")
		return
	}
	if onError != nil {
		onError(err)
	}
}

func (r Retention) logger() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}
