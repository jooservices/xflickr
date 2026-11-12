package river

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	keylessOwnerCursorKey     = "keyless_owner_cursor"
	keylessOwnerCandidatesSQL = `SELECT id FROM work_items
		WHERE state='pending' AND key_id IS NULL AND id > $1
		ORDER BY id LIMIT $2 FOR UPDATE SKIP LOCKED`
	keylessOwnerCandidatesBeforeSQL = `SELECT id FROM work_items
		WHERE state='pending' AND key_id IS NULL AND id <= $1
		ORDER BY id LIMIT $2 FOR UPDATE SKIP LOCKED`
)

// assignOwnerKeys gives a bounded, ordered batch of pending keyless rows the
// key of the owning user's active Flickr account. The cursor lets ownerless
// rows remain pending without starving later rows that have an account.
func assignOwnerKeys(ctx context.Context, tx pgx.Tx, batchSize int) error {
	if batchSize < 1 {
		return errors.New("schedule: key assignment batch size must be positive")
	}
	value, err := lockScheduleMetaCursor(ctx, tx, keylessOwnerCursorKey, "0")
	if err != nil {
		return err
	}
	cursor, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fmt.Errorf("schedule: parse keyless owner cursor %q: %w", value, err)
	}
	if cursor < 0 {
		return errors.New("schedule: keyless owner cursor cannot be negative")
	}
	ids, err := selectKeylessOwnerCandidates(ctx, tx, cursor, batchSize, false)
	if err != nil {
		return err
	}
	if len(ids) < batchSize && cursor > 0 {
		before, err := selectKeylessOwnerCandidates(ctx, tx, cursor, batchSize-len(ids), true)
		if err != nil {
			return err
		}
		ids = append(ids, before...)
	}
	if len(ids) == 0 {
		return storeScheduleMetaCursor(ctx, tx, keylessOwnerCursorKey, "0")
	}
	if err := storeScheduleMetaCursor(ctx, tx, keylessOwnerCursorKey, strconv.FormatInt(ids[len(ids)-1], 10)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `WITH candidates AS MATERIALIZED (
		SELECT id, user_id FROM work_items WHERE id=ANY($1::bigint[])
	), owners AS MATERIALIZED (
		SELECT DISTINCT ON (a.user_id) a.user_id, a.key_id
		FROM flickr_accounts a
		JOIN (SELECT DISTINCT user_id FROM candidates) c ON c.user_id=a.user_id
		WHERE a.status='ok'
		ORDER BY a.user_id, a.id
	)
	UPDATE work_items w SET key_id=owners.key_id, updated_at=now()
	FROM candidates JOIN owners USING (user_id)
	WHERE w.id=candidates.id AND w.state='pending' AND w.key_id IS NULL`, ids); err != nil {
		return fmt.Errorf("schedule: assign owner keys: %w", err)
	}
	return nil
}

func selectKeylessOwnerCandidates(ctx context.Context, tx pgx.Tx, cursor int64, limit int, before bool) ([]int64, error) {
	query := keylessOwnerCandidatesSQL
	if before {
		query = keylessOwnerCandidatesBeforeSQL
	}
	rows, err := tx.Query(ctx, query, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("schedule: select keyless work: %w", err)
	}
	defer rows.Close()
	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("schedule: scan keyless work: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("schedule: read keyless work: %w", err)
	}
	return ids, nil
}

const keylessWarnInterval = time.Minute

var (
	keylessWarnMu   sync.Mutex
	keylessWarnLast time.Time
)

// warnKeylessPending logs, at most once per minute, due pending rows that no
// key can dispatch.
func warnKeylessPending(ctx context.Context, tx pgx.Tx) error {
	var found bool
	if err := tx.QueryRow(ctx, keylessPendingExistsSQL).Scan(&found); err != nil {
		return fmt.Errorf("schedule: count keyless pending work: %w", err)
	}
	if !found {
		return nil
	}
	keylessWarnMu.Lock()
	defer keylessWarnMu.Unlock()
	if now := time.Now(); now.Sub(keylessWarnLast) >= keylessWarnInterval {
		keylessWarnLast = now
		slog.WarnContext(ctx, "pending work cannot be dispatched: owner has no active Flickr account key", "has_pending_without_key", true)
	}
	return nil
}

const keylessPendingExistsSQL = `SELECT EXISTS (
	SELECT 1 FROM work_items WHERE state='pending' AND key_id IS NULL AND not_before <= now()
)`

func pendingKeys(ctx context.Context, tx pgx.Tx) ([]int64, error) {
	// Active keys come from the keys table. Per-key claim uses the pending
	// score index and returns nothing when a key has no due work.
	rows, err := tx.Query(ctx, `
SELECT DISTINCT k.id
FROM api_keys k
JOIN flickr_accounts a ON a.key_id = k.id AND a.status = 'ok'
WHERE k.status = 'valid'
ORDER BY k.id`)
	if err != nil {
		return nil, fmt.Errorf("schedule: list active keys: %w", err)
	}
	defer rows.Close()
	keys := make([]int64, 0)
	for rows.Next() {
		var key int64
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("schedule: scan active key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("schedule: read active keys: %w", err)
	}
	return keys, nil
}
