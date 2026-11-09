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

type expiredLeaseCandidate struct {
	lease      expiredLease
	expiresAt  time.Time
	shouldReap bool
}

type expiredLeaseCursor struct {
	ExpiresAt time.Time `json:"expires_at"`
	ID        int64     `json:"id"`
}

type inflightSnapshot struct {
	node    int
	workers map[string]int
}

type pumpResult struct {
	total   int
	workers map[string]int
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
	if err := assignOwnerKeys(ctx, tx, settings.ReclaimBatchSize); err != nil {
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

const staticScoreExpr = `COALESCE(p.weight,1)*$1
	+ CASE WHEN work_items_active_direct_action(w2.direct_action,w2.user_id,w2.subject_key) THEN $2 ELSE 0 END
	+ CASE WHEN w2.starred THEN $3 ELSE 0 END
	+ ($4::int - w2.depth)*$5
	- $6*EXTRACT(EPOCH FROM w2.created_at)/60.0`

// RescorePendingBatch rewrites a bounded batch of pending scores with one
// set-based UPDATE. The stored value is the time-invariant static sort key.
func (s Store) RescorePendingBatch(ctx context.Context, batchSize int, weights domain.ScoreWeights, maxDepth int) (int, error) {
	if s.backend() == nil || ctx == nil || batchSize <= 0 || maxDepth < 0 {
		return 0, errors.New("schedule: invalid score rescore dependencies")
	}
	if _, err := domain.Score(domain.ScoreInput{MaxDepth: maxDepth}, weights); err != nil {
		return 0, err
	}
	return s.rescoreSet(ctx, `WHERE w2.state='pending' AND w2.score IS DISTINCT FROM (`+staticScoreExpr+`) ORDER BY w2.id LIMIT $7`, []any{
		weights.PlanWeight, weights.DirectAction, weights.Starred, maxDepth, weights.Depth, weights.AgeMinutes, batchSize,
	})
}

// RescoreExpiredSelectionsBatch removes direct-action bonuses after the
// associated user's selection expires, without scanning unrelated pending work.
func (s Store) RescoreExpiredSelectionsBatch(ctx context.Context, batchSize int, weights domain.ScoreWeights, maxDepth int) (int, error) {
	if s.backend() == nil || ctx == nil || batchSize <= 0 || maxDepth < 0 {
		return 0, errors.New("schedule: invalid expired selection rescore dependencies")
	}
	if _, err := domain.Score(domain.ScoreInput{MaxDepth: maxDepth}, weights); err != nil {
		return 0, err
	}
	return s.rescoreSet(ctx, `WHERE w2.state='pending'
		AND w2.direct_action
		AND ci.selected_until IS NOT NULL
		AND ci.selected_until <= now()
		AND w2.score IS DISTINCT FROM (`+staticScoreExpr+`)
		ORDER BY ci.selected_until, w2.id LIMIT $7`, []any{
		weights.PlanWeight, weights.DirectAction, weights.Starred, maxDepth, weights.Depth, weights.AgeMinutes, batchSize,
	})
}

// RescorePerson immediately refreshes the stored scores of one user's pending
// backlog for a person, so star/select changes affect dispatch order at once.
func (s Store) RescorePerson(ctx context.Context, userID int64, person string, weights domain.ScoreWeights, maxDepth int) (int, error) {
	if s.backend() == nil || ctx == nil || userID <= 0 || person == "" || maxDepth < 0 {
		return 0, errors.New("schedule: invalid person rescore dependencies")
	}
	if _, err := domain.Score(domain.ScoreInput{MaxDepth: maxDepth}, weights); err != nil {
		return 0, err
	}
	return s.rescoreSet(ctx, `WHERE w2.state='pending' AND w2.user_id=$7 AND w2.subject_key=$8`, []any{
		weights.PlanWeight, weights.DirectAction, weights.Starred, maxDepth, weights.Depth, weights.AgeMinutes, userID, person,
	})
}

func (s Store) rescoreSet(ctx context.Context, selection string, args []any) (int, error) {
	tag, err := s.backend().Exec(ctx, `
UPDATE work_items AS w SET score = batch.score
FROM (
	SELECT w2.id, `+staticScoreExpr+` AS score
	FROM work_items w2
	LEFT JOIN users u ON u.id=w2.user_id
	LEFT JOIN plans p ON p.id=u.plan_id
	LEFT JOIN crawl_interests ci ON ci.user_id=w2.user_id AND ci.person_nsid=w2.subject_key
	`+selection+`
) AS batch
WHERE w.id = batch.id`, args...)
	if err != nil {
		return 0, fmt.Errorf("schedule: set-based score update: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

const weightsFingerprintKey = "score_weights_fingerprint"

// ComputeWeightsFingerprint returns the live fingerprint of score weights, max
// depth, and plan weights used to detect a needed full pending rescore.
func (s Store) ComputeWeightsFingerprint(ctx context.Context, weights domain.ScoreWeights, maxDepth int) (string, error) {
	if s.backend() == nil || ctx == nil {
		return "", errors.New("schedule: fingerprint dependencies are required")
	}
	var planDigest string
	if err := s.backend().QueryRow(ctx, `SELECT coalesce(md5(string_agg(id::text || ':' || weight::text, ',' ORDER BY id)), '') FROM plans`).Scan(&planDigest); err != nil {
		return "", fmt.Errorf("schedule: hash plan weights: %w", err)
	}
	return domain.WeightsFingerprint(weights, maxDepth) + "|plans:" + planDigest, nil
}

// LoadWeightsFingerprint reads the last completed rescore fingerprint.
func (s Store) LoadWeightsFingerprint(ctx context.Context) (string, error) {
	if s.backend() == nil || ctx == nil {
		return "", errors.New("schedule: fingerprint dependencies are required")
	}
	var value string
	err := s.backend().QueryRow(ctx, `SELECT value FROM schedule_meta WHERE key=$1`, weightsFingerprintKey).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("schedule: load weights fingerprint: %w", err)
	}
	return value, nil
}

// StoreWeightsFingerprint records that a full pending rescore finished for fp.
func (s Store) StoreWeightsFingerprint(ctx context.Context, fp string) error {
	if s.backend() == nil || ctx == nil {
		return errors.New("schedule: fingerprint dependencies are required")
	}
	_, err := s.backend().Exec(ctx, `
INSERT INTO schedule_meta (key, value) VALUES ($1, $2)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, weightsFingerprintKey, fp)
	if err != nil {
		return fmt.Errorf("schedule: store weights fingerprint: %w", err)
	}
	return nil
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
	if len(keys) == 0 {
		return 0, nil
	}
	count := s.inflight
	if count == nil {
		count = countInflight
	}
	snapshot := inflightSnapshot{workers: make(map[string]int, 3)}
	var err error
	snapshot.node, err = count(ctx, tx, 0, "", settings.NodeReadyCap)
	if err != nil {
		return 0, err
	}
	for _, cap := range configuredQueues(settings) {
		if cap.cap == 0 {
			continue
		}
		snapshot.workers[cap.name], err = count(ctx, tx, 0, cap.name, cap.cap)
		if err != nil {
			return 0, err
		}
	}
	dispatched := 0
	for _, keyID := range keys {
		result, err := s.pumpKey(ctx, tx, keyID, settings, credits, jobs, count, snapshot)
		var unavailable interface{ KeyUnavailable() bool }
		if errors.As(err, &unavailable) && unavailable.KeyUnavailable() {
			continue
		}
		if err != nil {
			return 0, err
		}
		dispatched += result.total
		snapshot.node += result.total
		for worker, queued := range result.workers {
			snapshot.workers[worker] += queued
		}
	}
	return dispatched, nil
}

type inflightCounter func(context.Context, pgx.Tx, int64, string, int) (int, error)

func (s Store) pumpKey(ctx context.Context, tx pgx.Tx, keyID int64, settings app.PumpSettings, credits app.CreditProvider, jobs queue.JobQueue, count inflightCounter, snapshot inflightSnapshot) (pumpResult, error) {
	result := pumpResult{workers: make(map[string]int, 3)}
	remaining, err := credits.Capacity(ctx, keyID, 2*time.Minute)
	if err != nil {
		return result, fmt.Errorf("schedule: read key %d quota credits: %w", keyID, err)
	}
	keyInflight, err := count(ctx, tx, keyID, "", settings.PerKeyReadyCap)
	if err != nil {
		return result, err
	}
	if remaining <= 0 || keyInflight >= settings.PerKeyReadyCap || snapshot.node >= settings.NodeReadyCap {
		return result, nil
	}
	for _, cap := range configuredQueues(settings) {
		workerInflight := snapshot.workers[cap.name] + result.workers[cap.name]
		limit, err := app.Credits(app.CreditInput{
			RemainingQuota: remaining - result.total,
			PerKeyReadyCap: settings.PerKeyReadyCap,
			KeyInflight:    keyInflight + result.total,
			NodeCap:        settings.NodeReadyCap,
			NodeInflight:   snapshot.node + result.total,
			WorkerCap:      cap.cap,
			WorkerInflight: workerInflight,
		})
		if err != nil {
			return pumpResult{}, err
		}
		if limit == 0 {
			continue
		}
		queued, err := s.pumpQueue(ctx, tx, keyID, cap, limit, settings, jobs)
		if err != nil {
			return pumpResult{}, err
		}
		result.total += queued
		result.workers[cap.name] += queued
		if result.total >= remaining || keyInflight+result.total >= settings.PerKeyReadyCap || snapshot.node+result.total >= settings.NodeReadyCap {
			break
		}
	}
	return result, nil
}

const planBacklogSQL = `SELECT p.id, p.resource_share, count(backlog.work_item_id)::int
	FROM plans p
	CROSS JOIN LATERAL (
		SELECT w.id AS work_item_id
		FROM users u
		CROSS JOIN LATERAL (
			SELECT w.id
			FROM work_items w
			WHERE w.user_id=u.id AND w.state='pending' AND w.key_id=$1
				AND w.worker_type=$2 AND w.not_before<=now()
				AND NOT EXISTS (
					SELECT 1 FROM crawl_interests ci
					WHERE ci.user_id=w.user_id AND ci.person_nsid=w.subject_key
						AND ci.paused_at IS NOT NULL
				)
			ORDER BY w.not_before
			LIMIT $3
		) w
		WHERE u.plan_id=p.id
		LIMIT $3
	) backlog
	GROUP BY p.id, p.resource_share
	ORDER BY p.id`

func (s Store) pumpQueue(ctx context.Context, tx pgx.Tx, keyID int64, cap queueCap, limit int, settings app.PumpSettings, jobs queue.JobQueue) (int, error) {
	rows, err := tx.Query(ctx, planBacklogSQL, keyID, cap.name, limit)
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

const (
	keylessOwnerCursorKey     = "keyless_owner_cursor"
	keylessOwnerCandidatesSQL = `SELECT id FROM work_items
		WHERE state='pending' AND key_id IS NULL AND id > $1
		ORDER BY id LIMIT $2 FOR UPDATE SKIP LOCKED`
	keylessOwnerCandidatesBeforeSQL = `SELECT id FROM work_items
		WHERE state='pending' AND key_id IS NULL AND id <= $1
		ORDER BY id LIMIT $2 FOR UPDATE SKIP LOCKED`
)

// assignOwnerKeys gives a bounded, ordered batch of pending keyless rows the
// key of the owning user's active Flickr account. The cursor lets ownerless
// rows remain pending without starving later rows that have an account.
func assignOwnerKeys(ctx context.Context, tx pgx.Tx, batchSize int) error {
	if batchSize < 1 {
		return errors.New("schedule: key assignment batch size must be positive")
	}
	value, err := lockScheduleMetaCursor(ctx, tx, keylessOwnerCursorKey, "0")
	if err != nil {
		return err
	}
	cursor, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fmt.Errorf("schedule: parse keyless owner cursor %q: %w", value, err)
	}
	if cursor < 0 {
		return errors.New("schedule: keyless owner cursor cannot be negative")
	}
	ids, err := selectKeylessOwnerCandidates(ctx, tx, cursor, batchSize, false)
	if err != nil {
		return err
	}
	if len(ids) < batchSize && cursor > 0 {
		before, err := selectKeylessOwnerCandidates(ctx, tx, cursor, batchSize-len(ids), true)
		if err != nil {
			return err
		}
		ids = append(ids, before...)
	}
	if len(ids) == 0 {
		return storeScheduleMetaCursor(ctx, tx, keylessOwnerCursorKey, "0")
	}
	if err := storeScheduleMetaCursor(ctx, tx, keylessOwnerCursorKey, strconv.FormatInt(ids[len(ids)-1], 10)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `WITH candidates AS MATERIALIZED (
		SELECT id, user_id FROM work_items WHERE id=ANY($1::bigint[])
	), owners AS MATERIALIZED (
		SELECT DISTINCT ON (a.user_id) a.user_id, a.key_id
		FROM flickr_accounts a
		JOIN (SELECT DISTINCT user_id FROM candidates) c ON c.user_id=a.user_id
		WHERE a.status='ok'
		ORDER BY a.user_id, a.id
	)
	UPDATE work_items w SET key_id=owners.key_id, updated_at=now()
	FROM candidates JOIN owners USING (user_id)
	WHERE w.id=candidates.id AND w.state='pending' AND w.key_id IS NULL`, ids); err != nil {
		return fmt.Errorf("schedule: assign owner keys: %w", err)
	}
	return nil
}

func selectKeylessOwnerCandidates(ctx context.Context, tx pgx.Tx, cursor int64, limit int, before bool) ([]int64, error) {
	query := keylessOwnerCandidatesSQL
	if before {
		query = keylessOwnerCandidatesBeforeSQL
	}
	rows, err := tx.Query(ctx, query, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("schedule: select keyless work: %w", err)
	}
	defer rows.Close()
	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("schedule: scan keyless work: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("schedule: read keyless work: %w", err)
	}
	return ids, nil
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

const keylessWarnInterval = time.Minute

var (
	keylessWarnMu   sync.Mutex
	keylessWarnLast time.Time
)

// warnKeylessPending logs, at most once per minute, due pending rows that no
// key can dispatch.
func warnKeylessPending(ctx context.Context, tx pgx.Tx) error {
	var found bool
	if err := tx.QueryRow(ctx, keylessPendingExistsSQL).Scan(&found); err != nil {
		return fmt.Errorf("schedule: count keyless pending work: %w", err)
	}
	if !found {
		return nil
	}
	keylessWarnMu.Lock()
	defer keylessWarnMu.Unlock()
	if now := time.Now(); now.Sub(keylessWarnLast) >= keylessWarnInterval {
		keylessWarnLast = now
		slog.WarnContext(ctx, "pending work cannot be dispatched: owner has no active Flickr account key", "has_pending_without_key", true)
	}
	return nil
}

const keylessPendingExistsSQL = `SELECT EXISTS (
	SELECT 1 FROM work_items WHERE state='pending' AND key_id IS NULL AND not_before <= now()
)`

func pendingKeys(ctx context.Context, tx pgx.Tx) ([]int64, error) {
	// Active keys come from the keys table. Per-key claim uses the pending
	// score index and returns nothing when a key has no due work.
	rows, err := tx.Query(ctx, `
SELECT DISTINCT k.id
FROM api_keys k
JOIN flickr_accounts a ON a.key_id = k.id AND a.status = 'ok'
WHERE k.status = 'valid'
ORDER BY k.id`)
	if err != nil {
		return nil, fmt.Errorf("schedule: list active keys: %w", err)
	}
	defer rows.Close()
	keys := make([]int64, 0)
	for rows.Next() {
		var key int64
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("schedule: scan active key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("schedule: read active keys: %w", err)
	}
	return keys, nil
}

const (
	inflightNodeCountSQL      = `SELECT count(*) FROM (SELECT 1 FROM work_items WHERE state IN ('ready','running') LIMIT $1) AS inflight`
	inflightKeyCountSQL       = `SELECT count(*) FROM (SELECT 1 FROM work_items WHERE state IN ('ready','running') AND key_id=$1 LIMIT $2) AS inflight`
	inflightWorkerCountSQL    = `SELECT count(*) FROM (SELECT 1 FROM work_items WHERE state IN ('ready','running') AND worker_type=$1 LIMIT $2) AS inflight`
	inflightKeyWorkerCountSQL = `SELECT count(*) FROM (SELECT 1 FROM work_items WHERE state IN ('ready','running') AND key_id=$1 AND worker_type=$2 LIMIT $3) AS inflight`
)

func countInflight(ctx context.Context, tx pgx.Tx, keyID int64, worker string, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	query := inflightNodeCountSQL
	args := []any{limit}
	switch {
	case keyID != 0 && worker != "":
		query = inflightKeyWorkerCountSQL
		args = []any{keyID, worker, limit}
	case keyID != 0:
		query = inflightKeyCountSQL
		args = []any{keyID, limit}
	case worker != "":
		query = inflightWorkerCountSQL
		args = []any{worker, limit}
	}
	var count int
	err := tx.QueryRow(ctx, query, args...).Scan(&count)
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

const reclaimSelfHealSQL = `WITH candidates AS MATERIALIZED (
	SELECT id FROM work_items
	WHERE state='failed' AND last_error='lease expired' AND updated_at <= $2
	ORDER BY updated_at, id
	LIMIT $3
	FOR UPDATE SKIP LOCKED
)
UPDATE work_items AS w SET state='pending', attempts=0, not_before=$1, lease_expires_at=NULL, last_error=NULL, updated_at=$1
FROM candidates
WHERE w.id=candidates.id`

const (
	expiredLeaseCursorKey   = "reclaim_expired_lease_cursor"
	reclaimExpiredLeasesSQL = `WITH candidates AS MATERIALIZED (
		SELECT w.id, w.attempts, w.kind, w.lease_expires_at
		FROM work_items w
		WHERE w.state IN ('ready','running') AND w.lease_expires_at <= $1
			AND (w.lease_expires_at,w.id) > (COALESCE($2::timestamptz,'-infinity'::timestamptz),COALESCE($3::bigint,0))
		ORDER BY w.lease_expires_at,w.id
		LIMIT $4
		FOR UPDATE OF w SKIP LOCKED
	)
	SELECT c.id,c.attempts,c.kind,c.lease_expires_at,
		NOT EXISTS (SELECT 1 FROM river_job j WHERE j.state IN ` + liveRiverStates + `
			AND j.args->'args'->>'work_item_id'=c.id::text) AS should_reap
	FROM candidates c
	ORDER BY c.lease_expires_at,c.id`
	reclaimExpiredLeasesBeforeSQL = `WITH candidates AS MATERIALIZED (
		SELECT w.id, w.attempts, w.kind, w.lease_expires_at
		FROM work_items w
		WHERE w.state IN ('ready','running') AND w.lease_expires_at <= $1
			AND (w.lease_expires_at,w.id) <= ($2::timestamptz,$3::bigint)
		ORDER BY w.lease_expires_at,w.id
		LIMIT $4
		FOR UPDATE OF w SKIP LOCKED
	)
	SELECT c.id,c.attempts,c.kind,c.lease_expires_at,
		NOT EXISTS (SELECT 1 FROM river_job j WHERE j.state IN ` + liveRiverStates + `
			AND j.args->'args'->>'work_item_id'=c.id::text) AS should_reap
	FROM candidates c
	ORDER BY c.lease_expires_at,c.id`
)

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

// Reclaim processes one bounded batch of expired leases and self-heal failures.
// Expired leases without a live River job return to pending with exponential
// delay based on the already-incremented attempt.
func (s Store) Reclaim(ctx context.Context, retryMax int, base time.Duration, batchSize int, now time.Time) (int, error) {
	db := s.backend()
	if db == nil || batchSize < 1 {
		return 0, errors.New("schedule: reclaim database and positive batch size are required")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("schedule: begin reclaim: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	candidates, err := nextExpiredLeaseBatch(ctx, tx, now, batchSize)
	if err != nil {
		return 0, err
	}
	expiredRows := make([]expiredLease, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.shouldReap {
			expiredRows = append(expiredRows, candidate.lease)
		}
	}
	if err := applyExpiredLeases(ctx, tx, expiredRows, retryMax, base, now); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, reclaimSelfHealSQL, now, now.Add(-SelfHealAfter), batchSize); err != nil {
		return 0, fmt.Errorf("schedule: self-heal lease-expired work: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("schedule: commit reclaim: %w", err)
	}
	return len(expiredRows), nil
}

func nextExpiredLeaseBatch(ctx context.Context, tx pgx.Tx, now time.Time, batchSize int) ([]expiredLeaseCandidate, error) {
	cursorValue, err := lockScheduleMetaCursor(ctx, tx, expiredLeaseCursorKey, "")
	if err != nil {
		return nil, err
	}
	cursor, err := parseExpiredLeaseCursor(cursorValue)
	if err != nil {
		return nil, err
	}
	candidates, err := selectExpiredLeaseCandidates(ctx, tx, now, cursor, batchSize, false)
	if err != nil {
		return nil, err
	}
	if len(candidates) < batchSize && cursor.ID > 0 {
		before, err := selectExpiredLeaseCandidates(ctx, tx, now, cursor, batchSize-len(candidates), true)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, before...)
	}
	if len(candidates) == 0 {
		if err := storeScheduleMetaCursor(ctx, tx, expiredLeaseCursorKey, ""); err != nil {
			return nil, err
		}
	} else {
		last := candidates[len(candidates)-1]
		value, err := json.Marshal(expiredLeaseCursor{ExpiresAt: last.expiresAt, ID: last.lease.id})
		if err != nil {
			return nil, fmt.Errorf("schedule: encode expired lease cursor: %w", err)
		}
		if err := storeScheduleMetaCursor(ctx, tx, expiredLeaseCursorKey, string(value)); err != nil {
			return nil, err
		}
	}
	return candidates, nil
}

func parseExpiredLeaseCursor(value string) (expiredLeaseCursor, error) {
	if value == "" {
		return expiredLeaseCursor{}, nil
	}
	var cursor expiredLeaseCursor
	if err := json.Unmarshal([]byte(value), &cursor); err != nil {
		return expiredLeaseCursor{}, fmt.Errorf("schedule: parse expired lease cursor: %w", err)
	}
	if cursor.ID <= 0 || cursor.ExpiresAt.IsZero() {
		return expiredLeaseCursor{}, errors.New("schedule: expired lease cursor is invalid")
	}
	return cursor, nil
}

func selectExpiredLeaseCandidates(ctx context.Context, tx pgx.Tx, now time.Time, cursor expiredLeaseCursor, limit int, before bool) ([]expiredLeaseCandidate, error) {
	query := reclaimExpiredLeasesSQL
	var cursorTime any
	var cursorID any
	if !cursor.ExpiresAt.IsZero() {
		cursorTime = cursor.ExpiresAt
		cursorID = cursor.ID
	}
	if before {
		query = reclaimExpiredLeasesBeforeSQL
	}
	rows, err := tx.Query(ctx, query, now, cursorTime, cursorID, limit)
	if err != nil {
		return nil, fmt.Errorf("schedule: select expired lease candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]expiredLeaseCandidate, 0, limit)
	for rows.Next() {
		var candidate expiredLeaseCandidate
		if err := rows.Scan(&candidate.lease.id, &candidate.lease.attempts, &candidate.lease.kind, &candidate.expiresAt, &candidate.shouldReap); err != nil {
			return nil, fmt.Errorf("schedule: scan expired lease candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("schedule: read expired lease candidates: %w", err)
	}
	return candidates, nil
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
