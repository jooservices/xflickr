package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	mediaapp "github.com/jooservices/xflickr/internal/media/app"
	"github.com/jooservices/xflickr/internal/platform/queue"
)

// PackedParts returns the currently recorded ZIP objects for repacking a
// download after its failed items are retried.
func (r Downloads) PackedParts(ctx context.Context, downloadID int64) ([]mediaapp.ZIPPart, error) {
	if r.Pool == nil || ctx == nil || downloadID <= 0 {
		return nil, errors.New("media: pack database, context, and download id are required")
	}
	rows, err := r.Pool.Query(ctx, `SELECT part_number,blob_key,bytes,expires_at FROM download_parts WHERE download_id=$1 ORDER BY part_number`, downloadID)
	if err != nil {
		return nil, fmt.Errorf("media: load recorded ZIP parts: %w", err)
	}
	defer rows.Close()
	var parts []mediaapp.ZIPPart
	for rows.Next() {
		var part mediaapp.ZIPPart
		if err := rows.Scan(&part.Number, &part.BlobKey, &part.Bytes, &part.ExpiresAt); err != nil {
			return nil, fmt.Errorf("media: scan recorded ZIP part: %w", err)
		}
		parts = append(parts, part)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("media: read recorded ZIP parts: %w", err)
	}
	return parts, nil
}

func (r Downloads) PartsForDownload(ctx context.Context, userID, downloadID int64) ([]mediaapp.DownloadPart, error) {
	if r.Pool == nil || ctx == nil || userID <= 0 || downloadID <= 0 {
		return nil, errors.New("media: download database, context, user and download id are required")
	}
	rows, err := r.Pool.Query(ctx, `
		SELECT dp.download_id,dp.part_number,dp.blob_key,dp.bytes,LEAST(dp.expires_at,d.expires_at)
		FROM downloads d
		JOIN download_parts dp ON dp.download_id=d.id
		WHERE d.id=$1 AND d.user_id=$2
		  AND d.status IN ('ready','ready_with_errors')
		  AND d.expires_at>NOW() AND dp.expires_at>NOW()
		ORDER BY dp.part_number`, downloadID, userID)
	if err != nil {
		return nil, fmt.Errorf("media: load download parts: %w", err)
	}
	defer rows.Close()
	var parts []mediaapp.DownloadPart
	for rows.Next() {
		var part mediaapp.DownloadPart
		if err := rows.Scan(&part.DownloadID, &part.Number, &part.BlobKey, &part.Bytes, &part.ExpiresAt); err != nil {
			return nil, fmt.Errorf("media: scan download part: %w", err)
		}
		parts = append(parts, part)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("media: read download parts: %w", err)
	}
	if len(parts) == 0 {
		return nil, mediaapp.ErrDownloadNotFound
	}
	return parts, nil
}

func (r Downloads) MarkPartFetched(ctx context.Context, userID, downloadID int64, partNumber int) error {
	if r.Pool == nil || ctx == nil || userID <= 0 || downloadID <= 0 || partNumber <= 0 {
		return errors.New("media: invalid fetched download part")
	}
	var id int64
	err := r.Pool.QueryRow(ctx, `
		UPDATE download_parts dp
		SET fetched_at=COALESCE(dp.fetched_at,NOW())
		FROM downloads d
		WHERE d.id=dp.download_id AND d.id=$1 AND d.user_id=$2
		  AND d.status IN ('ready','ready_with_errors')
		  AND d.expires_at>NOW() AND dp.expires_at>NOW()
		  AND dp.part_number=$3
		RETURNING dp.id`, downloadID, userID, partNumber).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return mediaapp.ErrDownloadNotFound
	}
	if err != nil {
		return fmt.Errorf("media: mark download part fetched: %w", err)
	}
	return nil
}

func (r Downloads) RetryFailedDownload(ctx context.Context, userID, downloadID int64) (int, error) {
	if r.Pool == nil || r.Jobs == nil || ctx == nil || userID <= 0 || downloadID <= 0 {
		return 0, errors.New("media: retry requires download database, queue, context, user and download id")
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("media: begin failed-download retry: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	if err := lockDownloadForRetry(ctx, tx, userID, downloadID); err != nil {
		return 0, err
	}
	photoIDs, err := resetFailedDownloadItems(ctx, tx, downloadID)
	if err != nil {
		return 0, err
	}
	if len(photoIDs) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return 0, fmt.Errorf("media: commit empty failed-download retry: %w", err)
		}
		return 0, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE downloads SET status='pending' WHERE id=$1`, downloadID); err != nil {
		return 0, fmt.Errorf("media: reset download status for retry: %w", err)
	}
	if err := enqueueFailedDownloadItems(ctx, tx, r.Jobs, downloadID, photoIDs); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("media: commit failed-download retry: %w", err)
	}
	r.publishProgress(ctx, downloadID)
	return len(photoIDs), nil
}

func lockDownloadForRetry(ctx context.Context, tx pgx.Tx, userID, downloadID int64) error {
	var status string
	var expired bool
	err := tx.QueryRow(ctx, `SELECT status,expires_at<=NOW() FROM downloads WHERE id=$1 AND user_id=$2 FOR UPDATE`, downloadID, userID).Scan(&status, &expired)
	if errors.Is(err, pgx.ErrNoRows) {
		return mediaapp.ErrDownloadNotFound
	}
	if err != nil {
		return fmt.Errorf("media: lock download for retry: %w", err)
	}
	if expired || status == "expired" || (status != "pending" && status != "ready" && status != "ready_with_errors") {
		return mediaapp.ErrDownloadNotFound
	}
	return nil
}

func resetFailedDownloadItems(ctx context.Context, tx pgx.Tx, downloadID int64) ([]int64, error) {
	rows, err := tx.Query(ctx, `UPDATE download_items SET state='pending',error='' WHERE download_id=$1 AND state='failed' RETURNING photo_id`, downloadID)
	if err != nil {
		return nil, fmt.Errorf("media: reset failed download items: %w", err)
	}
	var photoIDs []int64
	for rows.Next() {
		var photoID int64
		if err := rows.Scan(&photoID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("media: scan failed download item: %w", err)
		}
		photoIDs = append(photoIDs, photoID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("media: read failed download items: %w", err)
	}
	rows.Close()
	return photoIDs, nil
}

func enqueueFailedDownloadItems(ctx context.Context, tx pgx.Tx, jobs queue.JobQueue, downloadID int64, photoIDs []int64) error {
	for _, photoID := range photoIDs {
		args, _ := json.Marshal(map[string]int64{"download_id": downloadID, "photo_id": photoID})
		if err := jobs.Enqueue(ctx, tx, queue.Job{Kind: "media.fetch", Args: args}, queue.Options{Queue: "media", MaxAttempts: mediaapp.FetchMaxAttempts}); err != nil {
			return fmt.Errorf("media: enqueue failed download item %d: %w", photoID, err)
		}
	}
	return nil
}

func (r Downloads) ExpiredDownloadParts(ctx context.Context, limit int) ([]mediaapp.ExpiredDownloadPart, error) {
	if r.Pool == nil || ctx == nil || limit <= 0 {
		return nil, errors.New("media: invalid expired download part selection")
	}
	rows, err := r.Pool.Query(ctx, `
		SELECT dp.download_id,dp.part_number,dp.blob_key
		FROM download_parts dp
		JOIN downloads d ON d.id=dp.download_id
		WHERE d.expires_at<=NOW()
		   OR EXISTS (
			SELECT 1 FROM download_parts due
			WHERE due.download_id=d.id AND due.expires_at<=NOW()
		   )
		   OR (
			d.status IN ('ready','ready_with_errors')
			AND EXISTS (SELECT 1 FROM download_parts any_part WHERE any_part.download_id=d.id)
			AND NOT EXISTS (
				SELECT 1 FROM download_parts unfetched
				WHERE unfetched.download_id=d.id AND unfetched.fetched_at IS NULL
			)
		   )
		ORDER BY d.expires_at,dp.download_id,dp.part_number
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("media: list expired download parts: %w", err)
	}
	defer rows.Close()
	var parts []mediaapp.ExpiredDownloadPart
	for rows.Next() {
		var part mediaapp.ExpiredDownloadPart
		if err := rows.Scan(&part.DownloadID, &part.Number, &part.BlobKey); err != nil {
			return nil, fmt.Errorf("media: scan expired download part: %w", err)
		}
		parts = append(parts, part)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("media: read expired download parts: %w", err)
	}
	return parts, nil
}

func (r Downloads) DeleteDownloadPart(ctx context.Context, downloadID int64, partNumber int) error {
	if r.Pool == nil || ctx == nil || downloadID <= 0 || partNumber <= 0 {
		return errors.New("media: invalid download part deletion")
	}
	if _, err := r.Pool.Exec(ctx, `DELETE FROM download_parts WHERE download_id=$1 AND part_number=$2`, downloadID, partNumber); err != nil {
		return fmt.Errorf("media: delete download part metadata: %w", err)
	}
	return nil
}

func (r Downloads) MarkDownloadExpired(ctx context.Context, downloadID int64) error {
	if r.Pool == nil || ctx == nil || downloadID <= 0 {
		return errors.New("media: invalid download expiry")
	}
	if _, err := r.Pool.Exec(ctx, `
		UPDATE downloads d SET status='expired'
		WHERE d.id=$1 AND d.status<>'expired'
		  AND NOT EXISTS (SELECT 1 FROM download_parts dp WHERE dp.download_id=d.id)`, downloadID); err != nil {
		return fmt.Errorf("media: mark download expired: %w", err)
	}
	return nil
}

func (r Downloads) MarkExpiredDownloads(ctx context.Context, limit int) error {
	if r.Pool == nil || ctx == nil || limit <= 0 {
		return errors.New("media: invalid partless download expiry")
	}
	if _, err := r.Pool.Exec(ctx, `
		WITH expired AS (
			SELECT d.id
			FROM downloads d
			WHERE d.status<>'expired' AND d.expires_at<=NOW()
			  AND NOT EXISTS (SELECT 1 FROM download_parts dp WHERE dp.download_id=d.id)
			ORDER BY d.expires_at,d.id
			LIMIT $1
		)
		UPDATE downloads d SET status='expired'
		FROM expired e WHERE d.id=e.id`, limit); err != nil {
		return fmt.Errorf("media: mark expired partless downloads: %w", err)
	}
	return nil
}
