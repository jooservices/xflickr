package queuepostgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/schedule/app"
)

type row interface{ Scan(...any) error }

type rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}

type queueTx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Commit(context.Context) error
	Rollback(context.Context) error
}

type queueDB interface {
	Query(context.Context, string, ...any) (rows, error)
	QueryRow(context.Context, string, ...any) row
	Begin(context.Context) (queueTx, error)
}

type poolDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Begin(context.Context) (pgx.Tx, error)
}

type poolAdapter struct{ pool poolDB }

func (p poolAdapter) Query(ctx context.Context, query string, args ...any) (rows, error) {
	return p.pool.Query(ctx, query, args...)
}

func (p poolAdapter) QueryRow(ctx context.Context, query string, args ...any) row {
	return p.pool.QueryRow(ctx, query, args...)
}

func (p poolAdapter) Begin(ctx context.Context) (queueTx, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return tx, nil
}

// Store reads queue snapshots and emits aggregate queue state events.
type Store struct{ pool queueDB }

// New adapts a PostgreSQL pool for queue snapshot reads and events.
func New(pool *pgxpool.Pool) *Store {
	if pool == nil {
		return &Store{}
	}
	return &Store{pool: poolAdapter{pool: pool}}
}

const activeBacklog = "('pending', 'ready', 'running', 'waiting')"

func (s *Store) UserQueue(ctx context.Context, userID int64) ([]app.LaneEstimate, error) {
	if s == nil || s.pool == nil || ctx == nil || userID <= 0 {
		return nil, errors.New("schedule: queue database, context, and user are required")
	}
	query := `WITH lanes(lane) AS (VALUES ('person'::text), ('sizes'::text)),
backlog AS (
    SELECT CASE WHEN kind ILIKE '%size%' THEN 'sizes' ELSE 'person' END AS lane, count(*) AS calls
    FROM work_items WHERE user_id=$1 AND state IN ` + activeBacklog + ` GROUP BY 1
), key_rates AS (
    SELECT DISTINCT k.id,
        LEAST(k.per_hour_limit, k.per_second_limit * 3600,
              CASE WHEN (daily.value #>> '{}')::bigint > 0 THEN (daily.value #>> '{}')::bigint ELSE k.per_hour_limit END)::float8 AS calls_per_hour,
        (share.value #>> '{}')::float8 AS person_share,
        (reserve.value #>> '{}')::float8 AS reserve
    FROM flickr_accounts fa
	JOIN api_keys k ON k.id=fa.key_id AND k.status='valid'
    CROSS JOIN app_settings daily
    CROSS JOIN app_settings share
    CROSS JOIN app_settings reserve
    WHERE fa.user_id=$1 AND fa.status='ok' AND daily.key='daily_call_limit' AND share.key='person_lane_share' AND reserve.key='quota_reserve'
), capacity AS (
    SELECT lanes.lane, COALESCE(sum(CASE WHEN lanes.lane='person'
        THEN GREATEST(0, key_rates.calls_per_hour-key_rates.reserve)
        ELSE LEAST(floor(key_rates.calls_per_hour*(1-key_rates.person_share)), GREATEST(0, key_rates.calls_per_hour-key_rates.reserve)) END), 0)::float8 AS calls_per_hour
    FROM lanes LEFT JOIN key_rates ON true GROUP BY lanes.lane
)
SELECT lanes.lane, COALESCE(backlog.calls, 0), capacity.calls_per_hour
FROM lanes LEFT JOIN backlog USING(lane) JOIN capacity USING(lane)
ORDER BY lanes.lane`
	rows, err := s.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("schedule: query user queue: %w", err)
	}
	defer rows.Close()
	lanes := make([]app.LaneEstimate, 0, 2)
	for rows.Next() {
		var lane app.LaneEstimate
		if err := rows.Scan(&lane.Lane, &lane.BacklogCalls, &lane.EffectiveCallsPerHour); err != nil {
			return nil, fmt.Errorf("schedule: scan user queue: %w", err)
		}
		lanes = append(lanes, lane)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("schedule: read user queue: %w", err)
	}
	return lanes, nil
}

func (s *Store) AdminQueue(ctx context.Context) (app.AdminQueue, error) {
	if s == nil || s.pool == nil || ctx == nil {
		return app.AdminQueue{}, errors.New("schedule: queue database and context are required")
	}
	result := app.AdminQueue{BacklogByPlan: []app.PlanLaneBacklog{}, ReadyQueueSizes: []app.ReadyQueueSize{}}
	rows, err := s.pool.Query(ctx, `SELECT COALESCE(p.name, 'unassigned'),
    CASE WHEN w.kind ILIKE '%size%' THEN 'sizes' ELSE 'person' END, count(*)
FROM work_items w LEFT JOIN users u ON u.id=w.user_id LEFT JOIN plans p ON p.id=u.plan_id
WHERE w.state IN `+activeBacklog+`
GROUP BY 1,2 ORDER BY 1,2`)
	if err != nil {
		return app.AdminQueue{}, fmt.Errorf("schedule: query admin backlog: %w", err)
	}
	for rows.Next() {
		var item app.PlanLaneBacklog
		if err := rows.Scan(&item.Plan, &item.Lane, &item.BacklogCalls); err != nil {
			rows.Close()
			return app.AdminQueue{}, fmt.Errorf("schedule: scan admin backlog: %w", err)
		}
		result.BacklogByPlan = append(result.BacklogByPlan, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return app.AdminQueue{}, fmt.Errorf("schedule: read admin backlog: %w", err)
	}
	rows.Close()
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(GREATEST(0, floor(extract(epoch FROM now()-min(created_at)))), 0)::bigint FROM work_items WHERE state IN `+activeBacklog).Scan(&result.OldestItemAgeSeconds); err != nil {
		return app.AdminQueue{}, fmt.Errorf("schedule: query oldest backlog age: %w", err)
	}
	readyRows, err := s.pool.Query(ctx, `SELECT queue, count(*) FROM river_job WHERE state='available' GROUP BY queue ORDER BY queue`)
	if err != nil {
		return app.AdminQueue{}, fmt.Errorf("schedule: query ready queues: %w", err)
	}
	defer readyRows.Close()
	for readyRows.Next() {
		var item app.ReadyQueueSize
		if err := readyRows.Scan(&item.Queue, &item.ReadyJobs); err != nil {
			return app.AdminQueue{}, fmt.Errorf("schedule: scan ready queue: %w", err)
		}
		result.ReadyQueueSizes = append(result.ReadyQueueSizes, item)
	}
	if err := readyRows.Err(); err != nil {
		return app.AdminQueue{}, fmt.Errorf("schedule: read ready queues: %w", err)
	}
	return result, nil
}

// PublishQueueState records a minute-level queue snapshot for event consumers.
func (s *Store) PublishQueueState(ctx context.Context) error {
	if s == nil || s.pool == nil || ctx == nil {
		return errors.New("schedule: queue state database and context are required")
	}
	var lanes []struct {
		Lane  string `json:"lane"`
		Calls int64  `json:"backlog_calls"`
	}
	rows, err := s.pool.Query(ctx, `SELECT lane, count(*) FROM (
    SELECT CASE WHEN kind ILIKE '%size%' THEN 'sizes' ELSE 'person' END AS lane
    FROM work_items WHERE state IN `+activeBacklog+`
) active GROUP BY lane ORDER BY lane`)
	if err != nil {
		return fmt.Errorf("schedule: query queue state lanes: %w", err)
	}
	for rows.Next() {
		var item struct {
			Lane  string `json:"lane"`
			Calls int64  `json:"backlog_calls"`
		}
		if err := rows.Scan(&item.Lane, &item.Calls); err != nil {
			rows.Close()
			return fmt.Errorf("schedule: scan queue state lane: %w", err)
		}
		lanes = append(lanes, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("schedule: read queue state lanes: %w", err)
	}
	rows.Close()
	var ready int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE state='available'`).Scan(&ready); err != nil {
		return fmt.Errorf("schedule: query queue state ready jobs: %w", err)
	}
	payload, err := json.Marshal(map[string]any{"backlog_by_lane": lanes, "ready_jobs": ready})
	if err != nil {
		return fmt.Errorf("schedule: encode queue state: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("schedule: begin queue state event: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO outbox_events(topic,key,payload) VALUES('queue.state', 'global', $1::jsonb)`, payload); err != nil {
		return fmt.Errorf("schedule: publish queue state: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("schedule: commit queue state: %w", err)
	}
	return nil
}
