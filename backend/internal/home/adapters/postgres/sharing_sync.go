package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const (
	lockSharingSyncSQL = `SELECT enabled, cursor_photo_id
FROM user_home_sharing_syncs WHERE user_id = $1 FOR UPDATE`
	sharingCandidatePageSQL = `SELECT photo_id
FROM user_home_photo_candidates
WHERE user_id = $1 AND photo_id > $2
ORDER BY photo_id
LIMIT $3`
	addSharedCandidatesSQL = `INSERT INTO user_home_shared_photo_candidates (photo_id, uploaded_at)
SELECT candidate.photo_id, candidate.uploaded_at
FROM user_home_photo_candidates candidate
JOIN photos photo ON photo.id = candidate.photo_id
WHERE candidate.user_id = $1
  AND candidate.photo_id = ANY($2::bigint[])
  AND photo.visibility = 'public'
  AND photo.tombstoned_at IS NULL
  AND EXISTS (
      SELECT 1 FROM sharing_settings setting
      WHERE setting.user_id = candidate.user_id AND setting.enabled
  )
ON CONFLICT (photo_id) DO UPDATE SET uploaded_at = EXCLUDED.uploaded_at`
	removeUnsharedCandidatesSQL = `DELETE FROM user_home_shared_photo_candidates candidate
WHERE candidate.photo_id = ANY($1::bigint[])
  AND NOT EXISTS (
      SELECT 1
      FROM photos photo
      WHERE photo.id = candidate.photo_id
        AND photo.visibility = 'public'
        AND photo.tombstoned_at IS NULL
        AND EXISTS (
            SELECT 1
            FROM observations observation
            JOIN sharing_settings setting
              ON setting.user_id = observation.user_id AND setting.enabled
            WHERE observation.record_kind = 'photo'
              AND observation.record_id = photo.id::text
        )
  )`
)

func syncSharingCandidatesBatch(ctx context.Context, tx refreshTx, userID int64, batchSize int) (bool, error) {
	var enabled bool
	var cursor int64
	if err := tx.QueryRow(ctx, lockSharingSyncSQL, userID).Scan(&enabled, &cursor); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("home: lock sharing candidate sync: %w", err)
	}
	photoIDs, err := readSharingCandidatePage(ctx, tx, userID, cursor, batchSize)
	if err != nil {
		return false, err
	}
	if len(photoIDs) == 0 {
		return false, finishSharingCandidateSync(ctx, tx, userID)
	}
	if err := updateSharedCandidates(ctx, tx, enabled, userID, photoIDs); err != nil {
		return false, err
	}
	if len(photoIDs) < batchSize {
		return false, finishSharingCandidateSync(ctx, tx, userID)
	}
	if _, err := tx.Exec(ctx, `UPDATE user_home_sharing_syncs
SET cursor_photo_id = $2 WHERE user_id = $1`, userID, photoIDs[len(photoIDs)-1]); err != nil {
		return false, fmt.Errorf("home: advance sharing candidate sync: %w", err)
	}
	return true, nil
}

func readSharingCandidatePage(ctx context.Context, tx refreshTx, userID, cursor int64, batchSize int) ([]int64, error) {
	rows, err := tx.Query(ctx, sharingCandidatePageSQL, userID, cursor, batchSize)
	if err != nil {
		return nil, fmt.Errorf("home: read sharing candidate page: %w", err)
	}
	defer rows.Close()
	photoIDs := make([]int64, 0, batchSize)
	for rows.Next() {
		var photoID int64
		if err := rows.Scan(&photoID); err != nil {
			return nil, fmt.Errorf("home: scan sharing candidate page: %w", err)
		}
		photoIDs = append(photoIDs, photoID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("home: iterate sharing candidate page: %w", err)
	}
	return photoIDs, nil
}

func updateSharedCandidates(ctx context.Context, tx refreshTx, enabled bool, userID int64, photoIDs []int64) error {
	query := removeUnsharedCandidatesSQL
	args := []any{photoIDs}
	if enabled {
		query = addSharedCandidatesSQL
		args = []any{userID, photoIDs}
	}
	if _, err := tx.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("home: update shared photo candidates: %w", err)
	}
	return nil
}

func finishSharingCandidateSync(ctx context.Context, tx refreshTx, userID int64) error {
	if _, err := tx.Exec(ctx, `DELETE FROM user_home_sharing_syncs WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("home: complete sharing candidate sync: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE user_home_shared_version SET revision = revision + 1 WHERE id = TRUE`); err != nil {
		return fmt.Errorf("home: publish shared candidate revision: %w", err)
	}
	return nil
}
