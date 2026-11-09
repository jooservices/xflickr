package postgres

import (
	"context"
	"fmt"
	"reflect"
	"strconv"

	"github.com/jooservices/xflickr/internal/platform/maintenance/app"
	"github.com/jooservices/xflickr/internal/platform/maintenance/domain"
)

const (
	settingRetentionDays = "maintenance.api_call_log_retention_days"
	settingDryRun        = "maintenance.api_call_log_dry_run"
)

// RetentionSettings reads admin-tunable retention controls from app_settings.
type RetentionSettings struct {
	Querier Querier
}

var _ app.Settings = RetentionSettings{}

// DefaultRetentionSettings returns registry defaults when rows are missing.
func DefaultRetentionSettings() (days int, dryRun bool) {
	return domain.DefaultRetentionDays, false
}

// RetentionDays returns the clamped retention window.
func (s RetentionSettings) RetentionDays(ctx context.Context) (int, error) {
	days, _, err := s.load(ctx)
	return days, err
}

// DryRun reports whether retention should only log intended actions.
func (s RetentionSettings) DryRun(ctx context.Context) (bool, error) {
	_, dryRun, err := s.load(ctx)
	return dryRun, err
}

func (s RetentionSettings) load(ctx context.Context) (int, bool, error) {
	days, dryRun := DefaultRetentionSettings()
	if ctx == nil || nilQuerier(s.Querier) {
		return days, dryRun, fmt.Errorf("maintenance: retention settings require context and querier")
	}
	rows, err := s.Querier.Query(ctx, `SELECT key, value #>> '{}' FROM app_settings WHERE key = ANY($1)`,
		[]string{settingRetentionDays, settingDryRun})
	if err != nil {
		return days, dryRun, fmt.Errorf("maintenance: query retention settings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			return days, dryRun, fmt.Errorf("maintenance: scan retention setting: %w", err)
		}
		switch key {
		case settingRetentionDays:
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				return days, dryRun, fmt.Errorf("maintenance: parse %s: %w", key, err)
			}
			days = domain.ClampRetentionDays(parsed)
		case settingDryRun:
			parsed, err := strconv.ParseBool(raw)
			if err != nil {
				return days, dryRun, fmt.Errorf("maintenance: parse %s: %w", key, err)
			}
			dryRun = parsed
		}
	}
	if err := rows.Err(); err != nil {
		return days, dryRun, fmt.Errorf("maintenance: iterate retention settings: %w", err)
	}
	return days, dryRun, nil
}

func nilQuerier(q Querier) bool {
	if q == nil {
		return true
	}
	value := reflect.ValueOf(q)
	return value.Kind() == reflect.Pointer && value.IsNil()
}
