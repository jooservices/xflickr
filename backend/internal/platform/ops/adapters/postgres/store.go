// Package postgres reads the crawl health snapshot from PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jooservices/xflickr/internal/platform/ops"
)

// DefaultStatementTimeout bounds each snapshot statement when unset.
const DefaultStatementTimeout = 5 * time.Second

// Querier is the subset of pgxpool.Pool / pgx.Tx the store uses.
type Querier interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// TxBeginner starts a transaction for SET LOCAL statement_timeout.
type TxBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

// Store implements ops.Store.
type Store struct {
	Pool             Querier
	DB               TxBeginner
	StatementTimeout time.Duration
}

var _ ops.Store = Store{}

// Snapshot reads every number of the health report.
func (s Store) Snapshot(ctx context.Context) (ops.Snapshot, error) {
	if ctx == nil || s.Pool == nil {
		return ops.Snapshot{}, errors.New("ops: postgres pool is required")
	}
	timeout := s.StatementTimeout
	if timeout <= 0 {
		timeout = DefaultStatementTimeout
	}
	if s.DB != nil {
		tx, err := s.DB.Begin(ctx)
		if err != nil {
			return ops.Snapshot{}, fmt.Errorf("ops: begin health snapshot: %w", err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", timeout.Milliseconds())); err != nil {
			return ops.Snapshot{}, fmt.Errorf("ops: set health statement timeout: %w", err)
		}
		snap, err := s.snapshotWith(ctx, tx)
		if err != nil {
			return ops.Snapshot{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return ops.Snapshot{}, fmt.Errorf("ops: commit health snapshot: %w", err)
		}
		return snap, nil
	}
	return s.snapshotWith(ctx, s.Pool)
}

func (s Store) snapshotWith(ctx context.Context, q Querier) (ops.Snapshot, error) {
	var snap ops.Snapshot
	var err error
	if snap.Counts, snap.CountsEstimated, err = s.counts(ctx, q, &snap); err != nil {
		return ops.Snapshot{}, err
	}
	if snap.WorkItems, err = s.grouped(ctx, q, &snap, "work_items", workItemStates, `SELECT state, count(*) FROM work_items GROUP BY state`); err != nil {
		return ops.Snapshot{}, err
	}
	if snap.RiverJobs, err = s.grouped(ctx, q, &snap, "river_jobs", riverStates, `SELECT state::text, count(*) FROM river_job GROUP BY state`); err != nil {
		return ops.Snapshot{}, err
	}
	if snap.PhotoSizesJobs, err = s.photoSizesJobs(ctx, q, &snap); err != nil {
		return ops.Snapshot{}, err
	}
	if snap.Tokens, err = s.grouped(ctx, q, &snap, "tokens", nil, `SELECT status, count(*) FROM flickr_accounts GROUP BY status`); err != nil {
		return ops.Snapshot{}, err
	}
	if snap.Keys, err = s.keys(ctx, q, &snap); err != nil {
		return ops.Snapshot{}, err
	}
	snap.FailedWorkItems = snap.WorkItems["failed"]
	snap.DeadJobs = snap.RiverJobs["discarded"]
	if err := s.queryRow(ctx, q, &snap, "credential_decryption_failures", `SELECT count(*) FROM work_items WHERE (state = 'pending' AND last_error = 'credential_decryption_unavailable') OR (state = 'failed' AND last_error LIKE '%crypto: authentication failed%')`).Scan(&snap.CredentialDecryptionFailures); err != nil {
		return ops.Snapshot{}, err
	}
	if err := s.queryRow(ctx, q, &snap, "recent_calls", `SELECT
  count(*) FILTER (WHERE outcome IS NULL AND called_at > now() - interval '10 minutes'),
  count(*) FILTER (WHERE outcome = 'quota_wait'),
  count(*) FILTER (WHERE outcome = 'quota_wait' AND called_at > now() - interval '10 minutes')
FROM api_call_log WHERE called_at > now() - interval '1 hour'`).Scan(&snap.CallsLast10Min, &snap.QuotaWaitsLastHour, &snap.QuotaWaitsLast10Min); err != nil {
		return ops.Snapshot{}, err
	}
	var oldest *time.Time
	if err := s.queryRow(ctx, q, &snap, "pending_work", `SELECT count(*), count(*) FILTER (WHERE key_id IS NULL), count(*) FILTER (WHERE key_id IS NOT NULL AND not_before > now()), min(created_at)
FROM work_items WHERE state = 'pending'`).Scan(&snap.Pending.Total, &snap.Pending.WithoutKey, &snap.Pending.NotBeforeLater, &oldest); err != nil {
		return ops.Snapshot{}, err
	}
	if oldest != nil {
		snap.Pending.Oldest = *oldest
	}
	return snap, nil
}

// photosNeedingSizesSQL matches photos_sizes_pending_idx so PostgreSQL can use
// an index-only scan. photo_sizes_pending is this count plus waiting photo.sizes
// river jobs (pending/scheduled/retryable); jobs already queued may be counted
// twice and the field is an operational backlog signal, not a unique photo total.
const photosNeedingSizesSQL = `SELECT count(*) FROM photos
WHERE tombstoned_at IS NULL
  AND (sizes_status NOT IN ('confirmed', 'unavailable')
       OR (sizes_status = 'confirmed' AND sizes_checked_at < last_update))`

func (s Store) photoSizesJobs(ctx context.Context, q Querier, snap *ops.Snapshot) (ops.LaneJobs, error) {
	var needing int64
	if err := s.queryRow(ctx, q, snap, "photo_sizes_pending_photos", photosNeedingSizesSQL).Scan(&needing); err != nil {
		return ops.LaneJobs{}, err
	}
	var waiting, ready, running int64
	if err := s.queryRow(ctx, q, snap, "photo_sizes_river", `SELECT
  count(*) FILTER (WHERE state IN ('pending','scheduled','retryable')),
  count(*) FILTER (WHERE state='available'),
  count(*) FILTER (WHERE state='running')
FROM river_job
WHERE args->>'kind'='photo.sizes'
  AND state IN ('available','pending','scheduled','retryable','running')`).Scan(&waiting, &ready, &running); err != nil {
		return ops.LaneJobs{}, err
	}
	return ops.LaneJobs{Pending: needing + waiting, Ready: ready, Running: running}, nil
}

// estimateSQL reads a table size. Partitioned catalog tables hold millions of
// rows, so the planner estimate (summed over partitions) is used.
const estimateSQL = `SELECT coalesce(sum(greatest(c.reltuples, 0)), 0)::bigint
FROM pg_class c WHERE c.oid = $1::regclass
   OR c.oid IN (SELECT inhrelid FROM pg_inherits WHERE inhparent = $1::regclass)`

// photoSizesEstimateSQL estimates confirmed sizes from pg_stats frequencies on
// each photos partition (or the parent) times reltuples. Never scans photos.
const photoSizesEstimateSQL = `SELECT coalesce(round(sum(
  greatest(c.reltuples, 0) * coalesce((
    SELECT s.most_common_freqs[ord.idx]
    FROM pg_stats s
    CROSS JOIN LATERAL unnest(s.most_common_vals::text::text[]) WITH ORDINALITY AS ord(val, idx)
    WHERE s.schemaname = current_schema()
      AND s.tablename = c.relname
      AND s.attname = 'sizes_status'
      AND ord.val = 'confirmed'
    LIMIT 1
  ), 0)
)), 0)::bigint
FROM pg_class c
WHERE c.relkind IN ('r', 'p')
  AND (c.oid = 'photos'::regclass
       OR c.oid IN (SELECT inhrelid FROM pg_inherits WHERE inhparent = 'photos'::regclass))`

func (s Store) counts(ctx context.Context, q Querier, snap *ops.Snapshot) (ops.Counts, bool, error) {
	var counts ops.Counts
	estimated := false
	targets := []struct {
		table   string
		out     *int64
		exactOK bool
	}{
		{"people", &counts.People, true},
		{"photos", &counts.Photos, false},
		{"favorites", &counts.Favorites, false},
		{"albums", &counts.Albums, true},
		{"galleries", &counts.Galleries, true},
		{"groups", &counts.Groups, true},
	}
	for _, target := range targets {
		var estimate int64
		if err := s.queryRow(ctx, q, snap, "estimate_"+target.table, estimateSQL, target.table).Scan(&estimate); err != nil {
			return ops.Counts{}, false, err
		}
		*target.out = estimate
		if estimate > 0 {
			estimated = true
			continue
		}
		if !target.exactOK {
			// Large tables never take an unbounded count(*) in the monitor loop.
			continue
		}
		if err := s.queryRow(ctx, q, snap, "count_"+target.table, "SELECT count(*) FROM "+target.table).Scan(target.out); err != nil {
			return ops.Counts{}, false, err
		}
	}
	if err := s.queryRow(ctx, q, snap, "photo_sizes_estimate", photoSizesEstimateSQL).Scan(&counts.PhotoSizes); err != nil {
		return ops.Counts{}, false, err
	}
	estimated = true
	return counts, estimated, nil
}

// Known states always appear in the report, with 0 when no row has them.
var (
	workItemStates = []string{"pending", "ready", "running", "waiting", "done", "failed"}
	riverStates    = []string{"available", "cancelled", "completed", "discarded", "pending", "retryable", "running", "scheduled"}
)

func (s Store) grouped(ctx context.Context, q Querier, snap *ops.Snapshot, name string, known []string, query string) (map[string]int64, error) {
	rows, err := q.Query(ctx, query)
	if err != nil {
		if markTimeout(snap, name, err) {
			out := map[string]int64{}
			for _, state := range known {
				out[state] = 0
			}
			return out, nil
		}
		return nil, fmt.Errorf("ops: query grouped counts: %w", err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for _, state := range known {
		out[state] = 0
	}
	for rows.Next() {
		var state string
		var count int64
		if err := rows.Scan(&state, &count); err != nil {
			return nil, fmt.Errorf("ops: scan grouped counts: %w", err)
		}
		out[state] = count
	}
	if err := rows.Err(); err != nil {
		if markTimeout(snap, name, err) {
			return out, nil
		}
		return nil, fmt.Errorf("ops: iterate grouped counts: %w", err)
	}
	return out, nil
}

func (s Store) keys(ctx context.Context, q Querier, snap *ops.Snapshot) ([]ops.KeyStatus, error) {
	rows, err := q.Query(ctx, `SELECT k.id, k.label, k.status, k.per_hour_limit,
  (SELECT count(*) FROM api_call_log l WHERE l.key_id = k.id AND l.outcome IS NULL AND l.called_at > now() - interval '1 hour')
FROM api_keys k ORDER BY k.id`)
	if err != nil {
		if markTimeout(snap, "keys", err) {
			return nil, nil
		}
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
	if err := rows.Err(); err != nil {
		if markTimeout(snap, "keys", err) {
			return keys, nil
		}
		return nil, fmt.Errorf("ops: iterate keys: %w", err)
	}
	return keys, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (s Store) queryRow(ctx context.Context, q Querier, snap *ops.Snapshot, name, query string, args ...any) rowScanner {
	row := q.QueryRow(ctx, query, args...)
	return timeoutAwareRow{row: row, snap: snap, name: name}
}

type timeoutAwareRow struct {
	row  pgx.Row
	snap *ops.Snapshot
	name string
}

func (r timeoutAwareRow) Scan(dest ...any) error {
	err := r.row.Scan(dest...)
	if err == nil {
		return nil
	}
	if markTimeout(r.snap, r.name, err) {
		zeroScan(dest)
		return nil
	}
	return fmt.Errorf("ops: %s: %w", r.name, err)
}

func zeroScan(dest []any) {
	for _, d := range dest {
		switch ptr := d.(type) {
		case *int64:
			*ptr = 0
		case **time.Time:
			*ptr = nil
		}
	}
}

func markTimeout(snap *ops.Snapshot, name string, err error) bool {
	if !isStatementTimeout(err) {
		return false
	}
	if snap != nil {
		snap.Degraded = true
		snap.DegradedQueries = append(snap.DegradedQueries, name)
	}
	return true
}

func isStatementTimeout(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "57014" {
		return true
	}
	return false
}
