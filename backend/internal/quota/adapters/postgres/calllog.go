package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/quota/app"
)

type queryPool interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// CallLog counts calls recorded in api_call_log (key_id, class, called_at).
// The call log is owned by another module; until its table exists the log
// counts as empty, so a rebuild then relies on quota_usage alone.
type CallLog struct{ Pool queryPool }

var _ app.CallLog = CallLog{}

// Usage counts a key's logged calls since the given time, by class.
func (c CallLog) Usage(ctx context.Context, keyID int64, since time.Time) (app.CallUsage, error) {
	if c.Pool == nil || ctx == nil || keyID <= 0 {
		return app.CallUsage{}, errors.New("quota: call log database, context and key are required")
	}
	out := app.CallUsage{ByClass: map[string]int64{}}
	var exists bool
	if err := c.Pool.QueryRow(ctx, `SELECT to_regclass('api_call_log') IS NOT NULL`).Scan(&exists); err != nil {
		return app.CallUsage{}, fmt.Errorf("quota: check call log: %w", err)
	}
	if !exists {
		return out, nil
	}
	rows, err := c.Pool.Query(ctx, `SELECT class, count(*), min(called_at) FROM api_call_log WHERE key_id=$1 AND called_at >= $2 GROUP BY class`, keyID, since)
	if err != nil {
		return app.CallUsage{}, fmt.Errorf("quota: count calls: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var class string
		var n int64
		var first time.Time
		if err := rows.Scan(&class, &n, &first); err != nil {
			return app.CallUsage{}, fmt.Errorf("quota: scan calls: %w", err)
		}
		out.ByClass[class] += n
		if out.First.IsZero() || first.Before(out.First) {
			out.First = first
		}
	}
	return out, rows.Err()
}
