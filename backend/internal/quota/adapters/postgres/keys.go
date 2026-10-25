package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/quota/app"
)

type keysPool interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Keys reads key ownership from api_keys and previous windows from quota_usage.
type Keys struct{ Pool keysPool }

var (
	_ app.KeyDirectory = Keys{}
	_ app.UsageHistory = Keys{}
)

// Owned returns the valid key when it belongs to userID.
func (k Keys) Owned(ctx context.Context, userID, keyID int64) (app.KeyInfo, bool, error) {
	if k.Pool == nil || ctx == nil || userID <= 0 || keyID <= 0 {
		return app.KeyInfo{}, false, errors.New("quota: key database, context, user and key are required")
	}
	info := app.KeyInfo{}
	err := k.Pool.QueryRow(ctx, `SELECT id, user_id, label FROM api_keys WHERE id=$1 AND user_id=$2 AND status='valid'`, keyID, userID).Scan(&info.ID, &info.UserID, &info.Label)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.KeyInfo{}, false, nil
	}
	if err != nil {
		return app.KeyInfo{}, false, fmt.Errorf("quota: load key: %w", err)
	}
	return info, true, nil
}

// Active lists every valid key.
func (k Keys) Active(ctx context.Context) ([]app.KeyInfo, error) {
	if k.Pool == nil || ctx == nil {
		return nil, errors.New("quota: key database and context are required")
	}
	rows, err := k.Pool.Query(ctx, `SELECT id, user_id, label FROM api_keys WHERE status='valid' ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("quota: list keys: %w", err)
	}
	defer rows.Close()
	var keys []app.KeyInfo
	for rows.Next() {
		var info app.KeyInfo
		if err := rows.Scan(&info.ID, &info.UserID, &info.Label); err != nil {
			return nil, fmt.Errorf("quota: scan key: %w", err)
		}
		keys = append(keys, info)
	}
	return keys, rows.Err()
}

// Previous returns the newest stored window that started in [notBefore, before).
func (k Keys) Previous(ctx context.Context, keyID int64, notBefore, before time.Time) (app.Usage, bool, error) {
	if k.Pool == nil || ctx == nil || keyID <= 0 {
		return app.Usage{}, false, errors.New("quota: usage database, context and key are required")
	}
	usage := app.Usage{KeyID: keyID}
	err := k.Pool.QueryRow(ctx, `SELECT window_start, used FROM quota_usage WHERE key_id=$1 AND window_start >= $2 AND window_start < $3 ORDER BY window_start DESC LIMIT 1`, keyID, notBefore, before).Scan(&usage.WindowStart, &usage.Used)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Usage{}, false, nil
	}
	if err != nil {
		return app.Usage{}, false, fmt.Errorf("quota: load previous usage: %w", err)
	}
	return usage, true, nil
}
