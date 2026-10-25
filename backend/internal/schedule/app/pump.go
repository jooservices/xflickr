package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jooservices/xflickr/internal/platform/queue"
	"github.com/jooservices/xflickr/internal/schedule/domain"
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
	AgingBatchSize int
	MaxDepth       int
	ScoreWeights   domain.ScoreWeights
}

func (s PumpSettings) Validate() error {
	if s.PerKeyReadyCap < 0 || s.NodeReadyCap < 0 || s.MediaReadyCap < 0 || s.AIReadyCap < 0 || s.SyncReadyCap < 0 || s.RetryMax < 0 || s.RetryBackoff < 0 || s.LeaseTimeout < 0 || s.AgingBatchSize < 0 || s.MaxDepth < 0 {
		return errors.New("schedule: invalid pump settings")
	}
	if s.AgingBatchSize > 0 {
		if _, err := domain.Score(domain.ScoreInput{MaxDepth: s.MaxDepth}, s.ScoreWeights); err != nil {
			return errors.New("schedule: invalid score settings")
		}
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

// QueueStatePublisher emits aggregate queue state for event consumers.
type QueueStatePublisher interface {
	PublishQueueState(context.Context) error
}

// ChangeCheckScheduler schedules per-user change checks alongside queue work.
type ChangeCheckScheduler interface {
	RunOnce(context.Context) error
}

// PhotoLaneDispatcher creates durable size jobs from provisional catalog photos.
type PhotoLaneDispatcher interface {
	Next(context.Context, int) ([]int64, error)
}

// Pump coordinates a single bounded scheduling pass.
type Pump struct {
	Store              PumpStore
	Credits            CreditProvider
	Queue              queue.JobQueue
	Settings           PumpSettings
	Logger             *slog.Logger
	QueueState         QueueStatePublisher
	ChangeChecks       ChangeCheckScheduler
	PhotoLane          PhotoLaneDispatcher
	MediaPressure      interface {
		Full(context.Context) (bool, error)
	}
	QueueStateInterval time.Duration
}

//nolint:cyclop // One scheduling pass validates prerequisites, recovers durable work, and dispatches both lanes.
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
	if p.ChangeChecks != nil {
		if err := p.ChangeChecks.RunOnce(ctx); err != nil {
			logger := p.Logger
			if logger == nil {
				logger = slog.Default()
			}
			logger.ErrorContext(ctx, "scheduler change check pass failed", "error", err)
		}
	}
	if _, err := p.Store.Reclaim(ctx, p.Settings.RetryMax, p.Settings.RetryBackoff, time.Now()); err != nil {
		return 0, err
	}
	if p.Settings.AgingBatchSize > 0 {
		agingStore, ok := p.Store.(AgingStore)
		if !ok {
			return 0, errors.New("schedule: pump store does not support score aging")
		}
		if _, err := (Aging{Store: agingStore, BatchSize: p.Settings.AgingBatchSize, MaxDepth: p.Settings.MaxDepth, Weights: p.Settings.ScoreWeights}).RunBatch(ctx); err != nil {
			return 0, err
		}
	}
	settings, err := p.settingsUnderPressure(ctx)
	if err != nil {
		return 0, err
	}
	dispatched, err := p.Store.Pump(ctx, settings, p.Credits, p.Queue)
	if err != nil {
		return dispatched, err
	}
	if p.PhotoLane != nil && p.Settings.SyncReadyCap > 0 {
		photos, err := p.PhotoLane.Next(ctx, p.Settings.SyncReadyCap)
		if err != nil {
			return dispatched, err
		}
		dispatched += len(photos)
	}
	return dispatched, nil
}

func (p Pump) settingsUnderPressure(ctx context.Context) (PumpSettings, error) {
	settings := p.Settings
	if p.MediaPressure == nil {
		return settings, nil
	}
	full, err := p.MediaPressure.Full(ctx)
	if err != nil {
		return PumpSettings{}, err
	}
	if full {
		settings.MediaReadyCap = 0
	}
	return settings, nil
}

// Run polls at the required two-second cadence. A transient error delays only
// the current tick; the next tick retries after service recovery.
func (p Pump) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("schedule: pump context is required")
	}
	ticker := time.NewTicker(PumpInterval)
	defer ticker.Stop()
	statePublisher := p.QueueState
	if statePublisher == nil {
		statePublisher, _ = p.Store.(QueueStatePublisher)
	}
	stateInterval := p.QueueStateInterval
	if stateInterval <= 0 {
		stateInterval = QueueStateInterval
	}
	stateTicker := time.NewTicker(stateInterval)
	defer stateTicker.Stop()
	publishQueueState := func() {
		if statePublisher == nil {
			return
		}
		if err := statePublisher.PublishQueueState(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger := p.Logger
			if logger == nil {
				logger = slog.Default()
			}
			logger.ErrorContext(ctx, "scheduler queue state publication failed", "error", err)
		}
	}
	publishQueueState()
	for {
		if _, err := p.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger := p.Logger
			if logger == nil {
				logger = slog.Default()
			}
			logger.ErrorContext(ctx, "scheduler pump tick failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		case <-stateTicker.C:
			publishQueueState()
		}
	}
}
