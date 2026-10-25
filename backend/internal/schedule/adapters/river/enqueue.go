// Package river dispatches durable schedule rows into River transactions.
package river

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/outbox"
	"github.com/jooservices/xflickr/internal/platform/queue"
	"github.com/jooservices/xflickr/internal/schedule/app"
)

const pumpLockID int64 = 113

// Store coordinates bounded work selection, River insertion, and lease recovery.
type Store struct {
	Pool     *pgxpool.Pool
	listKeys func(context.Context, pgx.Tx) ([]int64, error)
	inflight func(context.Context, pgx.Tx, int64, string) (int, error)
}

// LoadSettings reads scheduler limits from app_settings and retry defaults
// from the existing retry settings. Values are JSON integer scalars.
func LoadSettings(ctx context.Context, pool *pgxpool.Pool) (app.PumpSettings, error) {
	if pool == nil {
		return app.PumpSettings{}, errors.New("schedule: settings database is required")
	}
	if ctx == nil {
		return app.PumpSettings{}, errors.New("schedule: settings context is required")
	}
	rows, err := pool.Query(ctx, `SELECT key, value #>> '{}' FROM app_settings WHERE key = ANY($1)`, []string{"per_key_ready_cap", "node_ready_cap", "media_ready_cap", "ai_ready_cap", "sync_ready_cap", "retry_max", "retry_backoff", "lease_timeout"})
	if err != nil {
		return app.PumpSettings{}, fmt.Errorf("schedule: query pump settings: %w", err)
	}
	defer rows.Close()
	values := make(map[string]int64, 8)
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			return app.PumpSettings{}, fmt.Errorf("schedule: scan pump setting: %w", err)
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return app.PumpSettings{}, fmt.Errorf("schedule: parse pump setting %s: %w", key, err)
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return app.PumpSettings{}, fmt.Errorf("schedule: read pump settings: %w", err)
	}
	for _, key := range []string{"per_key_ready_cap", "node_ready_cap", "media_ready_cap", "ai_ready_cap", "sync_ready_cap", "retry_max", "retry_backoff", "lease_timeout"} {
		if _, ok := values[key]; !ok {
			return app.PumpSettings{}, fmt.Errorf("schedule: pump setting %s is missing", key)
		}
	}
	settings := app.PumpSettings{PerKeyReadyCap: int(values["per_key_ready_cap"]), NodeReadyCap: int(values["node_ready_cap"]), MediaReadyCap: int(values["media_ready_cap"]), AIReadyCap: int(values["ai_ready_cap"]), SyncReadyCap: int(values["sync_ready_cap"]), RetryMax: int(values["retry_max"]), RetryBackoff: time.Duration(values["retry_backoff"]) * time.Second, LeaseTimeout: time.Duration(values["lease_timeout"]) * time.Second}
	if err := settings.Validate(); err != nil {
		return app.PumpSettings{}, err
	}
	return settings, nil
}

type workRow struct {
	ID      int64           `json:"work_item_id"`
	Kind    string          `json:"kind"`
	Subject string          `json:"subject_key"`
	Cursor  json.RawMessage `json:"cursor"`
}

type queueCap struct {
	name string
	cap  int
}

func (s Store) Pump(ctx context.Context, settings app.PumpSettings, credits app.CreditProvider, jobs queue.JobQueue) (int, error) {
	if s.Pool == nil || credits == nil || jobs == nil {
		return 0, errors.New("schedule: pump adapter dependencies are required")
	}
	if ctx == nil {
		return 0, errors.New("schedule: pump context is required")
	}
	conn, err := s.Pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("schedule: acquire leader connection: %w", err)
	}
	defer conn.Release()
	var leader bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, pumpLockID).Scan(&leader); err != nil {
		return 0, fmt.Errorf("schedule: acquire pump leader lock: %w", err)
	}
	if !leader {
		return 0, nil
	}
	defer func() { _, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, pumpLockID) }()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("schedule: begin pump: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	listKeys := s.listKeys
	if listKeys == nil {
		listKeys = pendingKeys
	}
	keys, err := listKeys(ctx, tx)
	if err != nil {
		return 0, err
	}
	dispatched := 0
	for _, keyID := range keys {
		count := s.inflight
		if count == nil {
			count = countInflight
		}
		keyCount, err := s.pumpKey(ctx, tx, keyID, settings, credits, jobs, count)
		if err != nil {
			return 0, err
		}
		dispatched += keyCount
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("schedule: commit pump: %w", err)
	}
	return dispatched, nil
}

type inflightCounter func(context.Context, pgx.Tx, int64, string) (int, error)

func (s Store) pumpKey(ctx context.Context, tx pgx.Tx, keyID int64, settings app.PumpSettings, credits app.CreditProvider, jobs queue.JobQueue, count inflightCounter) (int, error) {
	remaining, err := credits.Capacity(ctx, keyID, 2*time.Minute)
	if err != nil {
		return 0, fmt.Errorf("schedule: read key %d quota credits: %w", keyID, err)
	}
	keyReady, err := count(ctx, tx, keyID, "")
	if err != nil {
		return 0, err
	}
	keyFree := min(remaining, settings.PerKeyReadyCap-keyReady)
	if keyFree <= 0 {
		return 0, nil
	}
	dispatched := 0
	for _, cap := range configuredQueues(settings) {
		workerInflight, err := count(ctx, tx, 0, cap.name)
		if err != nil {
			return 0, err
		}
		nodeInflight, err := count(ctx, tx, 0, "")
		if err != nil {
			return 0, err
		}
		limit := min(keyFree, max(0, settings.NodeReadyCap-nodeInflight), max(0, cap.cap-workerInflight))
		if limit == 0 {
			continue
		}
		queued, err := s.pumpQueue(ctx, tx, keyID, cap, limit, settings, jobs)
		if err != nil {
			return 0, err
		}
		dispatched += queued
		keyFree -= queued
		if keyFree <= 0 {
			break
		}
	}
	return dispatched, nil
}

func (s Store) pumpQueue(ctx context.Context, tx pgx.Tx, keyID int64, cap queueCap, limit int, settings app.PumpSettings, jobs queue.JobQueue) (int, error) {
	rows, err := tx.Query(ctx, `SELECT id, kind, subject_key, cursor FROM work_items WHERE state = 'pending' AND key_id = $1 AND not_before <= now() AND (CASE WHEN kind LIKE 'ai%' THEN 'ai' WHEN kind LIKE 'media%' THEN 'media' ELSE 'sync' END) = $2 ORDER BY priority_base DESC, not_before, id LIMIT $3 FOR UPDATE SKIP LOCKED`, keyID, cap.name, limit)
	if err != nil {
		return 0, fmt.Errorf("schedule: select pending work: %w", err)
	}
	batch := make([]workRow, 0, limit)
	for rows.Next() {
		var row workRow
		if err := rows.Scan(&row.ID, &row.Kind, &row.Subject, &row.Cursor); err != nil {
			rows.Close()
			return 0, fmt.Errorf("schedule: scan pending work: %w", err)
		}
		batch = append(batch, row)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return 0, fmt.Errorf("schedule: read pending work: %w", rowsErr)
	}
	for _, row := range batch {
		args, _ := json.Marshal(row)
		if err := jobs.Enqueue(ctx, tx, queue.Job{Kind: row.Kind, Args: args}, queue.Options{Queue: cap.name, MaxAttempts: max(1, settings.RetryMax), LeaseTimeout: settings.LeaseTimeout, Backoff: settings.RetryBackoff}); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `UPDATE work_items SET state='ready', lease_expires_at=now()+$2::bigint * interval '1 microsecond', updated_at=now() WHERE id=$1 AND state='pending'`, row.ID, settings.LeaseTimeout.Microseconds()); err != nil {
			return 0, fmt.Errorf("schedule: mark work ready: %w", err)
		}
	}
	return len(batch), nil
}

func configuredQueues(settings app.PumpSettings) []queueCap {
	return []queueCap{{"media", settings.MediaReadyCap}, {"ai", settings.AIReadyCap}, {"sync", settings.SyncReadyCap}}
}

func pendingKeys(ctx context.Context, tx pgx.Tx) ([]int64, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT key_id FROM work_items WHERE state='pending' AND key_id IS NOT NULL AND not_before <= now() ORDER BY key_id`)
	if err != nil {
		return nil, fmt.Errorf("schedule: list pending keys: %w", err)
	}
	defer rows.Close()
	keys := make([]int64, 0)
	for rows.Next() {
		var key int64
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("schedule: scan pending key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("schedule: read pending keys: %w", err)
	}
	return keys, nil
}

func countInflight(ctx context.Context, tx pgx.Tx, keyID int64, worker string) (int, error) {
	var count int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM work_items WHERE state IN ('ready','running') AND ($1=0 OR key_id=$1) AND ($2='' OR (CASE WHEN kind LIKE 'ai%' THEN 'ai' WHEN kind LIKE 'media%' THEN 'media' ELSE 'sync' END)=$2)`, keyID, worker).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("schedule: count inflight work: %w", err)
	}
	return count, nil
}

// Reclaim applies exponential delay based on the already-incremented attempt.
func (s Store) Reclaim(ctx context.Context, retryMax int, base time.Duration, now time.Time) (int, error) {
	if s.Pool == nil {
		return 0, errors.New("schedule: reclaim database is required")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("schedule: begin reclaim: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	rows, err := tx.Query(ctx, `SELECT id, attempts, kind FROM work_items WHERE state='ready' AND lease_expires_at <= $1 ORDER BY lease_expires_at, id FOR UPDATE SKIP LOCKED`, now)
	if err != nil {
		return 0, fmt.Errorf("schedule: select expired leases: %w", err)
	}
	type expired struct {
		id       int64
		attempts int
		kind     string
	}
	expiredRows := make([]expired, 0)
	for rows.Next() {
		var row expired
		if err := rows.Scan(&row.id, &row.attempts, &row.kind); err != nil {
			rows.Close()
			return 0, fmt.Errorf("schedule: scan expired lease: %w", err)
		}
		expiredRows = append(expiredRows, row)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return 0, fmt.Errorf("schedule: read expired leases: %w", rowsErr)
	}
	for _, row := range expiredRows {
		attempts := row.attempts + 1
		if attempts >= retryMax {
			if _, err := tx.Exec(ctx, `UPDATE work_items SET state='failed', attempts=$2, last_error='lease expired', lease_expires_at=NULL, updated_at=$3 WHERE id=$1`, row.id, attempts, now); err != nil {
				return 0, fmt.Errorf("schedule: fail exhausted lease: %w", err)
			}
			if err := outbox.PublishContext(ctx, tx, "job.dead", fmt.Sprint(row.id), map[string]any{"work_item_id": row.id, "kind": row.kind, "attempts": attempts, "error": "lease expired"}); err != nil {
				return 0, err
			}
			continue
		}
		delay := base
		for i := 1; i < attempts && delay < time.Hour; i++ {
			delay *= 2
		}
		if _, err := tx.Exec(ctx, `UPDATE work_items SET state='pending', attempts=$2, not_before=$3, lease_expires_at=NULL, last_error='lease expired', updated_at=$4 WHERE id=$1`, row.id, attempts, now.Add(delay), now); err != nil {
			return 0, fmt.Errorf("schedule: return expired lease to pending: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("schedule: commit reclaim: %w", err)
	}
	return len(expiredRows), nil
}
