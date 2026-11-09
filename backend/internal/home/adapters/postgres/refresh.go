package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jooservices/xflickr/internal/home/app"
)

type refreshDatabase interface {
	Begin(context.Context) (pgx.Tx, error)
}

type refreshTx interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Commit(context.Context) error
	Rollback(context.Context) error
}

var errRefreshDatabaseRequired = errors.New("home: postgres refresh database is required")

const (
	startRefreshSQL = `INSERT INTO user_home_stats_refreshes (user_id)
VALUES ($1) ON CONFLICT (user_id) DO NOTHING`
	lockRefreshSQL = `SELECT cursor_record_id, photos_total, sizes_ready, captioned
FROM user_home_stats_refreshes WHERE user_id = $1 FOR UPDATE`
	advanceRefreshSQL = `UPDATE user_home_stats_refreshes
SET cursor_record_id = $2,
    photos_total = photos_total + $3,
    sizes_ready = sizes_ready + $4,
    captioned = captioned + $5
WHERE user_id = $1`
	upsertContactRefreshSQL = `INSERT INTO user_home_contact_stats_refreshes
(user_id, person_nsid, photos_total, sizes_ready)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, person_nsid) DO UPDATE SET
photos_total = user_home_contact_stats_refreshes.photos_total + EXCLUDED.photos_total,
sizes_ready = user_home_contact_stats_refreshes.sizes_ready + EXCLUDED.sizes_ready`
	publishRefreshSQL = `INSERT INTO user_home_stats
(user_id, photos_total, sizes_ready, captioned, computed_at)
SELECT user_id, photos_total, sizes_ready, captioned, statement_timestamp()
FROM user_home_stats_refreshes WHERE user_id = $1
ON CONFLICT (user_id) DO UPDATE SET
photos_total = EXCLUDED.photos_total,
sizes_ready = EXCLUDED.sizes_ready,
captioned = EXCLUDED.captioned,
computed_at = EXCLUDED.computed_at`
	copyContactRefreshSQL = `INSERT INTO user_home_contact_stats
(user_id, person_nsid, photos_total, sizes_ready)
SELECT user_id, person_nsid, photos_total, sizes_ready
FROM user_home_contact_stats_refreshes WHERE user_id = $1`
)

// RefreshBatch advances one bounded, keyset-paginated part of a user's counts.
func (r *Repository) RefreshBatch(ctx context.Context, userID int64, batchSize int) (bool, error) {
	if r == nil || r.db == nil || ctx == nil || userID <= 0 || batchSize < 1 || batchSize > app.RefreshBatchSize {
		return false, errors.New("home: repository, context, user id, and bounded batch size are required")
	}
	database, ok := r.db.(refreshDatabase)
	if !ok {
		return false, errRefreshDatabaseRequired
	}
	tx, err := database.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("home: begin stats refresh batch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := refreshBatch(ctx, tx, userID, batchSize); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("home: commit stats refresh batch: %w", err)
	}
	var remaining bool
	if err := r.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_home_stats_refreshes WHERE user_id=$1)`, userID).Scan(&remaining); err != nil {
		return false, fmt.Errorf("home: check stats refresh progress: %w", err)
	}
	return !remaining, nil
}

func refreshBatch(ctx context.Context, tx refreshTx, userID int64, batchSize int) error {
	if err := execRefresh(ctx, tx, startRefreshSQL, "start stats refresh", userID); err != nil {
		return err
	}
	cursor, err := lockRefresh(ctx, tx, userID)
	if err != nil {
		return err
	}
	page, err := readRefreshPage(ctx, tx, userID, cursor, batchSize)
	if err != nil {
		return err
	}
	if err := saveRefreshPage(ctx, tx, userID, page); err != nil {
		return err
	}
	if page.count == batchSize {
		return nil
	}
	return publishRefresh(ctx, tx, userID)
}

func lockRefresh(ctx context.Context, tx refreshTx, userID int64) (string, error) {
	var cursor string
	if err := tx.QueryRow(ctx, lockRefreshSQL, userID).Scan(&cursor, new(int64), new(int64), new(int64)); err != nil {
		return "", fmt.Errorf("home: lock stats refresh: %w", err)
	}
	return cursor, nil
}

type refreshPage struct {
	cursor     string
	count      int
	total      int64
	sizesReady int64
	captioned  int64
	contacts   map[string]contactCounts
}

func readRefreshPage(ctx context.Context, tx refreshTx, userID int64, cursor string, batchSize int) (refreshPage, error) {
	rows, err := tx.Query(ctx, refreshPhotoPageSQL, userID, cursor, batchSize)
	if err != nil {
		return refreshPage{}, fmt.Errorf("home: read stats refresh page: %w", err)
	}
	defer rows.Close()
	page := refreshPage{contacts: make(map[string]contactCounts)}
	for rows.Next() {
		var recordID, owner string
		var visible, sizeReady, hasCaption bool
		if err := rows.Scan(&recordID, &owner, &visible, &sizeReady, &hasCaption); err != nil {
			return refreshPage{}, fmt.Errorf("home: scan stats refresh page: %w", err)
		}
		page.cursor = recordID
		page.count++
		if !visible {
			continue
		}
		page.total++
		if sizeReady {
			page.sizesReady++
		}
		if hasCaption {
			page.captioned++
		}
		counts := page.contacts[owner]
		counts.total++
		if sizeReady {
			counts.sizesReady++
		}
		page.contacts[owner] = counts
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return refreshPage{}, fmt.Errorf("home: iterate stats refresh page: %w", err)
	}
	return page, nil
}

func saveRefreshPage(ctx context.Context, tx refreshTx, userID int64, page refreshPage) error {
	if page.count > 0 {
		if err := execRefresh(ctx, tx, advanceRefreshSQL, "advance stats refresh cursor", userID, page.cursor, page.total, page.sizesReady, page.captioned); err != nil {
			return err
		}
	}
	for owner, counts := range page.contacts {
		if err := execRefresh(ctx, tx, upsertContactRefreshSQL, "save contact stats", userID, owner, counts.total, counts.sizesReady); err != nil {
			return fmt.Errorf("home: save contact stats for %q: %w", owner, err)
		}
	}
	return nil
}

func publishRefresh(ctx context.Context, tx refreshTx, userID int64) error {
	statements := []struct {
		query string
		name  string
	}{
		{publishRefreshSQL, "publish stats refresh"},
		{`DELETE FROM user_home_contact_stats WHERE user_id=$1`, "replace contact stats"},
		{copyContactRefreshSQL, "copy contact stats"},
		{`DELETE FROM user_home_contact_stats_refreshes WHERE user_id=$1`, "clear contact stats refresh"},
		{`DELETE FROM user_home_stats_refreshes WHERE user_id=$1`, "complete stats refresh"},
	}
	for _, statement := range statements {
		if err := execRefresh(ctx, tx, statement.query, statement.name, userID); err != nil {
			return err
		}
	}
	return nil
}

func execRefresh(ctx context.Context, tx refreshTx, query, operation string, args ...any) error {
	if _, err := tx.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("home: %s: %w", operation, err)
	}
	return nil
}

type contactCounts struct {
	total      int64
	sizesReady int64
}

const refreshPhotoPageSQL = `
WITH own_page AS MATERIALIZED (
    SELECT DISTINCT observation.record_id
    FROM observations observation
    WHERE observation.user_id = $1
      AND observation.record_kind = 'photo'
      AND observation.record_id ~ '^[0-9]+$'
      AND observation.record_id > $2
    ORDER BY observation.record_id
    LIMIT $3
),
shared_page AS MATERIALIZED (
    SELECT DISTINCT observation.record_id
    FROM observations observation
    JOIN sharing_settings setting
      ON setting.user_id = observation.user_id AND setting.enabled
    WHERE observation.record_kind = 'photo'
      AND observation.record_id ~ '^[0-9]+$'
      AND observation.record_id > $2
    ORDER BY observation.record_id
    LIMIT $3
),
candidate_ids AS MATERIALIZED (
    SELECT record_id FROM own_page
    UNION
    SELECT record_id FROM shared_page
),
page AS (
    SELECT candidate.record_id, candidate.record_id::bigint AS photo_id
    FROM candidate_ids candidate
    ORDER BY candidate.record_id
    LIMIT $3
)
SELECT page.record_id,
       COALESCE(photo.owner_nsid, ''),
       photo.id IS NOT NULL,
       COALESCE(photo.sizes_status = 'confirmed' AND photo.sizes_checked_at >= photo.last_update, FALSE),
       COALESCE(EXISTS (
           SELECT 1 FROM photo_enrichments enrichment
           WHERE enrichment.photo_id = photo.id AND enrichment.kind = 'caption'
       ), FALSE)
FROM page
LEFT JOIN photos photo
  ON photo.id = page.photo_id
 AND photo.visibility = 'public'
 AND photo.tombstoned_at IS NULL
ORDER BY page.record_id`
