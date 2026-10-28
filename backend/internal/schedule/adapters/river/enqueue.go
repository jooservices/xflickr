// Package river dispatches durable schedule rows into River transactions.
package river

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/outbox"
	"github.com/jooservices/xflickr/internal/platform/queue"
	"github.com/jooservices/xflickr/internal/schedule/app"
	"github.com/jooservices/xflickr/internal/schedule/domain"
)

const pumpLockID int64 = 113

// Store coordinates bounded work selection, River insertion, and lease recovery.
type Store struct {
	Pool     *pgxpool.Pool
	database scheduleDatabase
	listKeys func(context.Context, pgx.Tx) ([]int64, error)
	inflight func(context.Context, pgx.Tx, int64, string) (int, error)
}

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

func loadSettings(ctx context.Context, pool scheduleDatabase) (app.PumpSettings, error) {
	if ctx == nil {
		return app.PumpSettings{}, errors.New("schedule: settings context is required")
	}
	settingKeys := []string{"per_key_ready_cap", "node_ready_cap", "media_ready_cap", "ai_ready_cap", "sync_ready_cap", "retry_max", "retry_backoff", "lease_timeout", "max_depth", "score_weight_plan", "score_weight_direct_action", "score_weight_starred", "score_weight_depth", "score_weight_age_minutes", "score_aging_batch_size"}
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
	ints := make(map[string]int64, 10)
	for _, key := range []string{"per_key_ready_cap", "node_ready_cap", "media_ready_cap", "ai_ready_cap", "sync_ready_cap", "retry_max", "retry_backoff", "lease_timeout", "max_depth", "score_aging_batch_size"} {
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
	settings := app.PumpSettings{PerKeyReadyCap: int(ints["per_key_ready_cap"]), NodeReadyCap: int(ints["node_ready_cap"]), MediaReadyCap: int(ints["media_ready_cap"]), AIReadyCap: int(ints["ai_ready_cap"]), SyncReadyCap: int(ints["sync_ready_cap"]), RetryMax: int(ints["retry_max"]), RetryBackoff: time.Duration(ints["retry_backoff"]) * time.Second, LeaseTimeout: time.Duration(ints["lease_timeout"]) * time.Second, AgingBatchSize: int(ints["score_aging_batch_size"]), MaxDepth: int(ints["max_depth"]), ScoreWeights: domain.ScoreWeights{PlanWeight: weights[0], DirectAction: weights[1], Starred: weights[2], Depth: weights[3], AgeMinutes: weights[4]}}
	if err := settings.Validate(); err != nil {
		return app.PumpSettings{}, err
	}
	return settings, nil
}

type workRow struct {
	ID      int64           `json:"work_item_id"`
	Kind    string          `json:"kind"`
	Subject string          `json:"subject_key"`
	Cursor  json.RawMessage `json:"cursor"`
}

type queueCap struct {
	name string
	cap  int
}

type expiredLease struct {
	id       int64
	attempts int
	kind     string
}

func (s Store) Pump(ctx context.Context, settings app.PumpSettings, credits app.CreditProvider, jobs queue.JobQueue) (int, error) {
	db := s.backend()
	if db == nil || credits == nil || jobs == nil {
		return 0, errors.New("schedule: pump adapter dependencies are required")
	}
	if ctx == nil {
		return 0, errors.New("schedule: pump context is required")
	}
	conn, err := db.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("schedule: acquire leader connection: %w", err)
	}
	defer conn.Release()
	var leader bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, pumpLockID).Scan(&leader); err != nil {
		return 0, fmt.Errorf("schedule: acquire pump leader lock: %w", err)
	}
	if !leader {
		return 0, nil
	}
	defer func() { _, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, pumpLockID) }()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("schedule: begin pump: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := assignOwnerKeys(ctx, tx); err != nil {
		return 0, err
	}
	if err := warnKeylessPending(ctx, tx); err != nil {
		return 0, err
	}
	listKeys := s.listKeys
	if listKeys == nil {
		listKeys = pendingKeys
	}
	keys, err := listKeys(ctx, tx)
	if err != nil {
		return 0, err
	}
	dispatched, err := s.pumpKeys(ctx, tx, keys, settings, credits, jobs)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("schedule: commit pump: %w", err)
	}
	return dispatched, nil
}

// RecomputeScores refreshes stored scores for a bounded, rotating batch of
// pending rows. Score math stays in the schedule domain.
func (s Store) RecomputeScores(ctx context.Context, batchSize int, weights domain.ScoreWeights, maxDepth int) (int, error) {
	if s.backend() == nil || ctx == nil || batchSize <= 0 || maxDepth < 0 {
		return 0, errors.New("schedule: invalid score aging dependencies")
	}
	return s.rescore(ctx, `WHERE w.state='pending' AND w.not_before <= now() ORDER BY w.score_updated_at NULLS FIRST, w.created_at, w.id LIMIT $1`, []any{batchSize}, weights, maxDepth)
}

// RescorePerson immediately refreshes the stored scores of one user's pending
// backlog for a person, so star/select changes affect dispatch order at once.
func (s Store) RescorePerson(ctx context.Context, userID int64, person string, weights domain.ScoreWeights, maxDepth int) (int, error) {
	if s.backend() == nil || ctx == nil || userID <= 0 || person == "" || maxDepth < 0 {
		return 0, errors.New("schedule: invalid person rescore dependencies")
	}
	return s.rescore(ctx, `WHERE w.state='pending' AND w.user_id=$1 AND w.subject_key=$2 ORDER BY w.id`, []any{userID, person}, weights, maxDepth)
}

func (s Store) rescore(ctx context.Context, selection string, args []any, weights domain.ScoreWeights, maxDepth int) (int, error) {
	tx, err := s.backend().Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("schedule: begin score aging: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	rows, err := tx.Query(ctx, `SELECT w.id, COALESCE(p.weight,1), w.direct_action AND COALESCE(ci.selected_until IS NULL OR ci.selected_until > now(), TRUE), w.starred, w.depth, EXTRACT(EPOCH FROM (now()-w.created_at))/60.0 FROM work_items w LEFT JOIN users u ON u.id=w.user_id LEFT JOIN plans p ON p.id=u.plan_id LEFT JOIN crawl_interests ci ON ci.user_id=w.user_id AND ci.person_nsid=w.subject_key `+selection+` FOR UPDATE OF w SKIP LOCKED`, args...)
	if err != nil {
		return 0, fmt.Errorf("schedule: select score aging batch: %w", err)
	}
	type scored struct {
		id    int64
		score float64
	}
	var batch []scored
	for rows.Next() {
		var id int64
		var planWeight, depth int
		var direct, starred bool
		var age float64
		if err := rows.Scan(&id, &planWeight, &direct, &starred, &depth, &age); err != nil {
			rows.Close()
			return 0, fmt.Errorf("schedule: scan score aging row: %w", err)
		}
		score, err := domain.Score(domain.ScoreInput{PlanWeight: planWeight, DirectAction: direct, Starred: starred, Depth: depth, MaxDepth: maxDepth, AgeMinutes: age}, weights)
		if err != nil {
			rows.Close()
			return 0, err
		}
		batch = append(batch, scored{id: id, score: score})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("schedule: read score aging rows: %w", err)
	}
	rows.Close()
	for _, row := range batch {
		if _, err := tx.Exec(ctx, `UPDATE work_items SET score=$2, score_updated_at=now() WHERE id=$1`, row.id, row.score); err != nil {
			return 0, fmt.Errorf("schedule: store work score: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("schedule: commit score aging: %w", err)
	}
	return len(batch), nil
}

// PersonRescorer rescores a person's backlog using the stored pump settings.
type PersonRescorer struct{ Pool *pgxpool.Pool }

func (r PersonRescorer) RescorePerson(ctx context.Context, userID int64, person string) error {
	if r.Pool == nil || ctx == nil {
		return errors.New("schedule: person rescorer dependencies are required")
	}
	settings, err := LoadSettings(ctx, r.Pool)
	if err != nil {
		return err
	}
	_, err = Store{Pool: r.Pool}.RescorePerson(ctx, userID, person, settings.ScoreWeights, settings.MaxDepth)
	return err
}

func (s Store) pumpKeys(ctx context.Context, tx pgx.Tx, keys []int64, settings app.PumpSettings, credits app.CreditProvider, jobs queue.JobQueue) (int, error) {
	count := s.inflight
	if count == nil {
		count = countInflight
	}
	dispatched := 0
	for _, keyID := range keys {
		keyCount, err := s.pumpKey(ctx, tx, keyID, settings, credits, jobs, count)
		var unavailable interface{ KeyUnavailable() bool }
		if errors.As(err, &unavailable) && unavailable.KeyUnavailable() {
			continue
		}
		if err != nil {
			return 0, err
		}
		dispatched += keyCount
	}
	return dispatched, nil
}

type inflightCounter func(context.Context, pgx.Tx, int64, string) (int, error)

func (s Store) pumpKey(ctx context.Context, tx pgx.Tx, keyID int64, settings app.PumpSettings, credits app.CreditProvider, jobs queue.JobQueue, count inflightCounter) (int, error) {
	remaining, err := credits.Capacity(ctx, keyID, 2*time.Minute)
	if err != nil {
		return 0, fmt.Errorf("schedule: read key %d quota credits: %w", keyID, err)
	}
	keyInflight, err := count(ctx, tx, keyID, "")
	if err != nil {
		return 0, err
	}
	nodeInflight, err := count(ctx, tx, 0, "")
	if err != nil {
		return 0, err
	}
	if remaining <= 0 || keyInflight >= settings.PerKeyReadyCap || nodeInflight >= settings.NodeReadyCap {
		return 0, nil
	}
	dispatched := 0
	for _, cap := range configuredQueues(settings) {
		workerInflight, err := count(ctx, tx, 0, cap.name)
		if err != nil {
			return 0, err
		}
		limit, err := app.Credits(app.CreditInput{
			RemainingQuota: remaining - dispatched,
			PerKeyReadyCap: settings.PerKeyReadyCap,
			KeyInflight:    keyInflight + dispatched,
			NodeCap:        settings.NodeReadyCap,
			NodeInflight:   nodeInflight + dispatched,
			WorkerCap:      cap.cap,
			WorkerInflight: workerInflight,
		})
		if err != nil {
			return 0, err
		}
		if limit == 0 {
			continue
		}
		queued, err := s.pumpQueue(ctx, tx, keyID, cap, limit, settings, jobs)
		if err != nil {
			return 0, err
		}
		dispatched += queued
		if dispatched >= remaining || keyInflight+dispatched >= settings.PerKeyReadyCap || nodeInflight+dispatched >= settings.NodeReadyCap {
			break
		}
	}
	return dispatched, nil
}

func (s Store) pumpQueue(ctx context.Context, tx pgx.Tx, keyID int64, cap queueCap, limit int, settings app.PumpSettings, jobs queue.JobQueue) (int, error) {
	rows, err := tx.Query(ctx, `SELECT COALESCE(p.id,(SELECT id FROM plans WHERE name='free')), COALESCE(p.resource_share,(SELECT resource_share FROM plans WHERE name='free')), count(*) FROM work_items w LEFT JOIN users u ON u.id=w.user_id LEFT JOIN plans p ON p.id=u.plan_id WHERE w.state='pending' AND w.key_id=$1 AND w.worker_type=$2 AND w.not_before<=now() AND NOT EXISTS (SELECT 1 FROM crawl_interests ci WHERE ci.user_id=w.user_id AND ci.person_nsid=w.subject_key AND ci.paused_at IS NOT NULL) GROUP BY 1,2 ORDER BY 1`, keyID, cap.name)
	if err != nil {
		return 0, fmt.Errorf("schedule: count plan backlog: %w", err)
	}
	groups := make([]app.ShareGroup, 0)
	for rows.Next() {
		var group app.ShareGroup
		if err := rows.Scan(&group.ID, &group.Share, &group.Available); err != nil {
			rows.Close()
			return 0, fmt.Errorf("schedule: scan plan backlog: %w", err)
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("schedule: read plan backlog: %w", err)
	}
	rows.Close()
	rotation := int(time.Now().Unix() / int64(app.PumpInterval/time.Second))
	allocations, err := app.SplitShare(limit, groups, rotation)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, group := range groups {
		allocation := allocations[group.ID]
		if allocation == 0 {
			continue
		}
		queued, err := s.pumpQueuePlan(ctx, tx, keyID, cap, group.ID, allocation, settings, jobs)
		if err != nil {
			return 0, err
		}
		total += queued
	}
	return total, nil
}

func (s Store) pumpQueuePlan(ctx context.Context, tx pgx.Tx, keyID int64, cap queueCap, planID int64, limit int, settings app.PumpSettings, jobs queue.JobQueue) (int, error) {
	rows, err := tx.Query(ctx, `SELECT w.id, w.kind, w.subject_key, w.cursor FROM work_items w LEFT JOIN users u ON u.id=w.user_id WHERE w.state = 'pending' AND w.key_id = $1 AND w.worker_type = $2 AND w.not_before <= now() AND COALESCE(u.plan_id,(SELECT id FROM plans WHERE name='free'))=$3 AND NOT EXISTS (SELECT 1 FROM crawl_interests ci WHERE ci.user_id=w.user_id AND ci.person_nsid=w.subject_key AND ci.paused_at IS NOT NULL) ORDER BY w.score DESC, w.not_before, w.id LIMIT $4 FOR UPDATE OF w SKIP LOCKED`, keyID, cap.name, planID, limit)
	if err != nil {
		return 0, fmt.Errorf("schedule: select pending work: %w", err)
	}
	batch := make([]workRow, 0, limit)
	for rows.Next() {
		var row workRow
		if err := rows.Scan(&row.ID, &row.Kind, &row.Subject, &row.Cursor); err != nil {
			rows.Close()
			return 0, fmt.Errorf("schedule: scan pending work: %w", err)
		}
		batch = append(batch, row)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return 0, fmt.Errorf("schedule: read pending work: %w", rowsErr)
	}
	for _, row := range batch {
		args, _ := json.Marshal(row)
		if err := jobs.Enqueue(ctx, tx, queue.Job{Kind: row.Kind, Args: args}, queue.Options{Queue: cap.name, MaxAttempts: max(1, settings.RetryMax), LeaseTimeout: settings.LeaseTimeout, Backoff: settings.RetryBackoff}); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `UPDATE work_items SET state='ready', lease_expires_at=now()+$2::bigint * interval '1 microsecond', updated_at=now() WHERE id=$1 AND state='pending'`, row.ID, settings.LeaseTimeout.Microseconds()); err != nil {
			return 0, fmt.Errorf("schedule: mark work ready: %w", err)
		}
	}
	return len(batch), nil
}

func configuredQueues(settings app.PumpSettings) []queueCap {
	return []queueCap{{"media", settings.MediaReadyCap}, {"ai", settings.AIReadyCap}, {"sync", settings.SyncReadyCap}}
}

// assignOwnerKeys gives pending rows without a key the key of the owning
// user's active Flickr account, so producers that predate key assignment
// cannot strand work. Rows without an active account stay unassigned.
func assignOwnerKeys(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `UPDATE work_items w SET key_id=owner.key_id, updated_at=now()
FROM (SELECT DISTINCT ON (user_id) user_id, key_id FROM flickr_accounts WHERE status='ok' ORDER BY user_id, id) owner
WHERE w.state='pending' AND w.key_id IS NULL AND w.user_id=owner.user_id`); err != nil {
		return fmt.Errorf("schedule: assign owner keys: %w", err)
	}
	return nil
}

const keylessWarnInterval = time.Minute

var (
	keylessWarnMu   sync.Mutex
	keylessWarnLast time.Time
)

// warnKeylessPending logs, at most once per minute, due pending rows that no
// key can dispatch.
func warnKeylessPending(ctx context.Context, tx pgx.Tx) error {
	var count int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM work_items WHERE state='pending' AND key_id IS NULL AND not_before <= now()`).Scan(&count); err != nil {
		return fmt.Errorf("schedule: count keyless pending work: %w", err)
	}
	if count == 0 {
		return nil
	}
	keylessWarnMu.Lock()
	defer keylessWarnMu.Unlock()
	if now := time.Now(); now.Sub(keylessWarnLast) >= keylessWarnInterval {
		keylessWarnLast = now
		slog.WarnContext(ctx, "pending work cannot be dispatched: owner has no active Flickr account key", "pending_without_key", count)
	}
	return nil
}

func pendingKeys(ctx context.Context, tx pgx.Tx) ([]int64, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT key_id FROM work_items WHERE state='pending' AND key_id IS NOT NULL AND not_before <= now() ORDER BY key_id`)
	if err != nil {
		return nil, fmt.Errorf("schedule: list pending keys: %w", err)
	}
	defer rows.Close()
	keys := make([]int64, 0)
	for rows.Next() {
		var key int64
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("schedule: scan pending key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("schedule: read pending keys: %w", err)
	}
	return keys, nil
}

func countInflight(ctx context.Context, tx pgx.Tx, keyID int64, worker string) (int, error) {
	var count int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM work_items WHERE state IN ('ready','running') AND ($1=0 OR key_id=$1) AND ($2='' OR (CASE WHEN kind LIKE 'ai%' THEN 'ai' WHEN kind LIKE 'media%' THEN 'media' ELSE 'sync' END)=$2)`, keyID, worker).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("schedule: count inflight work: %w", err)
	}
	return count, nil
}

// liveRiverStates are the River job states in which a job still waits for or
// holds a worker. River owns such a job (its timeout and rescuer), so the lease
// reaper leaves the work item alone: a lease cannot expire while the job waits
// in the queue, and a live job is never cancelled.
const liveRiverStates = `('available','pending','retryable','scheduled','running')`

// SelfHealAfter is how long a work item that failed only because its lease
// expired (an infrastructure fault) stays failed before Reclaim gives it a
// fresh start with attempts reset.
const SelfHealAfter = 15 * time.Minute

// Requeue returns failed work items (all when ids is empty) to pending with
// attempts reset. Rows keep their payload; the normal pump and quota gate
// dispatch them. It reports how many rows were requeued.
func (s Store) Requeue(ctx context.Context, ids []int64, now time.Time) (int, error) {
	db := s.backend()
	if db == nil {
		return 0, errors.New("schedule: requeue database is required")
	}
	tag, err := db.Exec(ctx, `UPDATE work_items SET state='pending', attempts=0, not_before=$1, lease_expires_at=NULL, last_error=NULL, updated_at=$1
		WHERE state='failed' AND (coalesce(cardinality($2::bigint[]), 0) = 0 OR id = ANY($2))`, now, ids)
	if err != nil {
		return 0, fmt.Errorf("schedule: requeue failed work: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// Reclaim returns work items with an expired lease and no live River job to
// pending, with exponential delay based on the already-incremented attempt.
func (s Store) Reclaim(ctx context.Context, retryMax int, base time.Duration, now time.Time) (int, error) {
	db := s.backend()
	if db == nil {
		return 0, errors.New("schedule: reclaim database is required")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("schedule: begin reclaim: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	rows, err := tx.Query(ctx, `SELECT id, attempts, kind FROM work_items WHERE state IN ('ready','running') AND lease_expires_at <= $1
		AND NOT EXISTS (SELECT 1 FROM river_job j WHERE j.state IN `+liveRiverStates+` AND j.args->'args'->>'work_item_id' = work_items.id::text)
		ORDER BY lease_expires_at, id FOR UPDATE SKIP LOCKED`, now)
	if err != nil {
		return 0, fmt.Errorf("schedule: select expired leases: %w", err)
	}
	expiredRows := make([]expiredLease, 0)
	for rows.Next() {
		var row expiredLease
		if err := rows.Scan(&row.id, &row.attempts, &row.kind); err != nil {
			rows.Close()
			return 0, fmt.Errorf("schedule: scan expired lease: %w", err)
		}
		expiredRows = append(expiredRows, row)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return 0, fmt.Errorf("schedule: read expired leases: %w", rowsErr)
	}
	if err := applyExpiredLeases(ctx, tx, expiredRows, retryMax, base, now); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE work_items SET state='pending', attempts=0, not_before=$1, lease_expires_at=NULL, last_error=NULL, updated_at=$1
		WHERE state='failed' AND last_error='lease expired' AND updated_at <= $2`, now, now.Add(-SelfHealAfter)); err != nil {
		return 0, fmt.Errorf("schedule: self-heal lease-expired work: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("schedule: commit reclaim: %w", err)
	}
	return len(expiredRows), nil
}

func applyExpiredLeases(ctx context.Context, tx pgx.Tx, expiredRows []expiredLease, retryMax int, base time.Duration, now time.Time) error {
	for _, row := range expiredRows {
		attempts := row.attempts + 1
		if attempts >= retryMax {
			if _, err := tx.Exec(ctx, `UPDATE work_items SET state='failed', attempts=$2, last_error='lease expired', lease_expires_at=NULL, updated_at=$3 WHERE id=$1`, row.id, attempts, now); err != nil {
				return fmt.Errorf("schedule: fail exhausted lease: %w", err)
			}
			if err := outbox.PublishContext(ctx, tx, "job.dead", fmt.Sprint(row.id), map[string]any{"work_item_id": row.id, "kind": row.kind, "attempts": attempts, "error": "lease expired"}); err != nil {
				return err
			}
			continue
		}
		delay := base
		for i := 1; i < attempts && delay < time.Hour; i++ {
			delay *= 2
		}
		if _, err := tx.Exec(ctx, `UPDATE work_items SET state='pending', attempts=$2, not_before=$3, lease_expires_at=NULL, last_error='lease expired', updated_at=$4 WHERE id=$1`, row.id, attempts, now.Add(delay), now); err != nil {
			return fmt.Errorf("schedule: return expired lease to pending: %w", err)
		}
	}
	return nil
}
