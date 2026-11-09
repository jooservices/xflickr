package postgres

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// HealthSettings are the admin-tunable crawl-health monitor bounds.
type HealthSettings struct {
	Interval         time.Duration
	StatementTimeout time.Duration
}

// DefaultHealthSettings returns registry defaults when rows are missing.
func DefaultHealthSettings() HealthSettings {
	return HealthSettings{Interval: time.Minute, StatementTimeout: DefaultStatementTimeout}
}

// LoadHealthSettings reads ops.health_interval and ops.health_statement_timeout
// from app_settings. Missing or invalid rows fall back to defaults and caps.
func LoadHealthSettings(ctx context.Context, q Querier) (HealthSettings, error) {
	out := DefaultHealthSettings()
	if ctx == nil || q == nil {
		return out, fmt.Errorf("ops: health settings query requires context and querier")
	}
	rows, err := q.Query(ctx, `SELECT key, value #>> '{}' FROM app_settings WHERE key = ANY($1)`,
		[]string{"ops.health_interval", "ops.health_statement_timeout"})
	if err != nil {
		return out, fmt.Errorf("ops: query health settings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			return out, fmt.Errorf("ops: scan health setting: %w", err)
		}
		seconds, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return out, fmt.Errorf("ops: parse health setting %s: %w", key, err)
		}
		switch key {
		case "ops.health_interval":
			out.Interval = clampDuration(time.Duration(seconds)*time.Second, 10*time.Second, time.Hour)
		case "ops.health_statement_timeout":
			out.StatementTimeout = clampDuration(time.Duration(seconds)*time.Second, time.Second, time.Minute)
		}
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("ops: iterate health settings: %w", err)
	}
	return out, nil
}

func clampDuration(value, min, max time.Duration) time.Duration {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
