package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	mediaapp "github.com/jooservices/xflickr/internal/media/app"
	mediadomain "github.com/jooservices/xflickr/internal/media/domain"
	"github.com/jooservices/xflickr/internal/platform/queue"
	platformsse "github.com/jooservices/xflickr/internal/platform/sse"
	quotadomain "github.com/jooservices/xflickr/internal/quota/domain"
)

type Downloads struct {
	Pool   downloadsPool
	Jobs   queue.JobQueue
	Events interface {
		Publish(context.Context, platformsse.Event) error
	}
}

type downloadsPool interface {
	Begin(context.Context) (pgx.Tx, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
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

// PackedOriginals detects a committed package and lists its source blobs so a
// retry can finish cleanup without attempting to read already deleted files.
func (r Downloads) PackedOriginals(ctx context.Context, downloadID int64) ([]string, bool, error) {
	if r.Pool == nil || ctx == nil || downloadID <= 0 {
		return nil, false, errors.New("media: pack database, context, and download id are required")
	}
	var complete bool
	if err := r.Pool.QueryRow(ctx, `SELECT d.status IN ('ready','ready_with_errors') FROM downloads d WHERE d.id=$1`, downloadID).Scan(&complete); err != nil {
		return nil, false, fmt.Errorf("media: check existing ZIP parts: %w", err)
	}
	if !complete {
		return nil, false, nil
	}
	rows, err := r.Pool.Query(ctx, `SELECT a.blob_key FROM download_items di JOIN assets a ON a.id=di.asset_id WHERE di.download_id=$1 AND di.state='ready' AND a.blob_key<>'' ORDER BY di.photo_id`, downloadID)
	if err != nil {
		return nil, false, fmt.Errorf("media: load originals for ZIP cleanup: %w", err)
	}
	defer rows.Close()
	var originals []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, false, fmt.Errorf("media: scan original for ZIP cleanup: %w", err)
		}
		originals = append(originals, key)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("media: read originals for ZIP cleanup: %w", err)
	}
	return originals, true, nil
}

// SettleItem records the final result from a download worker and enqueues the
// packer in the same transaction when this was the last unsettled item.
//
//nolint:cyclop // Settlement validates the terminal state and commits item plus queue job atomically.
func (r Downloads) SettleItem(ctx context.Context, downloadID, photoID int64, state string, assetID *int64, failure string) error {
	if r.Pool == nil || r.Jobs == nil || ctx == nil || downloadID <= 0 || photoID <= 0 || (state != "ready" && state != "failed") || (state == "ready" && (assetID == nil || *assetID <= 0)) || (state == "failed" && failure == "") {
		return errors.New("media: invalid settled download item")
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("media: begin item settlement: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var lockedID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM downloads WHERE id=$1 FOR UPDATE`, downloadID).Scan(&lockedID); err != nil {
		return fmt.Errorf("media: lock download for item settlement: %w", err)
	}
	tag, err := tx.Exec(ctx, `UPDATE download_items SET state=$3,asset_id=$4,error=$5 WHERE download_id=$1 AND photo_id=$2 AND state NOT IN ('ready','failed')`, downloadID, photoID, state, assetID, failure)
	if err != nil {
		return fmt.Errorf("media: settle download item: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}
	var unsettled int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM download_items WHERE download_id=$1 AND state NOT IN ('ready','failed')`, downloadID).Scan(&unsettled); err != nil {
		return fmt.Errorf("media: check unsettled download items: %w", err)
	}
	if unsettled == 0 {
		args, _ := json.Marshal(map[string]int64{"download_id": downloadID})
		if err := r.Jobs.Enqueue(ctx, tx, queue.Job{Kind: "media.pack", Args: args}, queue.Options{Queue: "media", MaxAttempts: 3, LeaseTimeout: 2 * time.Hour}); err != nil {
			return fmt.Errorf("media: enqueue pack job: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("media: commit item settlement: %w", err)
	}
	r.publishProgress(ctx, downloadID)
	return nil
}

func (r Downloads) publishProgress(ctx context.Context, downloadID int64) {
	if r.Events == nil {
		return
	}
	var userID, total, settled int64
	if err := r.Pool.QueryRow(ctx, `SELECT user_id,(SELECT count(*) FROM download_items WHERE download_id=$1),(SELECT count(*) FROM download_items WHERE download_id=$1 AND state IN ('ready','failed')) FROM downloads WHERE id=$1`, downloadID).Scan(&userID, &total, &settled); err != nil {
		slog.WarnContext(ctx, "load download progress event failed", "download_id", downloadID, "error", err)
		return
	}
	data, _ := json.Marshal(map[string]int64{"download_id": downloadID, "completed": settled, "total": total})
	if err := r.Events.Publish(ctx, platformsse.Event{UserID: userID, Type: "download.progress", Data: data}); err != nil {
		slog.WarnContext(ctx, "publish download progress event failed", "download_id", downloadID, "error", err)
	}
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
	partNumbers := make([]int32, 0, len(parts))
	for _, part := range parts {
		if part.Number <= 0 || uint64(part.Number) > uint64(int32(^uint32(0)>>1)) || part.BlobKey == "" || part.Bytes < 0 || part.ExpiresAt.IsZero() {
			return errors.New("media: invalid ZIP part metadata")
		}
		partNumbers = append(partNumbers, int32(part.Number))
		if _, err := tx.Exec(ctx, `INSERT INTO download_parts(download_id,part_number,blob_key,bytes,expires_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(download_id,part_number) DO UPDATE SET blob_key=EXCLUDED.blob_key,bytes=EXCLUDED.bytes,expires_at=EXCLUDED.expires_at`, downloadID, part.Number, part.BlobKey, part.Bytes, part.ExpiresAt); err != nil {
			return fmt.Errorf("media: save ZIP part: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM download_parts WHERE download_id=$1 AND NOT (part_number = ANY($2))`, downloadID, partNumbers); err != nil {
		return fmt.Errorf("media: remove stale ZIP part metadata: %w", err)
	}
	var userID int64
	err = tx.QueryRow(ctx, `UPDATE downloads SET status=$2 WHERE id=$1 RETURNING user_id`, downloadID, status).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("media: download not found")
	}
	if err != nil {
		return fmt.Errorf("media: update packed download: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("media: commit ZIP completion: %w", err)
	}
	if r.Events != nil {
		data, _ := json.Marshal(map[string]any{"download_id": downloadID, "status": status})
		if err := r.Events.Publish(ctx, platformsse.Event{UserID: userID, Type: "download.ready", Data: data}); err != nil {
			slog.WarnContext(ctx, "publish download ready event failed", "download_id", downloadID, "error", err)
		}
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
	if r.Events != nil {
		data, _ := json.Marshal(map[string]int64{"download_id": download.ID, "completed": 0, "total": int64(len(download.Items))})
		if err := r.Events.Publish(ctx, platformsse.Event{UserID: download.UserID, Type: "download.progress", Data: data}); err != nil {
			slog.WarnContext(ctx, "publish initial download progress event failed", "download_id", download.ID, "error", err)
		}
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
