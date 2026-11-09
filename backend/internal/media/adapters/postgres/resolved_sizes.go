package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/platform/queue"
)

// ResolveSizes transitions only waiting items. Download locks serialize this
// handoff with fetch settlement, so the final terminal item always queues packing.
func (r Downloads) ResolveSizes(ctx context.Context, photoID, userID int64, status string) error {
	if ctx == nil || r.Pool == nil || r.Jobs == nil {
		return errors.New("media: sizes continuation database, queue and context are required")
	}
	if photoID <= 0 || userID < 0 || (status != "confirmed" && status != "unavailable") {
		return errors.New("media: invalid sizes continuation")
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("media: begin sizes continuation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ids, err := lockWaitingDownloads(ctx, tx, photoID, userID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := r.resolveDownloadSizes(ctx, tx, id, photoID, status); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("media: commit sizes continuation: %w", err)
	}
	for _, id := range ids {
		r.publishProgress(ctx, id)
	}
	return nil
}

func lockWaitingDownloads(ctx context.Context, tx pgx.Tx, photoID, userID int64) ([]int64, error) {
	rows, err := tx.Query(ctx, `SELECT d.id FROM downloads d WHERE ($2::bigint=0 OR d.user_id=$2)
AND EXISTS (SELECT 1 FROM download_items di WHERE di.download_id=d.id AND di.photo_id=$1 AND di.state='waiting_sizes')
ORDER BY d.id FOR UPDATE`, photoID, userID)
	if err != nil {
		return nil, fmt.Errorf("media: lock waiting downloads: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("media: scan waiting download: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r Downloads) resolveDownloadSizes(ctx context.Context, tx pgx.Tx, id int64, photoID int64, status string) error {
	state, failure := "pending", ""
	if status == "unavailable" {
		state, failure = "failed", "sizes_unavailable"
	}
	tag, err := tx.Exec(ctx, `UPDATE download_items SET state=$3,error=$4 WHERE download_id=$1 AND photo_id=$2 AND state='waiting_sizes'`, id, photoID, state, failure)
	if err != nil {
		return fmt.Errorf("media: resolve waiting item: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	if state == "failed" {
		return enqueuePackIfSettled(ctx, tx, r.Jobs, id)
	}
	return enqueueFailedDownloadItems(ctx, tx, r.Jobs, id, []int64{photoID})
}

func enqueuePackIfSettled(ctx context.Context, tx pgx.Tx, jobs queue.JobQueue, downloadID int64) error {
	var unsettled int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM download_items WHERE download_id=$1 AND state NOT IN ('ready','failed')`, downloadID).Scan(&unsettled); err != nil {
		return fmt.Errorf("media: check unsettled download items: %w", err)
	}
	if unsettled != 0 {
		return nil
	}
	args, _ := json.Marshal(map[string]int64{"download_id": downloadID})
	if err := jobs.Enqueue(ctx, tx, queue.Job{Kind: "media.pack", Args: args}, queue.Options{Queue: "media", MaxAttempts: 3, LeaseTimeout: 2 * time.Hour}); err != nil {
		return fmt.Errorf("media: enqueue pack job: %w", err)
	}
	return nil
}
