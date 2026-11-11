package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"

	metricsapp "github.com/jooservices/xflickr/internal/platform/metrics/app"
	"github.com/jooservices/xflickr/internal/platform/metrics/domain"
)

// SeriesQuerier is the subset of pgxpool.Pool used to read metric samples.
type SeriesQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// SeriesStore reads bounded minute series from metric_samples.
type SeriesStore struct {
	Pool SeriesQuerier
}

var _ metricsapp.SeriesReader = SeriesStore{}

// ReadSeries returns the stored queue samples in [From, To).
func (s SeriesStore) ReadSeries(ctx context.Context, query metricsapp.SeriesQuery) ([]metricsapp.Point, error) {
	if ctx == nil || s.Pool == nil {
		return nil, errors.New("metrics: postgres pool is required")
	}
	hash, err := domain.LabelsHash(map[string]string{
		"queue": query.Queue,
		"role":  query.Role,
		"node":  query.Node,
	})
	if err != nil {
		return nil, fmt.Errorf("metrics: hash series labels: %w", err)
	}
	rows, err := s.Pool.Query(ctx, `SELECT bucket, value
FROM metric_samples
WHERE name = $1 AND labels_hash = $2 AND bucket >= $3 AND bucket < $4
ORDER BY bucket ASC
LIMIT 60`, domain.MetricQueueJobsAvailable, hash, query.From.UTC(), query.To.UTC())
	if err != nil {
		return nil, fmt.Errorf("metrics: read series: %w", err)
	}
	if rows == nil {
		return nil, errors.New("metrics: read series returned no rows")
	}
	defer rows.Close()

	points := make([]metricsapp.Point, 0, 60)
	for rows.Next() {
		var point metricsapp.Point
		if err := rows.Scan(&point.At, &point.Value); err != nil {
			return nil, fmt.Errorf("metrics: scan series sample: %w", err)
		}
		if math.IsNaN(point.Value) || math.IsInf(point.Value, 0) {
			return nil, errors.New("metrics: series sample value is not finite")
		}
		point.At = point.At.UTC()
		points = append(points, point)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("metrics: iterate series samples: %w", err)
	}
	return points, nil
}
