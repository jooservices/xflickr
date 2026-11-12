package river

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/platform/outbox"
)

type expiredLease struct {
	id       int64
	attempts int
	kind     string
}

type expiredLeaseCandidate struct {
	lease      expiredLease
	expiresAt  time.Time
	shouldReap bool
}

type expiredLeaseCursor struct {
	ExpiresAt time.Time `json:"expires_at"`
	ID        int64     `json:"id"`
}

// liveRiverStates are the River job states in which a job still waits for or
// holds a worker. River owns such a job (its timeout and rescuer), so the lease
// reaper leaves the work item alone: a lease cannot expire while the job waits
// in the queue, and a live job is never cancelled.
const liveRiverStates = `('available','pending','retryable','scheduled','running')`

// SelfHealAfter is how long a work item that failed only because its lease
// expired (an infrastructure fault) stays failed before Reclaim gives it a
// fresh start with attempts reset.
const SelfHealAfter = 15 * time.Minute

const reclaimSelfHealSQL = `WITH candidates AS MATERIALIZED (
	SELECT id FROM work_items
	WHERE state='failed' AND last_error='lease expired' AND updated_at <= $2
	ORDER BY updated_at, id
	LIMIT $3
	FOR UPDATE SKIP LOCKED
)
UPDATE work_items AS w SET state='pending', attempts=0, not_before=$1, lease_expires_at=NULL, last_error=NULL, updated_at=$1
FROM candidates
WHERE w.id=candidates.id`

const (
	expiredLeaseCursorKey   = "reclaim_expired_lease_cursor"
	reclaimExpiredLeasesSQL = `WITH candidates AS MATERIALIZED (
		SELECT w.id, w.attempts, w.kind, w.lease_expires_at
		FROM work_items w
		WHERE w.state IN ('ready','running') AND w.lease_expires_at <= $1
			AND (w.lease_expires_at,w.id) > (COALESCE($2::timestamptz,'-infinity'::timestamptz),COALESCE($3::bigint,0))
		ORDER BY w.lease_expires_at,w.id
		LIMIT $4
		FOR UPDATE OF w SKIP LOCKED
	)
	SELECT c.id,c.attempts,c.kind,c.lease_expires_at,
		NOT EXISTS (SELECT 1 FROM river_job j WHERE j.state IN ` + liveRiverStates + `
			AND j.args->'args'->>'work_item_id'=c.id::text) AS should_reap
	FROM candidates c
	ORDER BY c.lease_expires_at,c.id`
	reclaimExpiredLeasesBeforeSQL = `WITH candidates AS MATERIALIZED (
		SELECT w.id, w.attempts, w.kind, w.lease_expires_at
		FROM work_items w
		WHERE w.state IN ('ready','running') AND w.lease_expires_at <= $1
			AND (w.lease_expires_at,w.id) <= ($2::timestamptz,$3::bigint)
		ORDER BY w.lease_expires_at,w.id
		LIMIT $4
		FOR UPDATE OF w SKIP LOCKED
	)
	SELECT c.id,c.attempts,c.kind,c.lease_expires_at,
		NOT EXISTS (SELECT 1 FROM river_job j WHERE j.state IN ` + liveRiverStates + `
			AND j.args->'args'->>'work_item_id'=c.id::text) AS should_reap
	FROM candidates c
	ORDER BY c.lease_expires_at,c.id`
)

// Requeue returns failed work items (all when ids is empty) to pending with
// attempts reset. Rows keep their payload; the normal pump and quota gate
// dispatch them. It reports how many rows were requeued.
func (s Store) Requeue(ctx context.Context, ids []int64, now time.Time) (int, error) {
	db := s.backend()
	if db == nil {
		return 0, errors.New("schedule: requeue database is required")
	}
	tag, err := db.Exec(ctx, `UPDATE work_items SET state='pending', attempts=0, not_before=$1, lease_expires_at=NULL, last_error=NULL, updated_at=$1
		WHERE state='failed' AND (coalesce(cardinality($2::bigint[]), 0) = 0 OR id = ANY($2))`, now, ids)
	if err != nil {
		return 0, fmt.Errorf("schedule: requeue failed work: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// Reclaim processes one bounded batch of expired leases and self-heal failures.
// Expired leases without a live River job return to pending with exponential
// delay based on the already-incremented attempt.
func (s Store) Reclaim(ctx context.Context, retryMax int, base time.Duration, batchSize int, now time.Time) (int, error) {
	db := s.backend()
	if db == nil || batchSize < 1 {
		return 0, errors.New("schedule: reclaim database and positive batch size are required")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("schedule: begin reclaim: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	candidates, err := nextExpiredLeaseBatch(ctx, tx, now, batchSize)
	if err != nil {
		return 0, err
	}
	expiredRows := make([]expiredLease, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.shouldReap {
			expiredRows = append(expiredRows, candidate.lease)
		}
	}
	if err := applyExpiredLeases(ctx, tx, expiredRows, retryMax, base, now); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, reclaimSelfHealSQL, now, now.Add(-SelfHealAfter), batchSize); err != nil {
		return 0, fmt.Errorf("schedule: self-heal lease-expired work: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("schedule: commit reclaim: %w", err)
	}
	return len(expiredRows), nil
}

func nextExpiredLeaseBatch(ctx context.Context, tx pgx.Tx, now time.Time, batchSize int) ([]expiredLeaseCandidate, error) {
	cursorValue, err := lockScheduleMetaCursor(ctx, tx, expiredLeaseCursorKey, "")
	if err != nil {
		return nil, err
	}
	cursor, err := parseExpiredLeaseCursor(cursorValue)
	if err != nil {
		return nil, err
	}
	candidates, err := selectExpiredLeaseCandidates(ctx, tx, now, cursor, batchSize, false)
	if err != nil {
		return nil, err
	}
	if len(candidates) < batchSize && cursor.ID > 0 {
		before, err := selectExpiredLeaseCandidates(ctx, tx, now, cursor, batchSize-len(candidates), true)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, before...)
	}
	if len(candidates) == 0 {
		if err := storeScheduleMetaCursor(ctx, tx, expiredLeaseCursorKey, ""); err != nil {
			return nil, err
		}
	} else {
		last := candidates[len(candidates)-1]
		value, err := json.Marshal(expiredLeaseCursor{ExpiresAt: last.expiresAt, ID: last.lease.id})
		if err != nil {
			return nil, fmt.Errorf("schedule: encode expired lease cursor: %w", err)
		}
		if err := storeScheduleMetaCursor(ctx, tx, expiredLeaseCursorKey, string(value)); err != nil {
			return nil, err
		}
	}
	return candidates, nil
}

func parseExpiredLeaseCursor(value string) (expiredLeaseCursor, error) {
	if value == "" {
		return expiredLeaseCursor{}, nil
	}
	var cursor expiredLeaseCursor
	if err := json.Unmarshal([]byte(value), &cursor); err != nil {
		return expiredLeaseCursor{}, fmt.Errorf("schedule: parse expired lease cursor: %w", err)
	}
	if cursor.ID <= 0 || cursor.ExpiresAt.IsZero() {
		return expiredLeaseCursor{}, errors.New("schedule: expired lease cursor is invalid")
	}
	return cursor, nil
}

func selectExpiredLeaseCandidates(ctx context.Context, tx pgx.Tx, now time.Time, cursor expiredLeaseCursor, limit int, before bool) ([]expiredLeaseCandidate, error) {
	query := reclaimExpiredLeasesSQL
	var cursorTime any
	var cursorID any
	if !cursor.ExpiresAt.IsZero() {
		cursorTime = cursor.ExpiresAt
		cursorID = cursor.ID
	}
	if before {
		query = reclaimExpiredLeasesBeforeSQL
	}
	rows, err := tx.Query(ctx, query, now, cursorTime, cursorID, limit)
	if err != nil {
		return nil, fmt.Errorf("schedule: select expired lease candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]expiredLeaseCandidate, 0, limit)
	for rows.Next() {
		var candidate expiredLeaseCandidate
		if err := rows.Scan(&candidate.lease.id, &candidate.lease.attempts, &candidate.lease.kind, &candidate.expiresAt, &candidate.shouldReap); err != nil {
			return nil, fmt.Errorf("schedule: scan expired lease candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("schedule: read expired lease candidates: %w", err)
	}
	return candidates, nil
}

func applyExpiredLeases(ctx context.Context, tx pgx.Tx, expiredRows []expiredLease, retryMax int, base time.Duration, now time.Time) error {
	for _, row := range expiredRows {
		attempts := row.attempts + 1
		if attempts >= retryMax {
			if _, err := tx.Exec(ctx, `UPDATE work_items SET state='failed', attempts=$2, last_error='lease expired', lease_expires_at=NULL, updated_at=$3 WHERE id=$1`, row.id, attempts, now); err != nil {
				return fmt.Errorf("schedule: fail exhausted lease: %w", err)
			}
			if err := outbox.PublishContext(ctx, tx, "job.dead", fmt.Sprint(row.id), map[string]any{"work_item_id": row.id, "kind": row.kind, "attempts": attempts, "error": "lease expired"}); err != nil {
				return err
			}
			continue
		}
		delay := base
		for i := 1; i < attempts && delay < time.Hour; i++ {
			delay *= 2
		}
		if _, err := tx.Exec(ctx, `UPDATE work_items SET state='pending', attempts=$2, not_before=$3, lease_expires_at=NULL, last_error='lease expired', updated_at=$4 WHERE id=$1`, row.id, attempts, now.Add(delay), now); err != nil {
			return fmt.Errorf("schedule: return expired lease to pending: %w", err)
		}
	}
	return nil
}
