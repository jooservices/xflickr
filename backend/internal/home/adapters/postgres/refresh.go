package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

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
	startRefreshSQL = `INSERT INTO user_home_stats_refreshes (user_id, source_revision, shared_revision)
SELECT $1, source.revision, shared.revision
FROM user_home_stats_versions source
CROSS JOIN user_home_shared_version shared
WHERE source.user_id = $1 AND shared.id = TRUE
ON CONFLICT (user_id) DO NOTHING`
	resetRefreshSQL = `WITH current_versions AS MATERIALIZED (
    SELECT source.revision AS source_revision, shared.revision AS shared_revision
    FROM user_home_stats_versions source
    CROSS JOIN user_home_shared_version shared
    WHERE source.user_id = $1 AND shared.id = TRUE
), reset AS (
    UPDATE user_home_stats_refreshes refresh
    SET cursor_record_id = '',
        photos_total = 0,
        sizes_ready = 0,
        captioned = 0,
        source_revision = current_versions.source_revision,
        shared_revision = current_versions.shared_revision
    FROM current_versions
    WHERE refresh.user_id = $1
      AND (refresh.source_revision <> current_versions.source_revision
           OR refresh.shared_revision <> current_versions.shared_revision)
    RETURNING refresh.user_id
)
DELETE FROM user_home_contact_stats_refreshes
WHERE user_id IN (SELECT user_id FROM reset)`
	lockRefreshSQL = `SELECT cursor_record_id, photos_total, sizes_ready, captioned, source_revision, shared_revision
FROM user_home_stats_refreshes WHERE user_id = $1 FOR UPDATE`
	advanceRefreshSQL = `UPDATE user_home_stats_refreshes
SET cursor_record_id = $2,
    photos_total = photos_total + $3,
    sizes_ready = sizes_ready + $4,
    captioned = captioned + $5
WHERE user_id = $1`
	upsertContactRefreshSQL = `INSERT INTO user_home_contact_stats_refreshes
(user_id, person_nsid, photos_total, sizes_ready)
SELECT $1, contact.person_nsid, contact.photos_total, contact.sizes_ready
FROM jsonb_to_recordset($2::jsonb) AS contact(
    person_nsid TEXT, photos_total BIGINT, sizes_ready BIGINT
)
ON CONFLICT (user_id, person_nsid) DO UPDATE SET
photos_total = user_home_contact_stats_refreshes.photos_total + EXCLUDED.photos_total,
sizes_ready = user_home_contact_stats_refreshes.sizes_ready + EXCLUDED.sizes_ready`
	publishRefreshSQL = `INSERT INTO user_home_stats
(user_id, photos_total, sizes_ready, captioned, computed_at, source_revision, shared_revision)
SELECT refresh.user_id, refresh.photos_total, refresh.sizes_ready, refresh.captioned,
       statement_timestamp(), refresh.source_revision, refresh.shared_revision
FROM user_home_stats_refreshes refresh
JOIN user_home_stats_versions source
  ON source.user_id = refresh.user_id AND source.revision = refresh.source_revision
JOIN user_home_shared_version shared
  ON shared.id = TRUE AND shared.revision = refresh.shared_revision
WHERE refresh.user_id = $1
ON CONFLICT (user_id) DO UPDATE SET
photos_total = EXCLUDED.photos_total,
sizes_ready = EXCLUDED.sizes_ready,
captioned = EXCLUDED.captioned,
computed_at = EXCLUDED.computed_at,
source_revision = EXCLUDED.source_revision,
shared_revision = EXCLUDED.shared_revision`
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
	batchSize, err := configuredRefreshBatchSize(ctx, r.db, batchSize)
	if err != nil {
		return false, err
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
	return refreshCompleted(ctx, r.db, userID)
}

func configuredRefreshBatchSize(ctx context.Context, database queryer, requested int) (int, error) {
	if requested != app.RefreshBatchSize {
		return requested, nil
	}
	var configured int64
	if err := database.QueryRow(ctx, `SELECT (value #>> '{}')::bigint
FROM app_settings WHERE key = 'home.stats_refresh_batch_size'`).Scan(&configured); err != nil {
		return 0, fmt.Errorf("home: load configured refresh batch size: %w", err)
	}
	if configured < 250 || configured > int64(app.RefreshBatchSize) {
		return 0, errors.New("home: configured refresh batch size is outside the supported range")
	}
	return int(configured), nil
}

func refreshCompleted(ctx context.Context, database queryer, userID int64) (bool, error) {
	var remaining bool
	if err := database.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_home_stats_refreshes WHERE user_id=$1)`, userID).Scan(&remaining); err != nil {
		return false, fmt.Errorf("home: check stats refresh progress: %w", err)
	}
	return !remaining, nil
}

func refreshBatch(ctx context.Context, tx refreshTx, userID int64, batchSize int) error {
	if err := execRefresh(ctx, tx, startRefreshSQL, "start stats refresh", userID); err != nil {
		return err
	}
	if err := execRefresh(ctx, tx, resetRefreshSQL, "reset changed stats refresh", userID); err != nil {
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
	if err := tx.QueryRow(ctx, lockRefreshSQL, userID).Scan(
		&cursor, new(int64), new(int64), new(int64), new(int64), new(int64),
	); err != nil {
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
	if len(page.contacts) == 0 {
		return nil
	}
	contacts := make([]struct {
		PersonNSID string `json:"person_nsid"`
		Photos     int64  `json:"photos_total"`
		SizesReady int64  `json:"sizes_ready"`
	}, 0, len(page.contacts))
	for owner, counts := range page.contacts {
		contacts = append(contacts, struct {
			PersonNSID string `json:"person_nsid"`
			Photos     int64  `json:"photos_total"`
			SizesReady int64  `json:"sizes_ready"`
		}{PersonNSID: owner, Photos: counts.total, SizesReady: counts.sizesReady})
	}
	sort.Slice(contacts, func(i, j int) bool { return contacts[i].PersonNSID < contacts[j].PersonNSID })
	payload, err := json.Marshal(contacts)
	if err != nil {
		return fmt.Errorf("home: encode contact stats: %w", err)
	}
	return execRefresh(ctx, tx, upsertContactRefreshSQL, "save contact stats", userID, string(payload))
}

func publishRefresh(ctx context.Context, tx refreshTx, userID int64) error {
	tag, err := tx.Exec(ctx, publishRefreshSQL, userID)
	if err != nil {
		return fmt.Errorf("home: publish stats refresh: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return execRefresh(ctx, tx, resetRefreshSQL, "reset changed stats refresh", userID)
	}
	statements := []struct {
		query string
		name  string
	}{
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
WITH candidate_pages AS MATERIALIZED (
    (SELECT candidate.photo_id
     FROM user_home_photo_candidates candidate
     WHERE candidate.user_id = $1
       AND candidate.photo_id > COALESCE(NULLIF($2, '')::bigint, 0)
     ORDER BY candidate.photo_id
     LIMIT $3)
    UNION
    (SELECT shared_candidate.photo_id
     FROM user_home_shared_photo_candidates shared_candidate
     WHERE shared_candidate.photo_id > COALESCE(NULLIF($2, '')::bigint, 0)
     ORDER BY shared_candidate.photo_id
     LIMIT $3)
),
page AS (
    SELECT candidate_pages.photo_id
    FROM candidate_pages
    ORDER BY candidate_pages.photo_id
    LIMIT $3
)
SELECT page.photo_id::text,
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
ORDER BY page.photo_id`
