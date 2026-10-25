package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/crawl/app"
)

// CycleRepository persists the per-user cycle cursor and check scheduling.
type CycleRepository struct {
	pool  *pgxpool.Pool
	begin cycleBegin
	users cycleUserQuery
}

type cycleRows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}

type cycleUserQuery func(context.Context) (cycleRows, error)

type cycleTx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Commit(context.Context) error
	Rollback(context.Context) error
}

type cycleBegin func(context.Context) (cycleTx, error)

var _ app.CycleStore = (*CycleRepository)(nil)

// NewCycleRepository creates a cycle repository backed by PostgreSQL.
func NewCycleRepository(pool *pgxpool.Pool) (*CycleRepository, error) {
	if pool == nil {
		return nil, errDatabaseRequired
	}
	return &CycleRepository{pool: pool, begin: func(ctx context.Context) (cycleTx, error) {
		return pool.BeginTx(ctx, pgx.TxOptions{})
	}, users: func(ctx context.Context) (cycleRows, error) {
		return pool.Query(ctx, `SELECT DISTINCT user_id FROM crawl_interests ORDER BY user_id`)
	}}, nil
}

// UsersWithInterests lists users whose cycle has at least one person.
func (repository *CycleRepository) UsersWithInterests(ctx context.Context) ([]int64, error) {
	if repository == nil || repository.users == nil {
		return nil, errDatabaseRequired
	}
	if ctx == nil {
		return nil, errContextRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := repository.users(ctx)
	if err != nil {
		return nil, fmt.Errorf("crawl: list cycle users: %w", err)
	}
	defer rows.Close()
	users := make([]int64, 0)
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return nil, fmt.Errorf("crawl: scan cycle user: %w", err)
		}
		users = append(users, userID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("crawl: list cycle users: %w", err)
	}
	return users, nil
}

// ScheduleNext atomically picks the next due person after the stored cursor,
// queues a check item, marks that interest checked, and persists the cursor.
// It wraps to the first due person only after reaching the end of the cycle.
func (repository *CycleRepository) ScheduleNext(ctx context.Context, userID int64, now time.Time, interval time.Duration) (bool, error) {
	if err := validateCycleSchedule(ctx, repository, userID, interval); err != nil {
		return false, err
	}
	tx, err := repository.begin(ctx)
	if err != nil {
		return false, fmt.Errorf("crawl: begin cycle scheduling: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	cursor, startedAt, err := loadCycleCursor(ctx, tx, userID)
	if err != nil {
		return false, err
	}
	candidate, found, err := selectCycleCandidate(ctx, tx, userID, cursor, now.Add(-interval))
	if err != nil || !found {
		return false, err
	}
	candidate.cycleDue = app.CycleDue(startedAt, now, interval)
	if !candidate.due {
		return false, commitCycleVisit(ctx, tx, userID, now, candidate)
	}
	enqueued, err := enqueueCycleCheck(ctx, tx, userID, now, candidate)
	if err != nil {
		return false, err
	}
	if !enqueued {
		// An existing pending or running check already represents this visit.
		// Move on without refreshing last_checked_at or holding the cursor here.
		candidate.due = false
		return false, commitCycleVisit(ctx, tx, userID, now, candidate)
	}
	if err := advanceCycle(ctx, tx, userID, now, candidate); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("crawl: commit cycle scheduling: %w", err)
	}
	return true, nil
}

func validateCycleSchedule(ctx context.Context, repository *CycleRepository, userID int64, interval time.Duration) error {
	if repository == nil || repository.begin == nil {
		return errDatabaseRequired
	}
	if ctx == nil {
		return errContextRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if userID <= 0 || interval <= 0 {
		return errors.New("crawl: valid user and interval are required")
	}
	return nil
}

func commitCycleVisit(ctx context.Context, tx cycleTx, userID int64, now time.Time, candidate cycleCandidate) error {
	if err := advanceCycle(ctx, tx, userID, now, candidate); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("crawl: commit cycle cursor: %w", err)
	}
	return nil
}

type cycleCandidate struct {
	person       string
	depth        int
	lastSeen     int64
	lastFavorite int64
	wrapped      bool
	due          bool
	cycleDue     bool
}

func loadCycleCursor(ctx context.Context, tx cycleTx, userID int64) (string, time.Time, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO crawl_cycles (user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING`, userID); err != nil {
		return "", time.Time{}, fmt.Errorf("crawl: initialize cycle: %w", err)
	}
	var cursor string
	var startedAt pgtype.Timestamptz
	if err := tx.QueryRow(ctx, `SELECT cursor_person_nsid, started_at FROM crawl_cycles WHERE user_id=$1 FOR UPDATE`, userID).Scan(&cursor, &startedAt); err != nil {
		return "", time.Time{}, fmt.Errorf("crawl: read cycle cursor: %w", err)
	}
	if !startedAt.Valid {
		return cursor, time.Time{}, nil
	}
	return cursor, startedAt.Time, nil
}

func selectCycleCandidate(ctx context.Context, tx cycleTx, userID int64, cursor string, cutoff time.Time) (cycleCandidate, bool, error) {
	person, depth, lastSeen, lastFavorite, lastChecked, found, err := selectNextInterest(ctx, tx, userID, cursor, false)
	if err != nil {
		return cycleCandidate{}, false, err
	}
	if found {
		return cycleCandidate{person: person, depth: depth, lastSeen: lastSeen, lastFavorite: lastFavorite, due: !lastChecked.Valid || !lastChecked.Time.After(cutoff)}, true, nil
	}
	if cursor == "" {
		return cycleCandidate{}, false, nil
	}
	person, depth, lastSeen, lastFavorite, lastChecked, found, err = selectNextInterest(ctx, tx, userID, cursor, true)
	if err != nil || !found {
		return cycleCandidate{}, false, err
	}
	return cycleCandidate{person: person, depth: depth, lastSeen: lastSeen, lastFavorite: lastFavorite, wrapped: true, due: !lastChecked.Valid || !lastChecked.Time.After(cutoff)}, true, nil
}

func enqueueCycleCheck(ctx context.Context, tx cycleTx, userID int64, now time.Time, candidate cycleCandidate) (bool, error) {
	cursorJSON, err := json.Marshal(map[string]any{
		"page": 1, "method": "flickr.people.getPhotos", "checkpoint_at": now.Unix(),
		"params": map[string]string{
			"user_id": candidate.person, "min_upload_date": fmt.Sprint(candidate.lastSeen), "min_fave_date": fmt.Sprint(candidate.lastFavorite), "per_page": "1",
		},
	})
	if err != nil {
		return false, fmt.Errorf("crawl: encode check cursor: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO work_items (kind, subject_key, user_id, key_id, depth, cursor)
		VALUES ('check', $1, $2, (SELECT fa.key_id FROM flickr_accounts fa WHERE fa.user_id=$2 AND fa.status='ok' ORDER BY fa.id LIMIT 1), $3, $4)
		ON CONFLICT ON CONSTRAINT work_items_dedupe_key DO UPDATE SET
			key_id=COALESCE(EXCLUDED.key_id, work_items.key_id), state='pending', cursor=EXCLUDED.cursor, attempts=0, last_error=NULL,
			not_before=$5, updated_at=$5
		WHERE work_items.state IN ('done','failed')`, candidate.person, userID, candidate.depth, cursorJSON, now)
	if err != nil {
		return false, fmt.Errorf("crawl: enqueue change check: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func advanceCycle(ctx context.Context, tx cycleTx, userID int64, now time.Time, candidate cycleCandidate) error {
	if candidate.due {
		if _, err := tx.Exec(ctx, `UPDATE crawl_interests SET last_checked_at=$3 WHERE user_id=$1 AND person_nsid=$2`, userID, candidate.person, now); err != nil {
			return fmt.Errorf("crawl: mark change check scheduled: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE crawl_cycles SET cursor_person_nsid=$2, updated_at=$3 WHERE user_id=$1`, userID, candidate.person, now); err != nil {
		return fmt.Errorf("crawl: advance cycle cursor: %w", err)
	}
	if candidate.wrapped && candidate.cycleDue {
		cursorJSON, err := json.Marshal(map[string]any{
			"page": 1, "method": "flickr.photos.getContactsPhotos", "checkpoint_at": now.Unix(),
			"params": map[string]string{"user_id": "", "count": "50"},
		})
		if err != nil {
			return fmt.Errorf("crawl: encode contacts photos cursor: %w", err)
		}
		tag, err := tx.Exec(ctx, `INSERT INTO work_items (kind, subject_key, user_id, key_id, depth, cursor)
			SELECT 'contacts_photos', nsid, user_id, key_id, 0, jsonb_set($2::jsonb, '{params,user_id}', to_jsonb(nsid))
			FROM flickr_accounts WHERE user_id=$1 AND status='ok'
			ON CONFLICT ON CONSTRAINT work_items_dedupe_key DO UPDATE SET
				key_id=COALESCE(EXCLUDED.key_id, work_items.key_id), state='pending', cursor=EXCLUDED.cursor, attempts=0, last_error=NULL,
				not_before=$3, lease_expires_at=NULL, updated_at=$3
			WHERE work_items.state IN ('done','failed')`, userID, cursorJSON, now)
		if err != nil {
			return fmt.Errorf("crawl: enqueue contacts photos probes: %w", err)
		}
		if tag.RowsAffected() > 0 {
			if _, err := tx.Exec(ctx, `UPDATE crawl_cycles SET started_at=$2 WHERE user_id=$1`, userID, now); err != nil {
				return fmt.Errorf("crawl: mark contacts photos cycle started: %w", err)
			}
		}
	}
	return nil
}

func selectNextInterest(ctx context.Context, tx cycleTx, userID int64, cursor string, wrap bool) (string, int, int64, int64, pgtype.Timestamptz, bool, error) {
	comparison := ">"
	if wrap {
		comparison = "<="
	}
	query := `SELECT person_nsid, depth,
		COALESCE(floor(extract(epoch FROM last_seen_at))::bigint,0),
		COALESCE(floor(extract(epoch FROM last_fave_at))::bigint,0), last_checked_at
		FROM crawl_interests
		WHERE user_id=$1 AND person_nsid ` + comparison + ` $2
		ORDER BY person_nsid LIMIT 1 FOR UPDATE`
	var person string
	var depth int
	var lastSeen, lastFavorite int64
	var lastChecked pgtype.Timestamptz
	err := tx.QueryRow(ctx, query, userID, cursor).Scan(&person, &depth, &lastSeen, &lastFavorite, &lastChecked)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, 0, 0, pgtype.Timestamptz{}, false, nil
	}
	if err != nil {
		return "", 0, 0, 0, pgtype.Timestamptz{}, false, fmt.Errorf("crawl: select next interest: %w", err)
	}
	return person, depth, lastSeen, lastFavorite, lastChecked, true, nil
}
