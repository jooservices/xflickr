// Package postgres reads the crawl health snapshot from PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/platform/ops"
)

// Querier is the subset of pgxpool.Pool the store uses.
type Querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Store implements ops.Store.
type Store struct {
	Pool Querier
}

var _ ops.Store = Store{}

// Snapshot reads every number of the health report.
func (s Store) Snapshot(ctx context.Context) (ops.Snapshot, error) {
	if ctx == nil || s.Pool == nil {
		return ops.Snapshot{}, errors.New("ops: postgres pool is required")
	}
	var snap ops.Snapshot
	var err error
	if snap.Counts, err = s.counts(ctx); err != nil {
		return ops.Snapshot{}, err
	}
	if snap.WorkItems, err = s.grouped(ctx, workItemStates, `SELECT state, count(*) FROM work_items GROUP BY state`); err != nil {
		return ops.Snapshot{}, err
	}
	if snap.RiverJobs, err = s.grouped(ctx, riverStates, `SELECT state::text, count(*) FROM river_job GROUP BY state`); err != nil {
		return ops.Snapshot{}, err
	}
	if snap.PhotoSizesJobs, err = s.photoSizesJobs(ctx); err != nil {
		return ops.Snapshot{}, err
	}
	if snap.Tokens, err = s.grouped(ctx, nil, `SELECT status, count(*) FROM flickr_accounts GROUP BY status`); err != nil {
		return ops.Snapshot{}, err
	}
	if snap.Keys, err = s.keys(ctx); err != nil {
		return ops.Snapshot{}, err
	}
	snap.FailedWorkItems = snap.WorkItems["failed"]
	snap.DeadJobs = snap.RiverJobs["discarded"]
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM api_call_log WHERE called_at > now() - interval '10 minutes'`).Scan(&snap.CallsLast10Min); err != nil {
		return ops.Snapshot{}, fmt.Errorf("ops: count recent calls: %w", err)
	}
	var oldest *time.Time
	if err := s.Pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE key_id IS NULL), count(*) FILTER (WHERE key_id IS NOT NULL AND not_before > now()), min(created_at)
FROM work_items WHERE state = 'pending'`).Scan(&snap.Pending.Total, &snap.Pending.WithoutKey, &snap.Pending.NotBeforeLater, &oldest); err != nil {
		return ops.Snapshot{}, fmt.Errorf("ops: read pending work: %w", err)
	}
	if oldest != nil {
		snap.Pending.Oldest = *oldest
	}
	return snap, nil
}

func (s Store) photoSizesJobs(ctx context.Context) (ops.LaneJobs, error) {
	var jobs ops.LaneJobs
	err := s.Pool.QueryRow(ctx, `SELECT
  (SELECT count(*) FROM photos p WHERE p.tombstoned_at IS NULL
    AND (p.sizes_status <> 'confirmed' OR p.sizes_checked_at < p.last_update)
    AND NOT EXISTS (SELECT 1 FROM river_job j WHERE j.args->>'kind'='photo.sizes'
      AND j.state IN ('available','pending','scheduled','retryable','running')
      AND (j.args->'args'->>'photo_id')::bigint=p.id))
    + count(*) FILTER (WHERE state IN ('pending','scheduled','retryable')),
  count(*) FILTER (WHERE state='available'),
  count(*) FILTER (WHERE state='running')
FROM river_job WHERE args->>'kind'='photo.sizes' AND state IN ('available','pending','scheduled','retryable','running')`).Scan(&jobs.Pending, &jobs.Ready, &jobs.Running)
	if err != nil {
		return ops.LaneJobs{}, fmt.Errorf("ops: count photo size lane: %w", err)
	}
	return jobs, nil
}

// estimateSQL reads a table size. Partitioned catalog tables hold millions of
// rows, so the planner estimate (summed over partitions) is used; a table the
// planner has never analysed reports 0 and is counted exactly instead.
const estimateSQL = `SELECT coalesce(sum(greatest(c.reltuples, 0)), 0)::bigint
FROM pg_class c WHERE c.oid = $1::regclass
   OR c.oid IN (SELECT inhrelid FROM pg_inherits WHERE inhparent = $1::regclass)`

func (s Store) counts(ctx context.Context) (ops.Counts, error) {
	var counts ops.Counts
	targets := []struct {
		table string
		out   *int64
	}{
		{"people", &counts.People},
		{"photos", &counts.Photos},
		{"favorites", &counts.Favorites},
		{"albums", &counts.Albums},
		{"galleries", &counts.Galleries},
		{"groups", &counts.Groups},
	}
	for _, target := range targets {
		var estimate int64
		if err := s.Pool.QueryRow(ctx, estimateSQL, target.table).Scan(&estimate); err != nil {
			return ops.Counts{}, fmt.Errorf("ops: estimate %s: %w", target.table, err)
		}
		*target.out = estimate
		if estimate > 0 {
			continue
		}
		// target.table comes from the fixed list above, never from input.
		if err := s.Pool.QueryRow(ctx, "SELECT count(*) FROM "+target.table).Scan(target.out); err != nil {
			return ops.Counts{}, fmt.Errorf("ops: count %s: %w", target.table, err)
		}
	}
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM photos WHERE sizes_status = 'confirmed'`).Scan(&counts.PhotoSizes); err != nil {
		return ops.Counts{}, fmt.Errorf("ops: count photo sizes: %w", err)
	}
	return counts, nil
}

// Known states always appear in the report, with 0 when no row has them.
var (
	workItemStates = []string{"pending", "ready", "running", "waiting", "done", "failed"}
	riverStates    = []string{"available", "cancelled", "completed", "discarded", "pending", "retryable", "running", "scheduled"}
)

func (s Store) grouped(ctx context.Context, known []string, query string) (map[string]int64, error) {
	rows, err := s.Pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("ops: query grouped counts: %w", err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for _, name := range known {
		out[name] = 0
	}
	for rows.Next() {
		var name string
		var count int64
		if err := rows.Scan(&name, &count); err != nil {
			return nil, fmt.Errorf("ops: scan grouped counts: %w", err)
		}
		out[name] = count
	}
	return out, rows.Err()
}

func (s Store) keys(ctx context.Context) ([]ops.KeyStatus, error) {
	rows, err := s.Pool.Query(ctx, `SELECT k.id, k.label, k.status, k.per_hour_limit,
  (SELECT count(*) FROM api_call_log l WHERE l.key_id = k.id AND l.called_at > now() - interval '1 hour')
FROM api_keys k ORDER BY k.id`)
	if err != nil {
		return nil, fmt.Errorf("ops: query keys: %w", err)
	}
	defer rows.Close()
	var keys []ops.KeyStatus
	for rows.Next() {
		var key ops.KeyStatus
		if err := rows.Scan(&key.KeyID, &key.Label, &key.Status, &key.Limit, &key.CallsLastHour); err != nil {
			return nil, fmt.Errorf("ops: scan key: %w", err)
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}
