package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jooservices/xflickr/internal/quota/app"
)

type queryPool interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// CallLog writes and counts api_call_log (key_id, class, called_at). A missing
// table is an error, so a rebuild fails closed instead of treating it as empty.
type CallLog struct{ Pool queryPool }

var _ app.CallLog = CallLog{}

// Usage counts a key's logged calls since the given time, by class.
func (c CallLog) Usage(ctx context.Context, keyID int64, since time.Time) (app.CallUsage, error) {
	if c.Pool == nil || ctx == nil || keyID <= 0 {
		return app.CallUsage{}, errors.New("quota: call log database, context and key are required")
	}
	out := app.CallUsage{ByClass: map[string]int64{}}
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

// Record logs one granted call.
func (c CallLog) Record(ctx context.Context, keyID int64, class string, at time.Time) error {
	if c.Pool == nil || ctx == nil || keyID <= 0 || class == "" {
		return errors.New("quota: call log database, context, key and class are required")
	}
	if _, err := c.Pool.Exec(ctx, `INSERT INTO api_call_log (key_id, class, called_at) VALUES ($1, $2, $3)`, keyID, class, at.UTC()); err != nil {
		return fmt.Errorf("quota: record call: %w", err)
	}
	return nil
}

// Prune deletes calls logged before the given time.
func (c CallLog) Prune(ctx context.Context, before time.Time) error {
	if c.Pool == nil || ctx == nil {
		return errors.New("quota: call log database and context are required")
	}
	if _, err := c.Pool.Exec(ctx, `DELETE FROM api_call_log WHERE called_at < $1`, before.UTC()); err != nil {
		return fmt.Errorf("quota: prune call log: %w", err)
	}
	return nil
}
