// Package postgres persists the durable schedule backlog.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/schedule/domain"
)

const enqueueSQL = `
INSERT INTO work_items (
    kind, subject_key, user_id, key_id, state, cursor, priority_base,
    attempts, not_before, last_error
) VALUES (
    $1, $2, $3, $4, COALESCE(NULLIF($5, ''), 'pending'),
    COALESCE($6, '{}'::jsonb), $7, $8, COALESCE($9, now()), $10
)
ON CONFLICT ON CONSTRAINT work_items_dedupe_key DO UPDATE
SET state = CASE WHEN work_items.state = 'done' AND $11 THEN 'pending' ELSE work_items.state END,
    cursor = CASE WHEN work_items.state = 'done' AND $11 THEN '{}'::jsonb ELSE work_items.cursor END,
    attempts = CASE WHEN work_items.state = 'done' AND $11 THEN 0 ELSE work_items.attempts END,
    last_error = CASE WHEN work_items.state = 'done' AND $11 THEN NULL ELSE work_items.last_error END,
    priority_base = GREATEST(work_items.priority_base, EXCLUDED.priority_base),
    updated_at = now()
WHERE work_items.state <> 'done' OR $11
`

const advanceSQL = `
UPDATE work_items
SET cursor = $2, state = 'pending', updated_at = now()
WHERE id = $1 AND state = 'running'
`

// Enqueue inserts or deduplicates an item inside the caller's transaction.
// Active items keep their progress and receive a priority increase when one is
// requested. Done items are reopened only when item.Reopen is true.
func Enqueue(ctx context.Context, tx pgx.Tx, item domain.WorkItem) error {
	if ctx == nil {
		return errors.New("schedule: enqueue context is required")
	}
	if tx == nil {
		return errors.New("schedule: enqueue transaction is required")
	}
	if err := item.Validate(); err != nil {
		return err
	}
	if item.State != "" && item.State != domain.StatePending {
		return errors.New("schedule: new work item must start pending")
	}
	var cursor any
	if len(item.Cursor) > 0 {
		cursor = json.RawMessage(item.Cursor)
	}
	_, err := tx.Exec(ctx, enqueueSQL,
		item.Kind, item.SubjectKey, item.UserID, item.KeyID, string(item.State), cursor,
		item.PriorityBase, item.Attempts, nullTime(item.NotBefore), item.LastError, item.Reopen,
	)
	if err != nil {
		return fmt.Errorf("schedule: enqueue work item: %w", err)
	}
	return nil
}

// Advance stores a next-page cursor after the current page has completed.
// Rows that are not running cannot advance, preventing duplicate page fan-out.
func Advance(ctx context.Context, tx pgx.Tx, id int64, newCursor json.RawMessage) error {
	if ctx == nil {
		return errors.New("schedule: advance context is required")
	}
	if tx == nil {
		return errors.New("schedule: advance transaction is required")
	}
	if id <= 0 {
		return errors.New("schedule: work item id must be positive")
	}
	if len(newCursor) == 0 || !json.Valid(newCursor) {
		return errors.New("schedule: next cursor must be valid JSON")
	}
	tag, err := tx.Exec(ctx, advanceSQL, id, json.RawMessage(newCursor))
	if err != nil {
		return fmt.Errorf("schedule: advance work item: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("schedule: only running work items can advance")
	}
	return nil
}

func nullTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
