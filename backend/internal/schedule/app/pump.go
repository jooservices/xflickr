package app

import (
	"context"
	"errors"
	"time"

	"github.com/jooservices/xflickr/internal/platform/queue"
)

const PumpInterval = 2 * time.Second

// PumpSettings are the dispatch caps read from application settings.
type PumpSettings struct {
	PerKeyReadyCap int
	NodeReadyCap   int
	MediaReadyCap  int
	AIReadyCap     int
	SyncReadyCap   int
	RetryMax       int
	RetryBackoff   time.Duration
	LeaseTimeout   time.Duration
}

func (s PumpSettings) Validate() error {
	if s.PerKeyReadyCap < 0 || s.NodeReadyCap < 0 || s.MediaReadyCap < 0 || s.AIReadyCap < 0 || s.SyncReadyCap < 0 || s.RetryMax < 0 || s.RetryBackoff < 0 || s.LeaseTimeout < 0 {
		return errors.New("schedule: invalid pump settings")
	}
	return nil
}

// PumpStore dispatches a bounded batch atomically and reclaims expired leases.
type PumpStore interface {
	Pump(context.Context, PumpSettings, CreditProvider, queue.JobQueue) (int, error)
	Reclaim(context.Context, int, time.Duration, time.Time) (int, error)
}

// CreditProvider reports the remaining Flickr calls a key may spend in the
// next two minutes. Offline providers return an error and halt pumping.
type CreditProvider interface {
	Capacity(context.Context, int64, time.Duration) (int, error)
}

// Pump coordinates a single bounded scheduling pass.
type Pump struct {
	Store    PumpStore
	Credits  CreditProvider
	Queue    queue.JobQueue
	Settings PumpSettings
}

func (p Pump) RunOnce(ctx context.Context) (int, error) {
	if ctx == nil || p.Store == nil || p.Credits == nil || p.Queue == nil {
		return 0, errors.New("schedule: pump dependencies are required")
	}
	if err := p.Settings.Validate(); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if _, err := p.Store.Reclaim(ctx, p.Settings.RetryMax, p.Settings.RetryBackoff, time.Now()); err != nil {
		return 0, err
	}
	return p.Store.Pump(ctx, p.Settings, p.Credits, p.Queue)
}

// Run polls at the required two-second cadence. A transient error delays only
// the current tick; the next tick retries after service recovery.
func (p Pump) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("schedule: pump context is required")
	}
	ticker := time.NewTicker(PumpInterval)
	defer ticker.Stop()
	for {
		_, _ = p.RunOnce(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
