// Package river dispatches durable schedule rows into River transactions.
package river

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/schedule/app"
	"github.com/jooservices/xflickr/internal/schedule/domain"
)

// Store coordinates bounded work selection, River insertion, and lease recovery.
type Store struct {
	Pool     *pgxpool.Pool
	database scheduleDatabase
	listKeys func(context.Context, pgx.Tx) ([]int64, error)
	inflight func(context.Context, pgx.Tx, int64, string, int) (int, error)
}

var _ app.PumpSettingsSource = Store{}

type scheduleDatabase interface {
	Acquire(context.Context) (scheduleConnection, error)
	Begin(context.Context) (pgx.Tx, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type scheduleConnection interface {
	Begin(context.Context) (pgx.Tx, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Release()
}

type pgxScheduleDatabase struct{ pool *pgxpool.Pool }

func (db pgxScheduleDatabase) Acquire(ctx context.Context) (scheduleConnection, error) {
	conn, err := db.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (db pgxScheduleDatabase) Begin(ctx context.Context) (pgx.Tx, error) {
	return db.pool.Begin(ctx)
}

func (db pgxScheduleDatabase) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return db.pool.Exec(ctx, sql, args...)
}

func (db pgxScheduleDatabase) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return db.pool.Query(ctx, sql, args...)
}

func (db pgxScheduleDatabase) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return db.pool.QueryRow(ctx, sql, args...)
}

func (s Store) backend() scheduleDatabase {
	if s.database != nil {
		return s.database
	}
	if s.Pool == nil {
		return nil
	}
	return pgxScheduleDatabase{pool: s.Pool}
}

// LoadSettings reads scheduler limits from app_settings and retry defaults
// from the existing retry settings. Values are JSON integer scalars.
func LoadSettings(ctx context.Context, pool *pgxpool.Pool) (app.PumpSettings, error) {
	if pool == nil {
		return app.PumpSettings{}, errors.New("schedule: settings database is required")
	}
	return loadSettings(ctx, pgxScheduleDatabase{pool: pool})
}

// LoadPumpSettings reads the current scheduler settings for one pump tick.
func (s Store) LoadPumpSettings(ctx context.Context) (app.PumpSettings, error) {
	db := s.backend()
	if db == nil {
		return app.PumpSettings{}, errors.New("schedule: settings database is required")
	}
	return loadSettings(ctx, db)
}

func loadSettings(ctx context.Context, pool scheduleDatabase) (app.PumpSettings, error) {
	if ctx == nil {
		return app.PumpSettings{}, errors.New("schedule: settings context is required")
	}
	settingKeys := []string{"per_key_ready_cap", "node_ready_cap", "media_ready_cap", "ai_ready_cap", "sync_ready_cap", "retry_max", "retry_backoff", "lease_timeout", "max_depth", "score_weight_plan", "score_weight_direct_action", "score_weight_starred", "score_weight_depth", "score_weight_age_minutes", "score_aging_batch_size", "queue.reclaim_batch_size"}
	rows, err := pool.Query(ctx, `SELECT key, value #>> '{}' FROM app_settings WHERE key = ANY($1)`, settingKeys)
	if err != nil {
		return app.PumpSettings{}, fmt.Errorf("schedule: query pump settings: %w", err)
	}
	defer rows.Close()
	values := make(map[string]string, len(settingKeys))
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			return app.PumpSettings{}, fmt.Errorf("schedule: scan pump setting: %w", err)
		}
		values[key] = raw
	}
	if err := rows.Err(); err != nil {
		return app.PumpSettings{}, fmt.Errorf("schedule: read pump settings: %w", err)
	}
	for _, key := range settingKeys {
		if _, ok := values[key]; !ok {
			return app.PumpSettings{}, fmt.Errorf("schedule: pump setting %s is missing", key)
		}
	}
	integer := func(key string) (int64, error) { return strconv.ParseInt(values[key], 10, 64) }
	decimal := func(key string) (float64, error) { return strconv.ParseFloat(values[key], 64) }
	ints := make(map[string]int64, 11)
	for _, key := range []string{"per_key_ready_cap", "node_ready_cap", "media_ready_cap", "ai_ready_cap", "sync_ready_cap", "retry_max", "retry_backoff", "lease_timeout", "max_depth", "score_aging_batch_size", "queue.reclaim_batch_size"} {
		value, parseErr := integer(key)
		if parseErr != nil {
			return app.PumpSettings{}, fmt.Errorf("schedule: parse pump setting %s: %w", key, parseErr)
		}
		ints[key] = value
	}
	if ints["score_aging_batch_size"] < 1 {
		return app.PumpSettings{}, errors.New("schedule: score aging batch size must be positive")
	}
	weights := make([]float64, 5)
	for i, key := range []string{"score_weight_plan", "score_weight_direct_action", "score_weight_starred", "score_weight_depth", "score_weight_age_minutes"} {
		value, parseErr := decimal(key)
		if parseErr != nil {
			return app.PumpSettings{}, fmt.Errorf("schedule: parse pump setting %s: %w", key, parseErr)
		}
		weights[i] = value
	}
	settings := app.PumpSettings{PerKeyReadyCap: int(ints["per_key_ready_cap"]), NodeReadyCap: int(ints["node_ready_cap"]), MediaReadyCap: int(ints["media_ready_cap"]), AIReadyCap: int(ints["ai_ready_cap"]), SyncReadyCap: int(ints["sync_ready_cap"]), RetryMax: int(ints["retry_max"]), RetryBackoff: time.Duration(ints["retry_backoff"]) * time.Second, LeaseTimeout: time.Duration(ints["lease_timeout"]) * time.Second, ReclaimBatchSize: int(ints["queue.reclaim_batch_size"]), AgingBatchSize: int(ints["score_aging_batch_size"]), MaxDepth: int(ints["max_depth"]), ScoreWeights: domain.ScoreWeights{PlanWeight: weights[0], DirectAction: weights[1], Starred: weights[2], Depth: weights[3], AgeMinutes: weights[4]}}
	if err := settings.Validate(); err != nil {
		return app.PumpSettings{}, err
	}
	return settings, nil
}

func lockScheduleMetaCursor(ctx context.Context, tx pgx.Tx, key, initial string) (string, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO schedule_meta(key,value) VALUES ($1,$2) ON CONFLICT (key) DO NOTHING`, key, initial); err != nil {
		return "", fmt.Errorf("schedule: initialize cursor %s: %w", key, err)
	}
	var value string
	if err := tx.QueryRow(ctx, `SELECT value FROM schedule_meta WHERE key=$1 FOR UPDATE`, key).Scan(&value); err != nil {
		return "", fmt.Errorf("schedule: lock cursor %s: %w", key, err)
	}
	return value, nil
}

func storeScheduleMetaCursor(ctx context.Context, tx pgx.Tx, key, value string) error {
	if _, err := tx.Exec(ctx, `UPDATE schedule_meta SET value=$2 WHERE key=$1`, key, value); err != nil {
		return fmt.Errorf("schedule: store cursor %s: %w", key, err)
	}
	return nil
}
