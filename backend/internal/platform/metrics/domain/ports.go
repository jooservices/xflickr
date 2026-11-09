package domain

import (
	"context"
	"time"
)

// Collector gathers one batch of samples for its cadence.
type Collector interface {
	Name() string
	Collect(ctx context.Context) ([]Sample, error)
}

// Store persists minute samples and hourly rollups.
type Store interface {
	WriteBatch(ctx context.Context, samples []Sample) error
	Rollup(ctx context.Context, from, through time.Time) (int64, error)
	Purge(ctx context.Context, before time.Time, limit int) (int64, error)
}

// Clock supplies the current time for collectors and the runner.
type Clock interface {
	Now() time.Time
}

// RetentionPolicy reads the admin-configured retention window.
type RetentionPolicy interface {
	RetentionDays(ctx context.Context) (int, error)
}
