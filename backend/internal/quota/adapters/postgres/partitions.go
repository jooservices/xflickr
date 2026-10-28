package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type partitionDB interface {
	Begin(context.Context) (pgx.Tx, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// Partitions manages the daily api_call_log partitions and hourly rollups.
type Partitions struct {
	Pool partitionDB
}

// MaintainCallLogPartitions creates every daily partition in [oldest, through]
// and drops older daily partitions belonging to api_call_log.
func (p Partitions) MaintainCallLogPartitions(ctx context.Context, oldest, through time.Time) error {
	if p.Pool == nil || ctx == nil {
		return fmt.Errorf("quota: partition database and context are required")
	}
	first := utcDay(oldest)
	last := utcDay(through)
	if first.After(last) {
		return fmt.Errorf("quota: oldest partition day is after the horizon")
	}
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("quota: begin partition maintenance: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	for day := first; !day.After(last); day = day.AddDate(0, 0, 1) {
		name := "api_call_log_" + day.Format("20060102")
		lower := day.Format("2006-01-02") + " 00:00:00+00"
		upper := day.AddDate(0, 0, 1).Format("2006-01-02") + " 00:00:00+00"
		query := fmt.Sprintf(
			"CREATE TABLE IF NOT EXISTS %s PARTITION OF api_call_log FOR VALUES FROM ('%s'::timestamptz) TO ('%s'::timestamptz)",
			pgx.Identifier{name}.Sanitize(), lower, upper,
		)
		if _, err := tx.Exec(ctx, query); err != nil {
			return fmt.Errorf("quota: create call log partition %s: %w", name, err)
		}
	}
	rows, err := tx.Query(ctx, `SELECT child.relname
FROM pg_inherits inheritance
JOIN pg_class parent ON parent.oid = inheritance.inhparent
JOIN pg_class child ON child.oid = inheritance.inhrelid
WHERE parent.oid = 'api_call_log'::regclass
  AND child.relname ~ '^api_call_log_[0-9]{8}$'
  AND to_date(substr(child.relname, 14, 8), 'YYYYMMDD') < $1
ORDER BY child.relname`, first)
	if err != nil {
		return fmt.Errorf("quota: list expired call log partitions: %w", err)
	}
	var expired []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return fmt.Errorf("quota: scan expired call log partition: %w", err)
		}
		expired = append(expired, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("quota: read expired call log partitions: %w", err)
	}
	rows.Close()
	for _, name := range expired {
		if _, err := tx.Exec(ctx, "DROP TABLE "+(pgx.Identifier{name}).Sanitize()); err != nil {
			return fmt.Errorf("quota: drop expired call log partition %s: %w", name, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("quota: commit partition maintenance: %w", err)
	}
	return nil
}

// RollupCalls recalculates complete hourly buckets in [from, until), replacing
// existing aggregates so a recent late-arriving batch is included.
func (p Partitions) RollupCalls(ctx context.Context, from, until time.Time) error {
	if p.Pool == nil || ctx == nil || !from.Before(until) {
		return fmt.Errorf("quota: rollup database, context and a non-empty range are required")
	}
	_, err := p.Pool.Exec(ctx, `INSERT INTO api_call_rollup (day, hour, key_id, method, call_count, errors, p95_ms)
SELECT (at AT TIME ZONE 'UTC')::date,
       EXTRACT(HOUR FROM at AT TIME ZONE 'UTC')::smallint,
       key_id,
       method,
       count(*)::bigint,
       count(*) FILTER (WHERE status IN ('fail', 'error'))::bigint,
       COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms)::bigint, 0)
FROM api_call_log
WHERE at >= $1 AND at < $2 AND class IS NULL AND method IS NOT NULL AND status IS NOT NULL
GROUP BY 1, 2, 3, 4
ON CONFLICT (day, hour, key_id, method) DO UPDATE
SET call_count = EXCLUDED.call_count,
    errors = EXCLUDED.errors,
    p95_ms = EXCLUDED.p95_ms`, from.UTC(), until.UTC())
	if err != nil {
		return fmt.Errorf("quota: roll up call log: %w", err)
	}
	return nil
}

// PruneCallRollups removes hourly aggregates older than the retention cutoff.
func (p Partitions) PruneCallRollups(ctx context.Context, before time.Time) error {
	if p.Pool == nil || ctx == nil {
		return fmt.Errorf("quota: rollup database and context are required")
	}
	if _, err := p.Pool.Exec(ctx, `DELETE FROM api_call_rollup WHERE day < $1`, utcDay(before)); err != nil {
		return fmt.Errorf("quota: prune call rollups: %w", err)
	}
	return nil
}

func utcDay(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}
