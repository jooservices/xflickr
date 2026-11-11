package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/crawl/app"
	"github.com/jooservices/xflickr/internal/crawl/domain"
	"github.com/jooservices/xflickr/internal/platform/db"
)

// TopicCrawlStopped is published when a bad credential stops queued work.
const TopicCrawlStopped = "crawl.stopped"

// Conn is the statement surface shared by a pool and a transaction.
type Conn interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	SendBatch(context.Context, *pgx.Batch) pgx.BatchResults
}

// CatalogFactory binds a catalog writer to the commit transaction. The wiring
// layer supplies it, so this adapter never imports the catalog adapters.
type CatalogFactory func(Conn) app.CatalogWriter

// txRunner runs fn in one atomic transaction.
type txRunner func(ctx context.Context, fn func(Conn) error) error

// Engine is the PostgreSQL store and unit of work behind the crawl engine.
type Engine struct {
	conn    Conn
	run     txRunner
	catalog CatalogFactory
}

var (
	_ app.Store         = (*Engine)(nil)
	_ app.UnitOfWork    = (*Engine)(nil)
	_ app.GroupPoolGate = (*Engine)(nil)
)

// NewEngine creates the engine adapter over pool.
func NewEngine(pool *pgxpool.Pool, catalog CatalogFactory) (*Engine, error) {
	if pool == nil {
		return nil, errDatabaseRequired
	}
	if catalog == nil {
		return nil, errors.New("crawl: catalog writer factory is required")
	}
	uow := db.NewUnitOfWork(pool)
	run := func(ctx context.Context, fn func(Conn) error) error {
		return uow.Do(ctx, func(tx db.Tx) error { return fn(tx) })
	}
	return &Engine{conn: pool, run: run, catalog: catalog}, nil
}

const itemColumns = `id, kind, subject_key, COALESCE(user_id, 0), depth, state, attempts, cursor`

type itemRow interface{ Scan(...any) error }

func scanItem(row itemRow) (domain.Item, error) {
	var item domain.Item
	var raw []byte
	if err := row.Scan(&item.ID, &item.Kind, &item.Subject, &item.UserID, &item.Depth, &item.State, &item.Attempts, &raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Item{}, app.ErrItemNotFound
		}
		return domain.Item{}, fmt.Errorf("crawl: read work item: %w", err)
	}
	cursor, err := domain.ParseCursor(raw)
	if err != nil {
		return domain.Item{}, fmt.Errorf("crawl: work item %d: %w", item.ID, err)
	}
	item.Cursor = cursor
	return item, nil
}

// Load reads a work item.
func (e *Engine) Load(ctx context.Context, id int64) (domain.Item, error) {
	return scanItem(e.conn.QueryRow(ctx, `SELECT `+itemColumns+` FROM work_items WHERE id = $1`, id))
}

// Enabled reports whether a user explicitly enabled crawling this contact's group pool.
func (e *Engine) Enabled(ctx context.Context, userID int64, personNSID, groupNSID string) (bool, error) {
	var enabled bool
	err := e.conn.QueryRow(ctx, `SELECT enabled FROM contact_group_crawl_preferences WHERE user_id=$1 AND person_nsid=$2 AND group_nsid=$3`, userID, personNSID, groupNSID).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("crawl: read group pool preference: %w", err)
	}
	return enabled, nil
}

// Start marks a ready item running. A running item stays running, so a rerun
// after a crash proceeds; any other state is not runnable.
func (e *Engine) Start(ctx context.Context, id int64) (bool, error) {
	var started bool
	if err := e.conn.QueryRow(ctx, startSQL, id).Scan(&started); err != nil {
		return false, fmt.Errorf("crawl: start work item: %w", err)
	}
	return started, nil
}

const startSQL = `WITH target AS MATERIALIZED (
    SELECT id, state
    FROM work_items
    WHERE id = $1 AND state IN ('ready', 'running')
    FOR UPDATE
),
changed AS (
    UPDATE work_items AS w
    SET state = 'running', updated_at = now()
    FROM target AS t
    WHERE w.id = t.id AND t.state = 'ready'
    RETURNING w.id
)
SELECT EXISTS (SELECT 1 FROM target)`

// StampScanStart writes checkpoint_at onto the unchanged photos/favorites
// cursor when it is still missing. A legacy later-page cursor restarts at page
// one and drops its opaque continuation before the first request.
func (e *Engine) StampScanStart(ctx context.Context, id int64, at int64, expected domain.Cursor) error {
	if at <= 0 {
		return nil
	}
	params := expected.Params
	if params == nil {
		params = map[string]string{}
	}
	encodedParams, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("crawl: encode scan-start cursor: %w", err)
	}
	_, err = e.conn.Exec(ctx, `
		UPDATE work_items SET cursor = jsonb_set(
			CASE WHEN COALESCE((cursor->>'page')::integer, 1) > 1
				THEN jsonb_set(cursor - 'continuation', '{page}', '1'::jsonb, true)
				ELSE cursor
			END,
			'{checkpoint_at}', to_jsonb($2::bigint), true
		), updated_at = now()
		WHERE id = $1
		  AND kind IN ('photos', 'favorites')
		  AND state = 'running'
		  AND COALESCE((cursor->>'checkpoint_at')::bigint, 0) <= 0
		  AND cursor->>'method' = $3
		  AND COALESCE((cursor->>'page')::integer, 1) = $4
		  AND COALESCE(cursor->>'continuation', '') = $5
		  AND COALESCE(cursor->'params', '{}'::jsonb) = $6::jsonb`,
		id, at, expected.Method, expected.Page, expected.Continuation, encodedParams)
	if err != nil {
		return fmt.Errorf("crawl: stamp scan-start checkpoint: %w", err)
	}
	return nil
}

// Defer returns a running item to pending until deferral.NotBefore.
func (e *Engine) Defer(ctx context.Context, id int64, deferral app.Deferral) error {
	consumed := 0
	if deferral.ConsumeAttempt {
		consumed = 1
	}
	_, err := e.conn.Exec(ctx, `UPDATE work_items SET state = 'pending', not_before = $2, attempts = attempts + $3, last_error = $4, lease_expires_at = NULL, updated_at = now() WHERE id = $1 AND state = 'running'`,
		id, deferral.NotBefore, consumed, deferral.Reason)
	if err != nil {
		return fmt.Errorf("crawl: defer work item: %w", err)
	}
	return nil
}

// Finish marks a running item done.
func (e *Engine) Finish(ctx context.Context, id int64) error {
	_, err := e.conn.Exec(ctx, finishSQL, id)
	if err != nil {
		return fmt.Errorf("crawl: finish work item: %w", err)
	}
	return nil
}

// Fail marks a running item failed.
func (e *Engine) Fail(ctx context.Context, id int64, reason string) error {
	_, err := e.conn.Exec(ctx, `UPDATE work_items SET state = 'failed', last_error = $2, lease_expires_at = NULL, updated_at = now() WHERE id = $1 AND state IN ('ready', 'running')`, id, reason)
	if err != nil {
		return fmt.Errorf("crawl: fail work item: %w", err)
	}
	return nil
}

const finishSQL = `UPDATE work_items SET state = 'done', cursor = cursor - 'scan_completed_at', last_error = NULL, lease_expires_at = NULL, updated_at = now() WHERE id = $1 AND state = 'running'`

const finishScanSQL = `UPDATE work_items
SET state = 'done',
    cursor = CASE
        WHEN kind IN ('photos', 'favorites') AND COALESCE((cursor->>'checkpoint_at')::bigint, 0) > 0
            THEN jsonb_set(cursor, '{scan_completed_at}', to_jsonb(floor(extract(epoch FROM clock_timestamp()))::bigint), true)
        ELSE cursor - 'scan_completed_at'
    END,
    last_error = NULL, lease_expires_at = NULL, updated_at = now()
WHERE id = $1 AND state = 'running'
RETURNING kind, subject_key, COALESCE(user_id,0), COALESCE((cursor->>'checkpoint_at')::bigint,0)`

// Stop fails every active item of the user (revoked token) or key (invalid
// key) and publishes crawl.stopped in the same transaction.
func (e *Engine) Stop(ctx context.Context, request app.StopRequest) error {
	column, owner := "user_id", request.UserID
	if request.Reason == app.StopInvalidKey {
		column, owner = "key_id", request.KeyID
	}
	if owner <= 0 {
		return e.Fail(ctx, request.ItemID, string(request.Reason))
	}
	return e.run(ctx, func(tx Conn) error {
		tag, err := tx.Exec(ctx, `UPDATE work_items SET state = 'failed', last_error = $2, lease_expires_at = NULL, updated_at = now() WHERE `+column+` = $1 AND state IN ('pending', 'ready', 'running', 'waiting')`, owner, string(request.Reason))
		if err != nil {
			return fmt.Errorf("crawl: stop work items: %w", err)
		}
		payload, err := json.Marshal(map[string]any{
			"user_id": request.UserID, "key_id": request.KeyID, "reason": request.Reason, "stopped": tag.RowsAffected(),
		})
		if err != nil {
			return fmt.Errorf("crawl: encode stop event: %w", err)
		}
		// Same row the outbox package writes; Conn cannot be passed to it
		// without a platform-to-adapter dependency.
		if _, err := tx.Exec(ctx, `INSERT INTO outbox_events (topic, key, payload) VALUES ($1, $2, $3)`,
			TopicCrawlStopped, fmt.Sprintf("%s:%d", column, owner), payload); err != nil {
			return fmt.Errorf("crawl: publish stop event: %w", err)
		}
		return nil
	})
}

// Do runs fn in one serializable transaction.
func (e *Engine) Do(ctx context.Context, fn func(app.Writers) error) error {
	return e.run(ctx, func(tx Conn) error {
		return fn(app.Writers{
			Work: workTx{tx}, Catalog: e.catalog(tx), Interests: interestTx{tx},
			Observations: observationTx{tx}, Reconcile: reconcileTx{tx}, Progress: contactProgressTx{tx},
		})
	})
}

type reconcileTx struct{ tx Conn }

func (r reconcileTx) Complete(ctx context.Context, userID int64, person string, scannedAt, completedAt time.Time) error {
	if userID <= 0 || person == "" || scannedAt.IsZero() || completedAt.IsZero() {
		return errors.New("crawl: invalid reconciliation completion")
	}
	var exists int
	err := r.tx.QueryRow(ctx, `SELECT 1 FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2 FOR UPDATE`, userID, person).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		// The interest may have been removed while its durable work item was
		// running. Do not apply a catalog diff for a scope that no longer exists.
		return nil
	}
	if err != nil {
		return fmt.Errorf("crawl: lock reconciliation interest: %w", err)
	}
	tombstoned, err := r.tx.Exec(ctx, `
		UPDATE photos p SET tombstoned_at=$3
		WHERE p.owner_nsid=$1 AND p.tombstoned_at IS NULL AND p.last_seen_at<$2
		  AND (p.visibility='public' OR EXISTS (
			SELECT 1 FROM flickr_accounts fa
			WHERE fa.user_id=$4 AND fa.nsid=$1 AND fa.status='ok'
			))`, person, scannedAt, completedAt, userID)
	if err != nil {
		return fmt.Errorf("crawl: tombstone photos missing from full list: %w", err)
	}
	removedFavorites, err := r.tx.Exec(ctx, `
		UPDATE favorites f SET removed_at=$3
		WHERE f.person_nsid=$1 AND f.removed_at IS NULL AND f.seen_at<$2
		  AND EXISTS (
			SELECT 1 FROM flickr_accounts fa
			WHERE fa.user_id=$4 AND fa.nsid=$1 AND fa.status='ok'
			)`, person, scannedAt, completedAt, userID)
	if err != nil {
		return fmt.Errorf("crawl: mark favorites missing from full list: %w", err)
	}
	if _, err := r.tx.Exec(ctx, `UPDATE crawl_interests SET tombstones_count=tombstones_count+$3,
		favorites_count=GREATEST(0,favorites_count-$4),last_reconciled_at=$5
		WHERE user_id=$1 AND person_nsid=$2`, userID, person, tombstoned.RowsAffected(), removedFavorites.RowsAffected(), completedAt); err != nil {
		return fmt.Errorf("crawl: record completed reconciliation: %w", err)
	}
	return nil
}

type workTx struct{ tx Conn }

func (w workTx) Lock(ctx context.Context, id int64) (domain.Item, error) {
	return scanItem(w.tx.QueryRow(ctx, `SELECT `+itemColumns+` FROM work_items WHERE id = $1 FOR UPDATE`, id))
}

func (w workTx) Enqueue(ctx context.Context, work app.NewWork) error {
	if work.Kind == "check" && work.UserID > 0 {
		var lastSeen, lastFavorite int64
		err := w.tx.QueryRow(ctx, `SELECT
			COALESCE(floor(extract(epoch FROM last_seen_at))::bigint, 0),
			COALESCE(floor(extract(epoch FROM last_fave_at))::bigint, 0)
			FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2`, work.UserID, work.Subject).Scan(&lastSeen, &lastFavorite)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("crawl: read check checkpoints: %w", err)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			// Contacts photos can include people outside this user's crawl scope.
			// Never create uncapped incremental work for an unknown interest.
			return nil
		}
		if err == nil {
			if work.Cursor.Params == nil {
				work.Cursor.Params = make(map[string]string)
			}
			work.Cursor.Params["user_id"] = work.Subject
			work.Cursor.Params["min_upload_date"] = fmt.Sprint(lastSeen)
			work.Cursor.Params["min_fave_date"] = fmt.Sprint(lastFavorite)
			work.Cursor.Params["per_page"] = "1"
		}
	}
	cursor, err := json.Marshal(work.Cursor)
	if err != nil {
		return fmt.Errorf("crawl: encode cursor: %w", err)
	}
	var userID any
	if work.UserID > 0 {
		userID = work.UserID
	}
	query := `INSERT INTO work_items (kind, subject_key, user_id, key_id, depth, cursor, starred, score)
		SELECT $1, $2, $3, (SELECT fa.key_id FROM flickr_accounts fa WHERE fa.user_id=$3 AND fa.status='ok' ORDER BY fa.id LIMIT 1), $4, $5,
			COALESCE((SELECT ci.starred FROM crawl_interests ci WHERE ci.user_id=$3 AND ci.person_nsid=$2), FALSE),
			work_items_static_score(
				COALESCE((SELECT p.weight FROM users u LEFT JOIN plans p ON p.id=u.plan_id WHERE u.id=$3), 1),
				FALSE,
				COALESCE((SELECT ci.starred FROM crawl_interests ci WHERE ci.user_id=$3 AND ci.person_nsid=$2), FALSE),
				$4, now())
		ON CONFLICT ON CONSTRAINT work_items_dedupe_key DO NOTHING`
	if work.Reopen {
		query = `INSERT INTO work_items (kind, subject_key, user_id, key_id, depth, cursor, starred, score)
			SELECT $1, $2, $3, (SELECT fa.key_id FROM flickr_accounts fa WHERE fa.user_id=$3 AND fa.status='ok' ORDER BY fa.id LIMIT 1), $4, $5,
				COALESCE((SELECT ci.starred FROM crawl_interests ci WHERE ci.user_id=$3 AND ci.person_nsid=$2), FALSE),
				work_items_static_score(
					COALESCE((SELECT p.weight FROM users u LEFT JOIN plans p ON p.id=u.plan_id WHERE u.id=$3), 1),
					FALSE,
					COALESCE((SELECT ci.starred FROM crawl_interests ci WHERE ci.user_id=$3 AND ci.person_nsid=$2), FALSE),
					$4, now())
			ON CONFLICT ON CONSTRAINT work_items_dedupe_key DO UPDATE SET
				key_id=COALESCE(EXCLUDED.key_id, work_items.key_id), state='pending', cursor=EXCLUDED.cursor, attempts=0, last_error=NULL,
				not_before=now(), lease_expires_at=NULL, starred=EXCLUDED.starred, score=EXCLUDED.score, created_at=now(), updated_at=now()
			WHERE work_items.state IN ('done','failed')`
	}
	_, err = w.tx.Exec(ctx, query, work.Kind, work.Subject, userID, work.Depth, cursor)
	if err != nil {
		return fmt.Errorf("crawl: enqueue work item: %w", err)
	}
	if work.Kind == "check" && work.UserID > 0 {
		if _, err := w.tx.Exec(ctx, `UPDATE crawl_interests SET last_checked_at=now() WHERE user_id=$1 AND person_nsid=$2`, work.UserID, work.Subject); err != nil {
			return fmt.Errorf("crawl: mark change check queued: %w", err)
		}
	}
	return nil
}

func (w workTx) Advance(ctx context.Context, id int64, cursor domain.Cursor) error {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return fmt.Errorf("crawl: encode cursor: %w", err)
	}
	_, err = w.tx.Exec(ctx, `UPDATE work_items SET cursor = $2, state = 'pending', attempts = 0, last_error = NULL, lease_expires_at = NULL, updated_at = now() WHERE id = $1 AND state = 'running'`, id, encoded)
	if err != nil {
		return fmt.Errorf("crawl: advance work item: %w", err)
	}
	return nil
}

func (w workTx) Finish(ctx context.Context, id int64) error {
	var kind, subject string
	var userID, scanStart int64
	err := w.tx.QueryRow(ctx, finishScanSQL, id).Scan(&kind, &subject, &userID, &scanStart)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("crawl: finish work item: %w", err)
	}
	if !domain.NeedsScanStartCheckpoint(kind) || userID <= 0 || subject == "" {
		return nil
	}
	var oldUnix int64
	if kind == domain.FacetFavorites {
		err = w.tx.QueryRow(ctx, `
			SELECT COALESCE(floor(extract(epoch FROM last_fave_at))::bigint, 0)
			FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2 FOR UPDATE`, userID, subject).Scan(&oldUnix)
	} else {
		err = w.tx.QueryRow(ctx, `
			SELECT COALESCE(floor(extract(epoch FROM last_seen_at))::bigint, 0)
			FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2 FOR UPDATE`, userID, subject).Scan(&oldUnix)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("crawl: read interest checkpoint: %w", err)
	}
	next := domain.NextCheckpoint(domain.ScanCheckpoint(scanStart), true, domain.ScanCheckpoint(oldUnix))
	if next == domain.ScanCheckpoint(oldUnix) {
		return nil
	}
	if kind == domain.FacetFavorites {
		_, err = w.tx.Exec(ctx, `UPDATE crawl_interests SET last_fave_at = to_timestamp($3) WHERE user_id=$1 AND person_nsid=$2`,
			userID, subject, next.Unix())
	} else {
		_, err = w.tx.Exec(ctx, `UPDATE crawl_interests SET last_seen_at = to_timestamp($3) WHERE user_id=$1 AND person_nsid=$2`,
			userID, subject, next.Unix())
	}
	if err != nil {
		return fmt.Errorf("crawl: store scan checkpoint: %w", err)
	}
	return nil
}

type interestTx struct{ tx Conn }

func (i interestTx) Add(ctx context.Context, interest domain.Interest) error {
	if err := interest.Validate(); err != nil {
		return err
	}
	_, err := i.tx.Exec(ctx, `
		INSERT INTO crawl_interests (user_id, person_nsid, depth, via)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, person_nsid) DO UPDATE SET
			depth = LEAST(crawl_interests.depth, EXCLUDED.depth),
			via = CASE WHEN EXCLUDED.depth < crawl_interests.depth THEN EXCLUDED.via ELSE crawl_interests.via END`,
		interest.UserID, strings.TrimSpace(interest.PersonNSID), interest.Depth, strings.TrimSpace(interest.Via))
	if err != nil {
		return fmt.Errorf("crawl: add discovered interest: %w", err)
	}
	return nil
}

type observationTx struct{ tx Conn }

func (o observationTx) Record(ctx context.Context, itemID int64, page int, observations []domain.Observation) error {
	for _, observation := range observations {
		data := observation.Data
		if len(data) == 0 {
			data = json.RawMessage(`{}`)
		}
		if _, err := o.tx.Exec(ctx, `INSERT INTO crawl_observations (work_item_id, page, kind, subject, data) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT ON CONSTRAINT crawl_observations_page_key DO NOTHING`, itemID, page, observation.Kind, observation.Subject, data); err != nil {
			return fmt.Errorf("crawl: record observation: %w", err)
		}
	}
	return nil
}
