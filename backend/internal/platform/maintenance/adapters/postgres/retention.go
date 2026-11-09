// Package postgres stores api_call_log retention against PostgreSQL partitions.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/maintenance/app"
	"github.com/jooservices/xflickr/internal/platform/maintenance/domain"
)

// RetentionLockKey is the fixed session advisory lock for call-log retention.
// Only one process in the cluster drops a partition at a time.
const RetentionLockKey int64 = 0x78666c6b7265746e // "xflkretn"

// Querier is the subset of pgx used to read settings.
type Querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// LockConn is one PostgreSQL session held for the advisory lock lifetime.
type LockConn interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Release()
}

// ConnPool acquires a dedicated connection so lock and unlock share a session.
type ConnPool interface {
	Acquire(context.Context) (LockConn, error)
}

type execQuery interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// Store detaches and drops expired daily api_call_log partitions.
type Store struct {
	Pool  execQuery
	Locks ConnPool
	Key   int64
}

var _ app.PartitionStore = (*Store)(nil)

// poolLock adapts *pgxpool.Pool to ConnPool without importing roles wiring.
type poolLock struct{ pool *pgxpool.Pool }

func (p poolLock) Acquire(ctx context.Context) (LockConn, error) {
	if p.pool == nil {
		return nil, errors.New("maintenance: lock pool is required")
	}
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("maintenance: acquire retention lock connection: %w", err)
	}
	return conn, nil
}

// NewStore wires a pgxpool for both queries and the advisory lock.
func NewStore(pool *pgxpool.Pool) Store {
	return Store{Pool: pool, Locks: poolLock{pool: pool}, Key: RetentionLockKey}
}

// TryLock acquires the cluster-wide retention advisory lock.
func (s Store) TryLock(ctx context.Context) (func(), bool, error) {
	if ctx == nil || s.Locks == nil {
		return nil, false, errors.New("maintenance: retention lock requires context and pool")
	}
	key := s.Key
	if key == 0 {
		key = RetentionLockKey
	}
	conn, err := s.Locks.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("maintenance: acquire retention lock connection: %w", err)
	}
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&locked); err != nil {
		conn.Release()
		return nil, false, fmt.Errorf("maintenance: try retention advisory lock: %w", err)
	}
	if !locked {
		conn.Release()
		return nil, false, nil
	}
	unlock := func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, key)
		conn.Release()
	}
	return unlock, true, nil
}

// ListDailyPartitions returns attached api_call_log_YYYYMMDD children.
func (s Store) ListDailyPartitions(ctx context.Context) ([]domain.DailyPartition, error) {
	if s.Pool == nil || ctx == nil {
		return nil, errors.New("maintenance: list partitions requires context and pool")
	}
	rows, err := s.Pool.Query(ctx, `SELECT child.relname
FROM pg_inherits inheritance
JOIN pg_class parent ON parent.oid = inheritance.inhparent
JOIN pg_class child ON child.oid = inheritance.inhrelid
WHERE parent.oid = 'api_call_log'::regclass
  AND child.relname ~ '^api_call_log_[0-9]{8}$'
ORDER BY child.relname`)
	if err != nil {
		return nil, fmt.Errorf("maintenance: list call log partitions: %w", err)
	}
	defer rows.Close()
	var out []domain.DailyPartition
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("maintenance: scan call log partition: %w", err)
		}
		part, err := domain.ParseDailyPartitionName(name)
		if err != nil {
			return nil, err
		}
		out = append(out, part)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("maintenance: iterate call log partitions: %w", err)
	}
	return out, nil
}

// RollupDay refreshes hourly aggregates for one UTC day before it is dropped.
func (s Store) RollupDay(ctx context.Context, day time.Time) error {
	if s.Pool == nil || ctx == nil {
		return errors.New("maintenance: rollup requires context and pool")
	}
	day = time.Date(day.UTC().Year(), day.UTC().Month(), day.UTC().Day(), 0, 0, 0, 0, time.UTC)
	until := day.AddDate(0, 0, 1)
	_, err := s.Pool.Exec(ctx, `INSERT INTO api_call_rollup (day, hour, key_id, method, call_count, errors, p95_ms)
SELECT (at AT TIME ZONE 'UTC')::date,
       EXTRACT(HOUR FROM at AT TIME ZONE 'UTC')::smallint,
       key_id,
       method,
       count(*)::bigint,
       count(*) FILTER (WHERE COALESCE(outcome, status) IN ('failure', 'throttled', 'fail', 'error'))::bigint,
       COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms)::bigint, 0)
FROM api_call_log
WHERE at >= $1 AND at < $2 AND key_id IS NOT NULL AND class IS NULL AND method IS NOT NULL AND status IS NOT NULL AND outcome IS DISTINCT FROM 'quota_wait'
GROUP BY 1, 2, 3, 4
ON CONFLICT (day, hour, key_id, method) DO UPDATE
SET call_count = EXCLUDED.call_count,
    errors = EXCLUDED.errors,
    p95_ms = EXCLUDED.p95_ms`, day, until)
	if err != nil {
		return fmt.Errorf("maintenance: roll up call log day %s: %w", day.Format("2006-01-02"), err)
	}
	return nil
}

// DetachConcurrently detaches one partition without blocking writers. It must
// not run inside a transaction block.
func (s Store) DetachConcurrently(ctx context.Context, name string) error {
	if s.Pool == nil || ctx == nil {
		return errors.New("maintenance: detach requires context and pool")
	}
	part, err := domain.ParseDailyPartitionName(name)
	if err != nil {
		return err
	}
	query := fmt.Sprintf(
		"ALTER TABLE api_call_log DETACH PARTITION %s CONCURRENTLY",
		pgx.Identifier{part.Name}.Sanitize(),
	)
	if _, err := s.Pool.Exec(ctx, query); err != nil {
		return fmt.Errorf("maintenance: detach partition %s concurrently: %w", part.Name, err)
	}
	return nil
}

// DropTable removes a previously detached partition table.
func (s Store) DropTable(ctx context.Context, name string) error {
	if s.Pool == nil || ctx == nil {
		return errors.New("maintenance: drop requires context and pool")
	}
	part, err := domain.ParseDailyPartitionName(name)
	if err != nil {
		return err
	}
	query := "DROP TABLE IF EXISTS " + pgx.Identifier{part.Name}.Sanitize()
	if _, err := s.Pool.Exec(ctx, query); err != nil {
		return fmt.Errorf("maintenance: drop partition table %s: %w", part.Name, err)
	}
	return nil
}
