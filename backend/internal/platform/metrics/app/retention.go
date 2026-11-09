package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jooservices/xflickr/internal/platform/metrics/domain"
)

// DefaultPurgeLimit caps how many minute rows one retention run deletes.
const DefaultPurgeLimit = 10000

// RetentionJob rolls up completed hours then deletes expired minute samples.
type RetentionJob struct {
	Store  domain.Store
	Policy domain.RetentionPolicy
	Clock  domain.Clock
	Logger *slog.Logger
	Limit  int
	Every  time.Duration
}

// NewRetentionJob validates dependencies.
func NewRetentionJob(store domain.Store, policy domain.RetentionPolicy, clock domain.Clock, logger *slog.Logger) (*RetentionJob, error) {
	if store == nil {
		return nil, errors.New("metrics: retention store is required")
	}
	if policy == nil {
		return nil, errors.New("metrics: retention policy is required")
	}
	if clock == nil {
		return nil, errors.New("metrics: retention clock is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &RetentionJob{Store: store, Policy: policy, Clock: clock, Logger: logger, Limit: DefaultPurgeLimit, Every: time.Hour}, nil
}

// Run executes retention on Every until ctx ends.
func (j *RetentionJob) Run(ctx context.Context) {
	if ctx == nil {
		return
	}
	interval := j.Every
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := j.Tick(ctx); err != nil && ctx.Err() == nil {
			j.Logger.ErrorContext(ctx, "metrics: retention failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Tick rolls up completed hours then purges a bounded batch of old minutes.
func (j *RetentionJob) Tick(ctx context.Context) error {
	if ctx == nil {
		return errors.New("metrics: retention context is required")
	}
	days, err := j.Policy.RetentionDays(ctx)
	if err != nil {
		return fmt.Errorf("metrics: read retention: %w", err)
	}
	days = domain.ClampRetentionDays(days)
	now := j.Clock.Now().UTC()
	through := domain.HourFloor(now)
	before := domain.HourFloor(now.AddDate(0, 0, -days))
	rolled, err := j.Store.Rollup(ctx, before, through)
	if err != nil {
		return fmt.Errorf("metrics: rollup: %w", err)
	}
	limit := j.Limit
	if limit <= 0 {
		limit = DefaultPurgeLimit
	}
	purged, err := j.Store.Purge(ctx, before, limit)
	if err != nil {
		return fmt.Errorf("metrics: purge: %w", err)
	}
	j.Logger.InfoContext(ctx, "metrics: retention complete", "rolled_up", rolled, "purged", purged, "retention_days", days)
	return nil
}
