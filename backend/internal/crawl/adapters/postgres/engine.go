package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
	_ app.Store      = (*Engine)(nil)
	_ app.UnitOfWork = (*Engine)(nil)
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

// Start marks a ready item running. A running item stays running, so a rerun
// after a crash proceeds; any other state is not runnable.
func (e *Engine) Start(ctx context.Context, id int64) (bool, error) {
	tag, err := e.conn.Exec(ctx, `UPDATE work_items SET state = 'running', updated_at = now() WHERE id = $1 AND state IN ('ready', 'running')`, id)
	if err != nil {
		return false, fmt.Errorf("crawl: start work item: %w", err)
	}
	return tag.RowsAffected() > 0, nil
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

const finishSQL = `UPDATE work_items SET state = 'done', last_error = NULL, lease_expires_at = NULL, updated_at = now() WHERE id = $1 AND state = 'running'`

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
		return fn(app.Writers{Work: workTx{tx}, Catalog: e.catalog(tx), Interests: interestTx{tx}, Observations: observationTx{tx}})
	})
}

type workTx struct{ tx Conn }

func (w workTx) Lock(ctx context.Context, id int64) (domain.Item, error) {
	return scanItem(w.tx.QueryRow(ctx, `SELECT `+itemColumns+` FROM work_items WHERE id = $1 FOR UPDATE`, id))
}

func (w workTx) Enqueue(ctx context.Context, work app.NewWork) error {
	cursor, err := json.Marshal(work.Cursor)
	if err != nil {
		return fmt.Errorf("crawl: encode cursor: %w", err)
	}
	var userID any
	if work.UserID > 0 {
		userID = work.UserID
	}
	_, err = w.tx.Exec(ctx, `INSERT INTO work_items (kind, subject_key, user_id, depth, cursor) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT ON CONSTRAINT work_items_dedupe_key DO NOTHING`, work.Kind, work.Subject, userID, work.Depth, cursor)
	if err != nil {
		return fmt.Errorf("crawl: enqueue work item: %w", err)
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
	if _, err := w.tx.Exec(ctx, finishSQL, id); err != nil {
		return fmt.Errorf("crawl: finish work item: %w", err)
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
