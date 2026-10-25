package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	mediaapp "github.com/jooservices/xflickr/internal/media/app"
	mediadomain "github.com/jooservices/xflickr/internal/media/domain"
	"github.com/jooservices/xflickr/internal/platform/queue"
	quotadomain "github.com/jooservices/xflickr/internal/quota/domain"
)

type Downloads struct {
	Pool *pgxpool.Pool
	Jobs queue.JobQueue
}

var _ mediaapp.DownloadRepository = Downloads{}

func (r Downloads) Create(ctx context.Context, download mediadomain.Download, candidates []mediaapp.DownloadItemCandidate) (mediadomain.Download, error) {
	if r.Pool == nil || r.Jobs == nil || ctx == nil {
		return mediadomain.Download{}, errors.New("media: download database, queue and context are required")
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return mediadomain.Download{}, fmt.Errorf("media: begin download: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	err = tx.QueryRow(ctx, `INSERT INTO downloads(user_id,kind,subject,status,created_at,expires_at) VALUES($1,$2,$3,'pending',$4,$5) RETURNING id`, download.UserID, string(download.Kind), download.Subject, download.CreatedAt, download.ExpiresAt).Scan(&download.ID)
	if err != nil {
		return mediadomain.Download{}, fmt.Errorf("media: insert download: %w", err)
	}
	byID := make(map[int64]mediaapp.DownloadItemCandidate, len(candidates))
	for _, candidate := range candidates {
		byID[candidate.PhotoID] = candidate
	}
	for _, item := range download.Items {
		if _, err := tx.Exec(ctx, `INSERT INTO download_items(download_id,photo_id,state) VALUES($1,$2,$3)`, download.ID, item.PhotoID, string(item.State)); err != nil {
			return mediadomain.Download{}, fmt.Errorf("media: insert download item: %w", err)
		}
		candidate := byID[item.PhotoID]
		if candidate.SizesConfirmed {
			args, _ := json.Marshal(map[string]any{"download_id": download.ID, "photo_id": item.PhotoID})
			if err := r.Jobs.Enqueue(ctx, tx, queue.Job{Kind: "media.fetch", Args: args}, queue.Options{Queue: "media"}); err != nil {
				return mediadomain.Download{}, fmt.Errorf("media: enqueue fetch: %w", err)
			}
			continue
		}
		var keyID int64
		if err := tx.QueryRow(ctx, `SELECT fa.key_id FROM flickr_accounts fa JOIN api_keys k ON k.id=fa.key_id WHERE fa.user_id=$1 AND fa.status='ok' AND k.status='valid' ORDER BY fa.id LIMIT 1`, download.UserID).Scan(&keyID); err != nil {
			return mediadomain.Download{}, fmt.Errorf("media: find user Flickr key: %w", err)
		}
		args, _ := json.Marshal(map[string]any{"photo_id": item.PhotoID, "owner_nsid": candidate.OwnerNSID, "facet": "sizes", "key_id": keyID, "class": string(quotadomain.Interactive), "dedupe_key": fmt.Sprintf("photo:%d:sizes", item.PhotoID)})
		if err := r.Jobs.Enqueue(ctx, tx, queue.Job{Kind: "photo.sizes", Args: args}, queue.Options{Queue: "sync"}); err != nil {
			return mediadomain.Download{}, fmt.Errorf("media: enqueue sizes: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return mediadomain.Download{}, fmt.Errorf("media: commit download: %w", err)
	}
	return download, nil
}
