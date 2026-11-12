package river

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/platform/queue"
	"github.com/jooservices/xflickr/internal/schedule/app"
)

const pumpLockID int64 = 113

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
		result, err := s.pumpKey(ctx, tx, keyID, pumpPass{pumpEnv: pumpEnv{settings: settings, jobs: jobs}, credits: credits, count: count, snapshot: snapshot})
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

// pumpEnv is the configuration and job queue shared by one pump pass.
type pumpEnv struct {
	settings app.PumpSettings
	jobs     queue.JobQueue
}

// pumpPass is what one key needs from its pump pass: the shared environment,
// the credit provider, the in-flight counter and the node and worker snapshot.
type pumpPass struct {
	pumpEnv
	credits  app.CreditProvider
	count    inflightCounter
	snapshot inflightSnapshot
}

// queuePlan is one plan's allocation of work items on one queue for a key.
type queuePlan struct {
	keyID  int64
	cap    queueCap
	planID int64
	limit  int
}

type inflightCounter func(context.Context, pgx.Tx, int64, string, int) (int, error)

// keyState is one key's pump state between its credit check and its queues.
type keyState struct {
	keyID       int64
	remaining   int
	keyInflight int
	result      pumpResult
}

func (s Store) pumpKey(ctx context.Context, tx pgx.Tx, keyID int64, pass pumpPass) (pumpResult, error) {
	result := pumpResult{workers: make(map[string]int, 3)}
	remaining, err := pass.credits.Capacity(ctx, keyID, 2*time.Minute)
	if err != nil {
		return result, fmt.Errorf("schedule: read key %d quota credits: %w", keyID, err)
	}
	keyInflight, err := pass.count(ctx, tx, keyID, "", pass.settings.PerKeyReadyCap)
	if err != nil {
		return result, err
	}
	if remaining <= 0 || keyInflight >= pass.settings.PerKeyReadyCap || pass.snapshot.node >= pass.settings.NodeReadyCap {
		return result, nil
	}
	state := keyState{keyID: keyID, remaining: remaining, keyInflight: keyInflight, result: result}
	if err := s.pumpKeyQueues(ctx, tx, pass, &state); err != nil {
		return pumpResult{}, err
	}
	return state.result, nil
}

// pumpKeyQueues dispatches one key's work queue by queue until the key, node
// or credit limit is reached.
func (s Store) pumpKeyQueues(ctx context.Context, tx pgx.Tx, pass pumpPass, state *keyState) error {
	for _, cap := range configuredQueues(pass.settings) {
		workerInflight := pass.snapshot.workers[cap.name] + state.result.workers[cap.name]
		limit, err := app.Credits(app.CreditInput{
			RemainingQuota: state.remaining - state.result.total,
			PerKeyReadyCap: pass.settings.PerKeyReadyCap,
			KeyInflight:    state.keyInflight + state.result.total,
			NodeCap:        pass.settings.NodeReadyCap,
			NodeInflight:   pass.snapshot.node + state.result.total,
			WorkerCap:      cap.cap,
			WorkerInflight: workerInflight,
		})
		if err != nil {
			return err
		}
		if limit == 0 {
			continue
		}
		queued, err := s.pumpQueue(ctx, tx, state.keyID, cap, limit, pass.settings, pass.jobs)
		if err != nil {
			return err
		}
		state.result.total += queued
		state.result.workers[cap.name] += queued
		if state.result.total >= state.remaining || state.keyInflight+state.result.total >= pass.settings.PerKeyReadyCap || pass.snapshot.node+state.result.total >= pass.settings.NodeReadyCap {
			break
		}
	}
	return nil
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
		queued, err := s.pumpQueuePlan(ctx, tx, queuePlan{keyID: keyID, cap: cap, planID: group.ID, limit: allocation}, pumpEnv{settings: settings, jobs: jobs})
		if err != nil {
			return 0, err
		}
		total += queued
	}
	return total, nil
}

func (s Store) pumpQueuePlan(ctx context.Context, tx pgx.Tx, plan queuePlan, env pumpEnv) (int, error) {
	rows, err := tx.Query(ctx, `SELECT w.id, w.kind, w.subject_key, w.cursor FROM work_items w LEFT JOIN users u ON u.id=w.user_id WHERE w.state = 'pending' AND w.key_id = $1 AND w.worker_type = $2 AND w.not_before <= now() AND COALESCE(u.plan_id,(SELECT id FROM plans WHERE name='free'))=$3 AND NOT EXISTS (SELECT 1 FROM crawl_interests ci WHERE ci.user_id=w.user_id AND ci.person_nsid=w.subject_key AND ci.paused_at IS NOT NULL) ORDER BY w.score DESC, w.not_before, w.id LIMIT $4 FOR UPDATE OF w SKIP LOCKED`, plan.keyID, plan.cap.name, plan.planID, plan.limit)
	if err != nil {
		return 0, fmt.Errorf("schedule: select pending work: %w", err)
	}
	batch := make([]workRow, 0, plan.limit)
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
		if err := env.jobs.Enqueue(ctx, tx, queue.Job{Kind: row.Kind, Args: args}, queue.Options{Queue: plan.cap.name, MaxAttempts: max(1, env.settings.RetryMax), LeaseTimeout: env.settings.LeaseTimeout, Backoff: env.settings.RetryBackoff}); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `UPDATE work_items SET state='ready', lease_expires_at=now()+$2::bigint * interval '1 microsecond', updated_at=now() WHERE id=$1 AND state='pending'`, row.ID, env.settings.LeaseTimeout.Microseconds()); err != nil {
			return 0, fmt.Errorf("schedule: mark work ready: %w", err)
		}
	}
	return len(batch), nil
}

func configuredQueues(settings app.PumpSettings) []queueCap {
	return []queueCap{{"media", settings.MediaReadyCap}, {"ai", settings.AIReadyCap}, {"sync", settings.SyncReadyCap}}
}

const (
	inflightNodeIDsSQL      = `SELECT id FROM work_items WHERE state IN ('ready','running') LIMIT $1`
	inflightKeyIDsSQL       = `SELECT id FROM work_items WHERE state IN ('ready','running') AND key_id=$1 LIMIT $2`
	inflightWorkerIDsSQL    = `SELECT id FROM work_items WHERE state IN ('ready','running') AND worker_type=$1 LIMIT $2`
	inflightKeyWorkerIDsSQL = `SELECT id FROM work_items WHERE state IN ('ready','running') AND key_id=$1 AND worker_type=$2 LIMIT $3`
)

func countInflight(ctx context.Context, tx pgx.Tx, keyID int64, worker string, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	query := inflightNodeIDsSQL
	args := []any{limit}
	switch {
	case keyID != 0 && worker != "":
		query = inflightKeyWorkerIDsSQL
		args = []any{keyID, worker, limit}
	case keyID != 0:
		query = inflightKeyIDsSQL
		args = []any{keyID, limit}
	case worker != "":
		query = inflightWorkerIDsSQL
		args = []any{worker, limit}
	}
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("schedule: count inflight work: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, fmt.Errorf("schedule: count inflight work: %w", err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("schedule: count inflight work: %w", err)
	}
	return count, nil
}
