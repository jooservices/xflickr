// Package river samples River queue, worker heartbeat and outbox backlog metrics.
package river

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/platform/metrics/domain"
)

// Querier is the database surface this collector needs.
type Querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Collector reads River and outbox tables once per Collect call.
type Collector struct {
	Pool  Querier
	Clock domain.Clock
	Role  string
	Node  string
}

var _ domain.Collector = (*Collector)(nil)

// NewCollector validates dependencies.
func NewCollector(pool Querier, clock domain.Clock, role, node string) (*Collector, error) {
	if pool == nil {
		return nil, errors.New("metrics: river pool is required")
	}
	if clock == nil {
		return nil, errors.New("metrics: river clock is required")
	}
	return &Collector{Pool: pool, Clock: clock, Role: role, Node: node}, nil
}

// Name identifies the collector.
func (c *Collector) Name() string { return "river" }

// Collect samples queue depths, heartbeats and outbox lag.
func (c *Collector) Collect(ctx context.Context) ([]domain.Sample, error) {
	if ctx == nil {
		return nil, errors.New("metrics: river collect context is required")
	}
	now := c.Clock.Now()
	samples, err := c.queueSamples(ctx, now)
	if err != nil {
		return nil, err
	}
	heartbeat := domain.Sample{
		Name: domain.MetricQueueWorkerHeartbeat,
		Labels: map[string]string{
			"role":    c.Role,
			"node_id": c.Node,
		},
		At:    now,
		Value: 1,
	}
	samples = append(samples, heartbeat)
	outbox, err := c.outboxSamples(ctx, now)
	if err != nil {
		return nil, err
	}
	return append(samples, outbox...), nil
}

func (c *Collector) queueSamples(ctx context.Context, now time.Time) ([]domain.Sample, error) {
	rows, err := c.Pool.Query(ctx, `
SELECT coalesce(queue, ''),
       count(*) FILTER (WHERE state = 'available'),
       count(*) FILTER (WHERE state = 'running'),
       count(*) FILTER (WHERE state = 'retryable'),
       count(*) FILTER (WHERE state = 'discarded'),
       count(*) FILTER (WHERE state = 'completed' AND finalized_at >= $1),
       extract(epoch FROM ($2 - min(created_at) FILTER (WHERE state = 'available')))
FROM river_job
GROUP BY queue`, now.Add(-time.Minute), now)
	if err != nil {
		return nil, fmt.Errorf("metrics: query river_job: %w", err)
	}
	defer rows.Close()
	var samples []domain.Sample
	for rows.Next() {
		var queue string
		var available, running, retryable, discarded, completed float64
		var oldestAge *float64
		if err := rows.Scan(&queue, &available, &running, &retryable, &discarded, &completed, &oldestAge); err != nil {
			return nil, fmt.Errorf("metrics: scan river_job: %w", err)
		}
		labels := map[string]string{"queue": queue}
		samples = append(samples,
			domain.Sample{Name: domain.MetricQueueJobsAvailable, Labels: labels, At: now, Value: available},
			domain.Sample{Name: domain.MetricQueueJobsRunning, Labels: domain.CloneLabels(labels), At: now, Value: running},
			domain.Sample{Name: domain.MetricQueueJobsRetryable, Labels: domain.CloneLabels(labels), At: now, Value: retryable},
			domain.Sample{Name: domain.MetricQueueJobsDiscarded, Labels: domain.CloneLabels(labels), At: now, Value: discarded},
			domain.Sample{Name: domain.MetricQueueJobsCompleted1m, Labels: domain.CloneLabels(labels), At: now, Value: completed},
		)
		age := 0.0
		if oldestAge != nil && *oldestAge > 0 {
			age = *oldestAge
		}
		samples = append(samples, domain.Sample{Name: domain.MetricQueueOldestAvailableAge, Labels: domain.CloneLabels(labels), At: now, Value: age})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("metrics: iterate river_job: %w", err)
	}
	return samples, nil
}

func (c *Collector) outboxSamples(ctx context.Context, now time.Time) ([]domain.Sample, error) {
	var backlog int64
	var oldest *time.Time
	if err := c.Pool.QueryRow(ctx, `
SELECT count(*), min(created_at)
FROM outbox_events
WHERE processed_at IS NULL`).Scan(&backlog, &oldest); err != nil {
		return nil, fmt.Errorf("metrics: query outbox: %w", err)
	}
	lag := 0.0
	if oldest != nil {
		lag = now.Sub(*oldest).Seconds()
		if lag < 0 {
			lag = 0
		}
	}
	return []domain.Sample{
		{Name: domain.MetricOutboxBacklog, At: now, Value: float64(backlog)},
		{Name: domain.MetricOutboxLagSeconds, At: now, Value: lag},
	}, nil
}
