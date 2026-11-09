package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jooservices/xflickr/internal/platform/metrics/domain"
)

// ScheduledCollector pairs a collector with its sampling interval.
type ScheduledCollector struct {
	Collector domain.Collector
	Interval  time.Duration
}

// Runner samples collectors on their cadence and persists the results.
type Runner struct {
	Store      domain.Store
	Collectors []ScheduledCollector
	Clock      domain.Clock
	Logger     *slog.Logger
	Role       string
	Node       string
	Version    string
	Interval   time.Duration

	lastRun map[string]time.Time
}

// NewRunner validates dependencies and returns a ready runner.
func NewRunner(store domain.Store, collectors []ScheduledCollector, clock domain.Clock, role, node, version string, logger *slog.Logger) (*Runner, error) {
	if store == nil {
		return nil, errors.New("metrics: store is required")
	}
	if clock == nil {
		return nil, errors.New("metrics: clock is required")
	}
	if role == "" {
		return nil, errors.New("metrics: role is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{
		Store:      store,
		Collectors: collectors,
		Clock:      clock,
		Logger:     logger,
		Role:       role,
		Node:       node,
		Version:    version,
		lastRun:    map[string]time.Time{},
	}, nil
}

// Run ticks immediately and then every Interval until ctx ends.
func (r *Runner) Run(ctx context.Context) {
	if ctx == nil {
		return
	}
	interval := r.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := r.Tick(ctx); err != nil && ctx.Err() == nil {
			r.Logger.ErrorContext(ctx, "metrics: tick failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Tick runs due collectors, isolates failures, and writes one batch.
func (r *Runner) Tick(ctx context.Context) error {
	if ctx == nil {
		return errors.New("metrics: tick context is required")
	}
	now := r.Clock.Now()
	var samples []domain.Sample
	var errorsCount float64
	for _, scheduled := range r.Collectors {
		if scheduled.Collector == nil {
			continue
		}
		interval := scheduled.Interval
		if interval <= 0 {
			interval = time.Minute
		}
		name := scheduled.Collector.Name()
		if last, ok := r.lastRun[name]; ok {
			elapsed := now.Sub(last)
			tolerance := interval / 10
			if tolerance > time.Second {
				tolerance = time.Second
			}
			if elapsed < interval && interval-elapsed > tolerance {
				continue
			}
		}
		collected, err := r.collectOne(ctx, scheduled.Collector)
		r.lastRun[name] = now
		if err != nil {
			errorsCount++
			r.Logger.ErrorContext(ctx, "metrics: collector failed", "collector", name, "error", err)
			continue
		}
		samples = append(samples, collected...)
	}
	samples = append(samples, domain.Sample{
		Name:   domain.MetricCollectorErrors,
		Labels: r.baseLabels(),
		At:     now,
		Value:  errorsCount,
	})
	if err := domain.ValidateSamples(samples); err != nil {
		return err
	}
	enriched := make([]domain.Sample, 0, len(samples))
	for _, sample := range samples {
		enriched = append(enriched, r.enrich(sample, now))
	}
	if err := r.Store.WriteBatch(ctx, enriched); err != nil {
		return fmt.Errorf("metrics: write batch: %w", err)
	}
	return nil
}

func (r *Runner) collectOne(ctx context.Context, collector domain.Collector) (samples []domain.Sample, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("metrics: collector %s panicked: %v", collector.Name(), recovered)
			samples = nil
		}
	}()
	return collector.Collect(ctx)
}

func (r *Runner) enrich(sample domain.Sample, now time.Time) domain.Sample {
	labels := domain.CloneLabels(sample.Labels)
	if _, ok := labels["role"]; !ok {
		labels["role"] = r.Role
	}
	if _, ok := labels["node"]; !ok && r.Node != "" {
		labels["node"] = r.Node
	}
	at := sample.At
	if at.IsZero() {
		at = now
	}
	return domain.Sample{Name: sample.Name, Labels: labels, At: at, Value: sample.Value}
}

func (r *Runner) baseLabels() map[string]string {
	labels := map[string]string{"role": r.Role}
	if r.Node != "" {
		labels["node"] = r.Node
	}
	return labels
}
