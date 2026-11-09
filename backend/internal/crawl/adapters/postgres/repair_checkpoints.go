package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CheckpointRepairOptions bounds one offline repair run.
type CheckpointRepairOptions struct {
	BatchSize int
	Now       func() time.Time
	Logger    *slog.Logger
}

// CheckpointRepairReport is safe to print; it never includes secrets.
type CheckpointRepairReport struct {
	PhotosUpdated          int64     `json:"photos_updated"`
	PhotosLegacyCatchup    int64     `json:"photos_legacy_catchup"`
	PhotosUnrepairable     int64     `json:"photos_unrepairable"`
	FavoritesUpdated       int64     `json:"favorites_updated"`
	FavoritesLegacyCatchup int64     `json:"favorites_legacy_catchup"`
	FavoritesUnrepairable  int64     `json:"favorites_unrepairable"`
	Batches                int       `json:"batches"`
	RunAt                  time.Time `json:"run_at"`
}

// CheckpointRepairTx is the transaction surface used by one repair batch.
type CheckpointRepairTx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
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

type repairCursor struct {
	UserID     int64
	PersonNSID string
}

type repairBatch struct {
	Selected      int64
	Updated       int64
	LegacyCatchup int64
	Unrepairable  int64
	Last          repairCursor
}

// RepairZeroCheckpoints fills zero checkpoints from same-observer completion
// markers, with a conservative catch-up floor for legacy done work. It makes
// no Flickr calls and leaves rows without matching work at zero.
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
	var after repairCursor
	for {
		batch, err := repairPhotosBatch(ctx, database, options.BatchSize, after)
		if err != nil {
			return report, err
		}
		report.PhotosUpdated += batch.Updated
		report.PhotosLegacyCatchup += batch.LegacyCatchup
		report.PhotosUnrepairable += batch.Unrepairable
		report.Batches++
		if batch.Selected == 0 {
			break
		}
		if err := validateRepairProgress(after, batch); err != nil {
			return report, err
		}
		after = batch.Last
		logger.InfoContext(ctx, "crawl repaired photo checkpoints", "updated", batch.Updated, "legacy_catchup", batch.LegacyCatchup, "unrepairable", batch.Unrepairable, "batch_size", options.BatchSize)
	}
	after = repairCursor{}
	for {
		batch, err := repairFavoritesBatch(ctx, database, options.BatchSize, after)
		if err != nil {
			return report, err
		}
		report.FavoritesUpdated += batch.Updated
		report.FavoritesLegacyCatchup += batch.LegacyCatchup
		report.FavoritesUnrepairable += batch.Unrepairable
		report.Batches++
		if batch.Selected == 0 {
			break
		}
		if err := validateRepairProgress(after, batch); err != nil {
			return report, err
		}
		after = batch.Last
		logger.InfoContext(ctx, "crawl repaired favorite checkpoints", "updated", batch.Updated, "legacy_catchup", batch.LegacyCatchup, "unrepairable", batch.Unrepairable, "batch_size", options.BatchSize)
	}
	return report, nil
}

func validateRepairProgress(after repairCursor, batch repairBatch) error {
	if batch.Selected < 0 || batch.Updated < 0 || batch.LegacyCatchup < 0 || batch.LegacyCatchup > batch.Updated || batch.Unrepairable < 0 || batch.Updated+batch.Unrepairable != batch.Selected {
		return errors.New("crawl: checkpoint repair returned invalid batch counts")
	}
	if batch.Last.UserID <= 0 || batch.Last.PersonNSID == "" {
		return errors.New("crawl: checkpoint repair batch did not advance its keyset cursor")
	}
	if after.UserID > 0 && (batch.Last.UserID < after.UserID ||
		(batch.Last.UserID == after.UserID && batch.Last.PersonNSID <= after.PersonNSID)) {
		return errors.New("crawl: checkpoint repair keyset cursor did not advance")
	}
	return nil
}

func repairPhotosBatch(ctx context.Context, database CheckpointRepairDB, batchSize int, after repairCursor) (repairBatch, error) {
	return repairZeroCheckpointsBatch(ctx, database, batchSize, after, "photos")
}

func repairFavoritesBatch(ctx context.Context, database CheckpointRepairDB, batchSize int, after repairCursor) (repairBatch, error) {
	return repairZeroCheckpointsBatch(ctx, database, batchSize, after, "favorites")
}

const repairCheckpointBatchSQL = `
WITH batch AS MATERIALIZED (
	SELECT user_id, person_nsid
	FROM crawl_interests
	WHERE (%[1]s IS NULL OR %[1]s <= 'epoch'::timestamptz)
	  AND ($2::bigint = 0 OR (user_id, person_nsid) > ($2::bigint, $3::text))
	ORDER BY user_id, person_nsid
	LIMIT $1
	FOR UPDATE
), computed AS MATERIALIZED (
	SELECT b.user_id, b.person_nsid,
		COALESCE(completed.checkpoint, CASE WHEN legacy.found THEN 1 ELSE 0 END) AS checkpoint,
		completed.checkpoint IS NULL AND legacy.found AS legacy_catchup
	FROM batch b
	LEFT JOIN LATERAL (
		SELECT (w.cursor->>'checkpoint_at')::bigint AS checkpoint
		FROM work_items w
		WHERE w.kind = $4
		  AND w.subject_key = b.person_nsid
		  AND w.user_id = b.user_id
		  AND w.state = 'done'
		  AND COALESCE((w.cursor->>'scan_completed_at')::bigint, 0) > 0
		  AND COALESCE((w.cursor->>'checkpoint_at')::bigint, 0) > 0
		LIMIT 1
	) completed ON true
	LEFT JOIN LATERAL (
		SELECT EXISTS (
			SELECT 1
			FROM work_items w
			WHERE w.kind = $4
			  AND w.subject_key = b.person_nsid
			  AND w.user_id = b.user_id
			  AND w.state = 'done'
			  AND COALESCE((w.cursor->>'scan_completed_at')::bigint, 0) <= 0
		) AS found
	) legacy ON true
), updated AS (
	UPDATE crawl_interests AS interest
	SET %[1]s = to_timestamp(computed.checkpoint)
	FROM computed
	WHERE interest.user_id = computed.user_id
	  AND interest.person_nsid = computed.person_nsid
	  AND computed.checkpoint > 0
	  AND (interest.%[1]s IS NULL OR interest.%[1]s <= 'epoch'::timestamptz)
	RETURNING interest.user_id, interest.person_nsid
)
SELECT
	(SELECT count(*) FROM batch),
	(SELECT count(*) FROM updated),
	(SELECT count(*) FROM computed c JOIN updated u USING (user_id, person_nsid) WHERE c.legacy_catchup),
	(SELECT count(*) FROM computed WHERE checkpoint <= 0),
	COALESCE((SELECT user_id FROM batch ORDER BY user_id DESC, person_nsid DESC LIMIT 1), 0),
	COALESCE((SELECT person_nsid FROM batch ORDER BY user_id DESC, person_nsid DESC LIMIT 1), '')`

func repairZeroCheckpointsBatch(ctx context.Context, database CheckpointRepairDB, batchSize int, after repairCursor, kind string) (repairBatch, error) {
	var column string
	switch kind {
	case "photos":
		column = "last_seen_at"
	case "favorites":
		column = "last_fave_at"
	default:
		return repairBatch{}, errors.New("crawl: unsupported checkpoint repair kind")
	}
	tx, err := database.Begin(ctx)
	if err != nil {
		return repairBatch{}, fmt.Errorf("crawl: begin %s checkpoint repair: %w", kind, err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // safe after commit
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '500ms'`); err != nil {
		return repairBatch{}, fmt.Errorf("crawl: set %s checkpoint repair lock timeout: %w", kind, err)
	}
	var batch repairBatch
	err = tx.QueryRow(ctx, fmt.Sprintf(repairCheckpointBatchSQL, column), batchSize, after.UserID, after.PersonNSID, kind).Scan(
		&batch.Selected, &batch.Updated, &batch.LegacyCatchup, &batch.Unrepairable, &batch.Last.UserID, &batch.Last.PersonNSID,
	)
	if err != nil {
		return repairBatch{}, fmt.Errorf("crawl: repair %s checkpoints: %w", kind, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return repairBatch{}, fmt.Errorf("crawl: commit %s checkpoint repair: %w", kind, err)
	}
	return batch, nil
}

// RepairZeroCheckpointsPool runs the repair against a pool.
func RepairZeroCheckpointsPool(ctx context.Context, pool *pgxpool.Pool, options CheckpointRepairOptions) (CheckpointRepairReport, error) {
	if pool == nil {
		return CheckpointRepairReport{}, errors.New("crawl: repair-checkpoints requires a database pool")
	}
	return RepairZeroCheckpoints(ctx, poolRepairDB{pool: pool}, options)
}
