// Package postgres supplies per-key quota budgets from PostgreSQL settings.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/quota/app"
	"github.com/jooservices/xflickr/internal/quota/domain"
)

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Budgets struct{ Pool rowQuerier }

func (b Budgets) Budget(ctx context.Context, keyID int64) (domain.Budget, error) {
	if b.Pool == nil || ctx == nil || keyID <= 0 {
		return domain.Budget{}, errors.New("quota: budget database, context, and key are required")
	}
	var hourly, rate, burst int64
	var reserveRaw, shareRaw []byte
	err := b.Pool.QueryRow(ctx, `SELECT LEAST(k.per_hour_limit, CASE WHEN (daily.value #>> '{}')::bigint > 0 THEN (daily.value #>> '{}')::bigint ELSE k.per_hour_limit END), k.per_second_limit, k.burst, reserve.value, share.value FROM api_keys k CROSS JOIN app_settings reserve CROSS JOIN app_settings share CROSS JOIN app_settings daily WHERE k.id=$1 AND k.status='valid' AND reserve.key='quota_reserve' AND share.key='person_lane_share' AND daily.key='daily_call_limit'`, keyID).Scan(&hourly, &rate, &burst, &reserveRaw, &shareRaw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Budget{}, fmt.Errorf("%w: %d", app.ErrKeyUnavailable, keyID)
		}
		return domain.Budget{}, fmt.Errorf("quota: load key budget: %w", err)
	}
	var reserve int64
	var personShare float64
	if err := json.Unmarshal(reserveRaw, &reserve); err != nil {
		return domain.Budget{}, fmt.Errorf("quota: decode reserve setting: %w", err)
	}
	if err := json.Unmarshal(shareRaw, &personShare); err != nil {
		return domain.Budget{}, fmt.Errorf("quota: decode person lane setting: %w", err)
	}
	budget := domain.Budget{HourlyLimit: hourly, PerSecondRate: rate, Burst: burst, Reserve: reserve, PersonLaneShare: personShare}
	if err := budget.Validate(); err != nil {
		return domain.Budget{}, err
	}
	return budget, nil
}
