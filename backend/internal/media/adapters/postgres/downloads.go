package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
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

var (
	_ mediaapp.DownloadRepository = Downloads{}
	_ mediaapp.PackRepository     = Downloads{}
)

// PackItems loads a settled download and the TTL configured by its user's plan.
func (r Downloads) PackItems(ctx context.Context, downloadID int64) ([]mediaapp.PackItem, time.Duration, error) {
	if r.Pool == nil || ctx == nil || downloadID <= 0 {
		return nil, 0, errors.New("media: pack database, context, and download id are required")
	}
	var ttlHours int
	if err := r.Pool.QueryRow(ctx, `SELECT p.zip_ttl_hours FROM downloads d JOIN users u ON u.id=d.user_id JOIN plans p ON p.id=u.plan_id WHERE d.id=$1`, downloadID).Scan(&ttlHours); err != nil {
		return nil, 0, fmt.Errorf("media: load download ZIP TTL: %w", err)
	}
	rows, err := r.Pool.Query(ctx, `SELECT di.photo_id, 'photo-'||di.photo_id::text||'-'||COALESCE(a.size_label,'original'), COALESCE(a.blob_key,''), COALESCE(a.bytes,0), COALESCE(encode(a.sha256,'hex'),''), di.error, di.state::text FROM download_items di LEFT JOIN assets a ON a.id=di.asset_id WHERE di.download_id=$1 ORDER BY di.photo_id`, downloadID)
	if err != nil {
		return nil, 0, fmt.Errorf("media: load download items for pack: %w", err)
	}
	defer rows.Close()
	items := make([]mediaapp.PackItem, 0)
	for rows.Next() {
		var item mediaapp.PackItem
		var state string
		if err := rows.Scan(&item.PhotoID, &item.Name, &item.BlobKey, &item.Size, &item.SHA256, &item.Failure, &state); err != nil {
			return nil, 0, fmt.Errorf("media: scan download pack item: %w", err)
		}
		if state != "ready" && state != "failed" {
			return nil, 0, fmt.Errorf("media: download item %d is not settled", item.PhotoID)
		}
		if state == "ready" && item.BlobKey == "" {
			return nil, 0, fmt.Errorf("media: ready download item %d has no asset", item.PhotoID)
		}
		if state == "failed" && item.Failure == "" {
			item.Failure = "download failed"
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("media: read download pack items: %w", err)
	}
	return items, time.Duration(ttlHours) * time.Hour, nil
}

// CompletePack records temporary part links and marks the download terminal
// in one transaction.
//
//nolint:cyclop // Completion validates the whole package and commits all links and status together.
func (r Downloads) CompletePack(ctx context.Context, downloadID int64, status string, parts []mediaapp.ZIPPart) error {
	if r.Pool == nil || ctx == nil || downloadID <= 0 || (status != "ready" && status != "ready_with_errors") || len(parts) == 0 {
		return errors.New("media: invalid ZIP completion")
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("media: begin ZIP completion: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	for _, part := range parts {
		if part.Number <= 0 || part.BlobKey == "" || part.Bytes < 0 || part.ExpiresAt.IsZero() {
			return errors.New("media: invalid ZIP part metadata")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO download_parts(download_id,part_number,blob_key,bytes,expires_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(download_id,part_number) DO UPDATE SET blob_key=EXCLUDED.blob_key,bytes=EXCLUDED.bytes,expires_at=EXCLUDED.expires_at`, downloadID, part.Number, part.BlobKey, part.Bytes, part.ExpiresAt); err != nil {
			return fmt.Errorf("media: save ZIP part: %w", err)
		}
	}
	tag, err := tx.Exec(ctx, `UPDATE downloads SET status=$2 WHERE id=$1`, downloadID, status)
	if err != nil {
		return fmt.Errorf("media: update packed download: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errors.New("media: download not found")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("media: commit ZIP completion: %w", err)
	}
	return nil
}

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
	byID := candidateMap(candidates)
	keyID, err := userKeyForPendingSizes(ctx, tx, download.UserID, download.Items, byID)
	if err != nil {
		return mediadomain.Download{}, err
	}
	if err := insertDownloadItems(ctx, tx, download); err != nil {
		return mediadomain.Download{}, err
	}
	if err := enqueueDownloadJobs(ctx, tx, r.Jobs, download, byID, keyID); err != nil {
		return mediadomain.Download{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return mediadomain.Download{}, fmt.Errorf("media: commit download: %w", err)
	}
	return download, nil
}

func candidateMap(candidates []mediaapp.DownloadItemCandidate) map[int64]mediaapp.DownloadItemCandidate {
	byID := make(map[int64]mediaapp.DownloadItemCandidate, len(candidates))
	for _, candidate := range candidates {
		byID[candidate.PhotoID] = candidate
	}
	return byID
}

func userKeyForPendingSizes(ctx context.Context, tx pgx.Tx, userID int64, items []mediadomain.DownloadItem, candidates map[int64]mediaapp.DownloadItemCandidate) (int64, error) {
	for _, item := range items {
		if !candidates[item.PhotoID].SizesConfirmed {
			var keyID int64
			err := tx.QueryRow(ctx, `SELECT fa.key_id FROM flickr_accounts fa JOIN api_keys k ON k.id=fa.key_id WHERE fa.user_id=$1 AND fa.status='ok' AND k.status='valid' ORDER BY fa.id LIMIT 1`, userID).Scan(&keyID)
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, mediaapp.ErrNoFlickrKey
			}
			if err != nil {
				return 0, fmt.Errorf("media: find user Flickr key: %w", err)
			}
			return keyID, nil
		}
	}
	return 0, nil
}

func insertDownloadItems(ctx context.Context, tx pgx.Tx, download mediadomain.Download) error {
	if len(download.Items) == 0 {
		return nil
	}
	rows := make([][]any, 0, len(download.Items))
	for _, item := range download.Items {
		rows = append(rows, []any{download.ID, item.PhotoID, string(item.State)})
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"download_items"}, []string{"download_id", "photo_id", "state"}, pgx.CopyFromRows(rows)); err != nil {
		return fmt.Errorf("media: insert download items: %w", err)
	}
	return nil
}

func enqueueDownloadJobs(ctx context.Context, tx pgx.Tx, jobs queue.JobQueue, download mediadomain.Download, candidates map[int64]mediaapp.DownloadItemCandidate, keyID int64) error {
	for _, item := range download.Items {
		candidate := candidates[item.PhotoID]
		kind, queueName := "photo.sizes", "sync"
		args := map[string]any{"photo_id": item.PhotoID, "owner_nsid": candidate.OwnerNSID, "facet": "sizes", "key_id": keyID, "class": string(quotadomain.Interactive), "dedupe_key": fmt.Sprintf("photo:%d:sizes", item.PhotoID)}
		if candidate.SizesConfirmed {
			kind, queueName = "media.fetch", "media"
			args = map[string]any{"download_id": download.ID, "photo_id": item.PhotoID}
		}
		encoded, _ := json.Marshal(args)
		if err := jobs.Enqueue(ctx, tx, queue.Job{Kind: kind, Args: encoded}, queue.Options{Queue: queueName}); err != nil {
			return fmt.Errorf("media: enqueue %s: %w", kind, err)
		}
	}
	return nil
}
