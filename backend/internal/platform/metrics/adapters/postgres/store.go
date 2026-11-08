// Package postgres persists metric samples and hourly rollups.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jooservices/xflickr/internal/platform/metrics/domain"
)

// Querier is the subset of pgxpool.Pool the store uses.
type Querier interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Store implements domain.Store.
type Store struct {
	Pool Querier
}

var _ domain.Store = Store{}

// WriteBatch upserts samples in chunks of at most MaxWriteBatch.
func (s Store) WriteBatch(ctx context.Context, samples []domain.Sample) error {
	if ctx == nil || s.Pool == nil {
		return errors.New("metrics: postgres pool is required")
	}
	if err := domain.ValidateSamples(samples); err != nil {
		return err
	}
	for _, chunk := range domain.ChunkSamples(samples, domain.MaxWriteBatch) {
		if err := s.writeChunk(ctx, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (s Store) writeChunk(ctx context.Context, samples []domain.Sample) error {
	if len(samples) == 0 {
		return nil
	}
	var builder strings.Builder
	args := make([]any, 0, len(samples)*7)
	builder.WriteString(`INSERT INTO metric_samples (name, labels, labels_hash, node, role, bucket, value) VALUES `)
	for i, sample := range samples {
		if i > 0 {
			builder.WriteByte(',')
		}
		labelsJSON, err := domain.CanonicalLabelsJSON(sample.Labels)
		if err != nil {
			return err
		}
		hash, err := domain.LabelsHash(sample.Labels)
		if err != nil {
			return err
		}
		node := sample.Labels["node"]
		role := sample.Labels["role"]
		bucket := domain.BucketFloor(sample.At)
		base := i * 7
		fmt.Fprintf(&builder, "($%d,$%d::jsonb,$%d,$%d,$%d,$%d,$%d)", base+1, base+2, base+3, base+4, base+5, base+6, base+7)
		args = append(args, sample.Name, string(labelsJSON), hash, node, role, bucket, sample.Value)
	}
	builder.WriteString(` ON CONFLICT (name, labels_hash, bucket) DO UPDATE SET value = EXCLUDED.value, labels = EXCLUDED.labels, node = EXCLUDED.node, role = EXCLUDED.role`)
	if _, err := s.Pool.Exec(ctx, builder.String(), args...); err != nil {
		return fmt.Errorf("metrics: upsert samples: %w", err)
	}
	return nil
}

// Rollup aggregates completed minute buckets into hourly rollups through the
// exclusive upper bound (typically the current hour floor).
func (s Store) Rollup(ctx context.Context, through time.Time) (int64, error) {
	if ctx == nil || s.Pool == nil {
		return 0, errors.New("metrics: postgres pool is required")
	}
	through = domain.HourFloor(through)
	const sql = `
WITH candidates AS (
  SELECT name, labels, labels_hash, node, role,
         date_trunc('hour', bucket) AS hour_bucket,
         avg(value) AS avg,
         min(value) AS min,
         max(value) AS max,
         percentile_cont(0.95) WITHIN GROUP (ORDER BY value) AS p95,
         count(*)::int AS samples
  FROM metric_samples
  WHERE bucket < $1
  GROUP BY name, labels, labels_hash, node, role, date_trunc('hour', bucket)
),
upserted AS (
  INSERT INTO metric_rollups (name, labels, labels_hash, node, role, bucket, resolution, avg, min, max, p95, samples)
  SELECT name, labels, labels_hash, node, role, hour_bucket, '1h', avg, min, max, p95, samples
  FROM candidates
  ON CONFLICT (name, labels_hash, bucket, resolution) DO UPDATE SET
    avg = EXCLUDED.avg,
    min = EXCLUDED.min,
    max = EXCLUDED.max,
    p95 = EXCLUDED.p95,
    samples = EXCLUDED.samples,
    labels = EXCLUDED.labels,
    node = EXCLUDED.node,
    role = EXCLUDED.role
  RETURNING 1
)
SELECT count(*) FROM upserted`
	var count int64
	if err := s.Pool.QueryRow(ctx, sql, through).Scan(&count); err != nil {
		return 0, fmt.Errorf("metrics: rollup: %w", err)
	}
	return count, nil
}

// Purge deletes at most limit minute samples older than before.
func (s Store) Purge(ctx context.Context, before time.Time, limit int) (int64, error) {
	if ctx == nil || s.Pool == nil {
		return 0, errors.New("metrics: postgres pool is required")
	}
	if limit <= 0 {
		return 0, errors.New("metrics: purge limit must be positive")
	}
	tag, err := s.Pool.Exec(ctx, `
DELETE FROM metric_samples
WHERE id IN (
  SELECT id FROM metric_samples
  WHERE bucket < $1
  ORDER BY bucket, id
  LIMIT $2
)`, before.UTC(), limit)
	if err != nil {
		return 0, fmt.Errorf("metrics: purge: %w", err)
	}
	return tag.RowsAffected(), nil
}
