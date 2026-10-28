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

type copyFromPool interface {
	CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error)
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
	rows, err := c.Pool.Query(ctx, `SELECT class, count(*), min(at) FROM api_call_log WHERE key_id=$1 AND at >= $2 AND class IS NOT NULL GROUP BY class`, keyID, since)
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
	if _, err := c.Pool.Exec(ctx, `INSERT INTO api_call_log (key_id, class, at, called_at) VALUES ($1, $2, $3, $3)`, keyID, class, at.UTC()); err != nil {
		return fmt.Errorf("quota: record call: %w", err)
	}
	return nil
}

// Prune deletes calls logged before the given time.
func (c CallLog) Prune(ctx context.Context, before time.Time) error {
	if c.Pool == nil || ctx == nil {
		return errors.New("quota: call log database and context are required")
	}
	if _, err := c.Pool.Exec(ctx, `DELETE FROM api_call_log WHERE class IS NOT NULL AND at < $1`, before.UTC()); err != nil {
		return fmt.Errorf("quota: prune call log: %w", err)
	}
	return nil
}

// InsertCalls appends one batch of non-quota diagnostic calls to the partitioned log.
func (c CallLog) InsertCalls(ctx context.Context, records []app.CallRecord) error {
	if c.Pool == nil || ctx == nil {
		return errors.New("quota: call log database and context are required")
	}
	if len(records) == 0 {
		return nil
	}
	pool, ok := c.Pool.(copyFromPool)
	if !ok {
		return errors.New("quota: call log database does not support batch copy")
	}
	for _, record := range records {
		if err := app.ValidateCallRecord(record); err != nil {
			return err
		}
	}
	columns := []string{
		"at", "called_at", "key_id", "user_id", "account_id", "node", "method", "status", "error_code", "latency_ms", "bytes", "lane",
		"flickr_account", "facet", "attempt", "started_at", "http_status", "flickr_stat", "flickr_error_code", "error_message", "response_bytes", "node_id", "request_id", "outcome",
	}
	inserted, err := pool.CopyFrom(ctx, pgx.Identifier{"api_call_log"}, columns, pgx.CopyFromSlice(len(records), func(i int) ([]any, error) {
		record := records[i]
		startedAt := record.StartedAt
		if startedAt.IsZero() {
			startedAt = record.At
		}
		responseBytes := record.ResponseBytes
		if responseBytes == 0 {
			responseBytes = record.Bytes
		}
		return []any{
			record.At.UTC(), record.At.UTC(), record.KeyID, nullableInt64(record.UserID), nullableInt64(record.AccountID),
			record.Node, record.Method, record.Status, nullableInt64(record.ErrorCode),
			record.LatencyMS, record.Bytes, record.Lane, nullableText(record.FlickrAccount), nullableText(record.Facet), positiveAttempt(record.Attempt),
			startedAt.UTC(), nullableInt(record.HTTPStatus), nullableText(record.FlickrStat), nullableInt64(record.FlickrErrorCode), nullableText(record.ErrorMessage),
			responseBytes, nullableText(record.NodeID), nullableText(record.RequestID), nullableText(record.Outcome),
		}, nil
	}))
	if err != nil {
		return fmt.Errorf("quota: batch insert calls: %w", err)
	}
	if inserted != int64(len(records)) {
		return fmt.Errorf("quota: inserted %d of %d call records", inserted, len(records))
	}
	return nil
}

// Stats returns aggregate call and crawl-completion metrics for the requested window.
func (c CallLog) Stats(ctx context.Context, window string) (app.CallStats, error) {
	if c.Pool == nil || ctx == nil {
		return app.CallStats{}, errors.New("quota: call log database and context are required")
	}
	var since time.Time
	switch window {
	case "1h":
		since = time.Now().UTC().Add(-time.Hour)
	case "24h":
		since = time.Now().UTC().Add(-24 * time.Hour)
	default:
		return app.CallStats{}, errors.New("quota: window must be 1h or 24h")
	}
	out := app.CallStats{
		Window: window, CallsPerHour: []app.CallOutcomeHour{}, TopErrorCodes: []app.CallErrorCode{},
		LatencyByMethod: []app.CallMethodLatency{}, RetriesByFacet: []app.CallFacetRetries{}, CompletedPerHour: []app.CallCompletedHour{},
	}
	var err error
	out.CallsPerHour, err = readCallStatsRows(ctx, c.Pool, since, "call outcomes", `SELECT date_trunc('hour', called_at), outcome, count(*) FROM api_call_log WHERE called_at >= $1 AND outcome IS NOT NULL AND method IS NOT NULL GROUP BY 1,2 ORDER BY 1,2`, func(rows pgx.Rows) (app.CallOutcomeHour, error) {
		var row app.CallOutcomeHour
		err := rows.Scan(&row.Hour, &row.Outcome, &row.Calls)
		return row, err
	})
	if err != nil {
		return app.CallStats{}, err
	}
	out.TopErrorCodes, err = readCallStatsRows(ctx, c.Pool, since, "call error codes", `SELECT COALESCE(flickr_error_code, error_code), count(*) FROM api_call_log WHERE called_at >= $1 AND method IS NOT NULL AND COALESCE(flickr_error_code, error_code) IS NOT NULL GROUP BY 1 ORDER BY count(*) DESC, 1 LIMIT 10`, func(rows pgx.Rows) (app.CallErrorCode, error) {
		var row app.CallErrorCode
		err := rows.Scan(&row.Code, &row.Calls)
		return row, err
	})
	if err != nil {
		return app.CallStats{}, err
	}
	out.LatencyByMethod, err = readCallStatsRows(ctx, c.Pool, since, "method latency", `SELECT method, percentile_cont(0.50) WITHIN GROUP (ORDER BY latency_ms), percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms) FROM api_call_log WHERE called_at >= $1 AND method IS NOT NULL AND latency_ms IS NOT NULL AND outcome IS DISTINCT FROM 'quota_wait' GROUP BY method ORDER BY method`, func(rows pgx.Rows) (app.CallMethodLatency, error) {
		var row app.CallMethodLatency
		err := rows.Scan(&row.Method, &row.P50MS, &row.P95MS)
		return row, err
	})
	if err != nil {
		return app.CallStats{}, err
	}
	out.RetriesByFacet, err = readCallStatsRows(ctx, c.Pool, since, "call retries", `SELECT facet, count(*) FILTER (WHERE attempt > 1 AND outcome IS DISTINCT FROM 'quota_wait') FROM api_call_log WHERE called_at >= $1 AND method IS NOT NULL AND facet IS NOT NULL GROUP BY facet HAVING count(*) FILTER (WHERE attempt > 1 AND outcome IS DISTINCT FROM 'quota_wait') > 0 ORDER BY 2 DESC, 1`, func(rows pgx.Rows) (app.CallFacetRetries, error) {
		var row app.CallFacetRetries
		err := rows.Scan(&row.Facet, &row.Retries)
		return row, err
	})
	if err != nil {
		return app.CallStats{}, err
	}
	out.CompletedPerHour, err = readCallStatsRows(ctx, c.Pool, since, "completed work", `SELECT date_trunc('hour', updated_at), count(*) FILTER (WHERE kind='photos'), count(*) FILTER (WHERE kind='person') FROM work_items WHERE state='done' AND updated_at >= $1 GROUP BY 1 ORDER BY 1`, func(rows pgx.Rows) (app.CallCompletedHour, error) {
		var row app.CallCompletedHour
		err := rows.Scan(&row.Hour, &row.Photos, &row.People)
		return row, err
	})
	if err != nil {
		return app.CallStats{}, err
	}
	return out, nil
}

func readCallStatsRows[T any](ctx context.Context, pool queryPool, since time.Time, metric, query string, scan func(pgx.Rows) (T, error)) ([]T, error) {
	rows, err := pool.Query(ctx, query, since)
	if err != nil {
		return nil, fmt.Errorf("quota: query %s: %w", metric, err)
	}
	defer rows.Close()
	out := make([]T, 0)
	for rows.Next() {
		row, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("quota: scan %s: %w", metric, err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("quota: read %s: %w", metric, err)
	}
	return out, nil
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func positiveAttempt(value int) int {
	if value <= 0 {
		return 1
	}
	return value
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}
