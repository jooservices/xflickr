package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jooservices/xflickr/internal/quota/app"
)

type usagePool interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Usage stores flushed per-key usage in quota_usage.
type Usage struct{ Pool usagePool }

var _ app.UsageStore = Usage{}

// Save upserts a window's usage; used and by_class only move forward within a
// window so a late or partial flush cannot lower them.
func (u Usage) Save(ctx context.Context, usage app.Usage) error {
	if u.Pool == nil || ctx == nil || usage.KeyID <= 0 || usage.WindowStart.IsZero() || usage.Used < 0 {
		return errors.New("quota: usage database, context, key and window are required")
	}
	byClass, err := json.Marshal(usage.ByClass)
	if err != nil {
		return fmt.Errorf("quota: encode usage by class: %w", err)
	}
	if usage.ByClass == nil {
		byClass = []byte("{}")
	}
	_, err = u.Pool.Exec(ctx, `INSERT INTO quota_usage (key_id, window_start, used, by_class) VALUES ($1, $2, $3, $4)
ON CONFLICT (key_id, window_start) DO UPDATE SET
  by_class = CASE WHEN EXCLUDED.used >= quota_usage.used THEN EXCLUDED.by_class ELSE quota_usage.by_class END,
  used = GREATEST(quota_usage.used, EXCLUDED.used),
  updated_at = now()`, usage.KeyID, usage.WindowStart.UTC().Truncate(time.Millisecond), usage.Used, byClass)
	if err != nil {
		return fmt.Errorf("quota: save usage: %w", err)
	}
	return nil
}

// Latest returns the newest window that started after since.
func (u Usage) Latest(ctx context.Context, keyID int64, since time.Time) (app.Usage, bool, error) {
	if u.Pool == nil || ctx == nil || keyID <= 0 {
		return app.Usage{}, false, errors.New("quota: usage database, context and key are required")
	}
	var usage app.Usage
	var raw []byte
	err := u.Pool.QueryRow(ctx, `SELECT window_start, used, by_class FROM quota_usage WHERE key_id=$1 AND window_start > $2 ORDER BY window_start DESC LIMIT 1`, keyID, since).Scan(&usage.WindowStart, &usage.Used, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Usage{}, false, nil
	}
	if err != nil {
		return app.Usage{}, false, fmt.Errorf("quota: load usage: %w", err)
	}
	usage.KeyID = keyID
	if err := json.Unmarshal(raw, &usage.ByClass); err != nil {
		return app.Usage{}, false, fmt.Errorf("quota: decode usage by class: %w", err)
	}
	return usage, true, nil
}

// KeyIDs lists the keys that can still make calls.
func (u Usage) KeyIDs(ctx context.Context) ([]int64, error) {
	if u.Pool == nil || ctx == nil {
		return nil, errors.New("quota: usage database and context are required")
	}
	rows, err := u.Pool.Query(ctx, `SELECT id FROM api_keys WHERE status='valid' ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("quota: list keys: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("quota: scan key: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
