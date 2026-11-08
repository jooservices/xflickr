package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CheckpointRepairMargin subtracts from the latest stored upload or favorite
// date when deriving a checkpoint from catalog rows that lack a prior scan
// observation time. One hour covers Flickr second precision and ordinary
// clock skew without skipping a large window of new activity.
const CheckpointRepairMargin = time.Hour

// CheckpointRepairOptions bounds one offline repair run.
type CheckpointRepairOptions struct {
	BatchSize int
	Now       func() time.Time
	Logger    *slog.Logger
}

// CheckpointRepairReport is safe to print; it never includes secrets.
type CheckpointRepairReport struct {
	PhotosUpdated    int64     `json:"photos_updated"`
	FavoritesUpdated int64     `json:"favorites_updated"`
	Batches          int       `json:"batches"`
	RunAt            time.Time `json:"run_at"`
}

// CheckpointRepairTx is the transaction surface used by one repair batch.
type CheckpointRepairTx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Commit(context.Context) error
	Rollback(context.Context) error
}

// CheckpointRepairDB starts each bounded batch in its own transaction.
type CheckpointRepairDB interface {
	Begin(context.Context) (CheckpointRepairTx, error)
}

type poolRepairDB struct{ pool *pgxpool.Pool }

func (p poolRepairDB) Begin(ctx context.Context) (CheckpointRepairTx, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return tx, nil
}

// RepairZeroCheckpoints fills null/zero photos and favorites checkpoints from
// already stored catalog data. It never calls Flickr, never changes a non-zero
// checkpoint, and is safe to run while the crawl role is active (SKIP LOCKED).
func RepairZeroCheckpoints(ctx context.Context, database CheckpointRepairDB, options CheckpointRepairOptions) (CheckpointRepairReport, error) {
	now := time.Now().UTC()
	if options.Now != nil {
		now = options.Now().UTC()
	}
	report := CheckpointRepairReport{RunAt: now}
	if ctx == nil || database == nil || options.BatchSize < 1 || options.BatchSize > 500 {
		return report, errors.New("crawl: repair-checkpoints requires database and batch size 1..500")
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	for {
		n, err := repairPhotosBatch(ctx, database, options.BatchSize)
		if err != nil {
			return report, err
		}
		report.PhotosUpdated += n
		report.Batches++
		if n == 0 {
			break
		}
		logger.InfoContext(ctx, "crawl repaired photo checkpoints", "updated", n, "batch_size", options.BatchSize)
	}
	for {
		n, err := repairFavoritesBatch(ctx, database, options.BatchSize)
		if err != nil {
			return report, err
		}
		report.FavoritesUpdated += n
		report.Batches++
		if n == 0 {
			break
		}
		logger.InfoContext(ctx, "crawl repaired favorite checkpoints", "updated", n, "batch_size", options.BatchSize)
	}
	return report, nil
}

func repairPhotosBatch(ctx context.Context, database CheckpointRepairDB, batchSize int) (int64, error) {
	tx, err := database.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("crawl: begin photo checkpoint repair: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // safe after commit
	tag, err := tx.Exec(ctx, `
WITH batch AS (
	SELECT user_id, person_nsid
	FROM crawl_interests
	WHERE last_seen_at IS NULL OR last_seen_at <= 'epoch'::timestamptz
	ORDER BY user_id, person_nsid
	LIMIT $1
	FOR UPDATE SKIP LOCKED
), computed AS (
	SELECT b.user_id, b.person_nsid,
		COALESCE(
			(SELECT MAX(p.last_seen_at) FROM photos p
			 WHERE p.owner_nsid = b.person_nsid
			   AND p.last_seen_at IS NOT NULL
			   AND p.last_seen_at > 'epoch'::timestamptz),
			(SELECT MAX(p.uploaded_at) - $2::interval FROM photos p
			 WHERE p.owner_nsid = b.person_nsid AND p.uploaded_at IS NOT NULL)
		) AS checkpoint
	FROM batch b
)
UPDATE crawl_interests AS interest SET last_seen_at = computed.checkpoint
FROM computed
WHERE interest.user_id = computed.user_id
  AND interest.person_nsid = computed.person_nsid
  AND computed.checkpoint IS NOT NULL
  AND computed.checkpoint > 'epoch'::timestamptz
  AND (interest.last_seen_at IS NULL OR interest.last_seen_at <= 'epoch'::timestamptz)`, batchSize, CheckpointRepairMargin)
	if err != nil {
		return 0, fmt.Errorf("crawl: repair photo checkpoints: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("crawl: commit photo checkpoint repair: %w", err)
	}
	return tag.RowsAffected(), nil
}

func repairFavoritesBatch(ctx context.Context, database CheckpointRepairDB, batchSize int) (int64, error) {
	tx, err := database.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("crawl: begin favorite checkpoint repair: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // safe after commit
	tag, err := tx.Exec(ctx, `
WITH batch AS (
	SELECT user_id, person_nsid
	FROM crawl_interests
	WHERE last_fave_at IS NULL OR last_fave_at <= 'epoch'::timestamptz
	ORDER BY user_id, person_nsid
	LIMIT $1
	FOR UPDATE SKIP LOCKED
), computed AS (
	SELECT b.user_id, b.person_nsid,
		COALESCE(
			(SELECT MAX(f.seen_at) FROM favorites f
			 WHERE f.person_nsid = b.person_nsid
			   AND f.seen_at IS NOT NULL
			   AND f.seen_at > 'epoch'::timestamptz),
			(SELECT MAX(f.fave_date) - $2::interval FROM favorites f
			 WHERE f.person_nsid = b.person_nsid)
		) AS checkpoint
	FROM batch b
)
UPDATE crawl_interests AS interest SET last_fave_at = computed.checkpoint
FROM computed
WHERE interest.user_id = computed.user_id
  AND interest.person_nsid = computed.person_nsid
  AND computed.checkpoint IS NOT NULL
  AND computed.checkpoint > 'epoch'::timestamptz
  AND (interest.last_fave_at IS NULL OR interest.last_fave_at <= 'epoch'::timestamptz)`, batchSize, CheckpointRepairMargin)
	if err != nil {
		return 0, fmt.Errorf("crawl: repair favorite checkpoints: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("crawl: commit favorite checkpoint repair: %w", err)
	}
	return tag.RowsAffected(), nil
}

// RepairZeroCheckpointsPool runs the repair against a pool.
func RepairZeroCheckpointsPool(ctx context.Context, pool *pgxpool.Pool, options CheckpointRepairOptions) (CheckpointRepairReport, error) {
	if pool == nil {
		return CheckpointRepairReport{}, errors.New("crawl: repair-checkpoints requires a database pool")
	}
	return RepairZeroCheckpoints(ctx, poolRepairDB{pool: pool}, options)
}

