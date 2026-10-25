package river

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/platform/queue"
	"github.com/jooservices/xflickr/internal/schedule/app"
)

// PhotoJobKind is the queue job kind of a photo-lane job.
const PhotoJobKind = "photo.sizes"

// PhotoQueue is the queue photo-lane jobs wait in.
const PhotoQueue = "sync"

type photoTxBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

type photoArgs struct {
	PhotoID   int64  `json:"photo_id"`
	OwnerNSID string `json:"owner_nsid"`
	Facet     string `json:"facet"`
	KeyID     int64  `json:"key_id"`
	Class     string `json:"class"`
	DedupeKey string `json:"dedupe_key"`
}

// PhotoEnqueuer inserts one River job per photo and skips a photo whose dedupe
// key is already waiting in the ready queue.
type PhotoEnqueuer struct {
	Pool    photoTxBeginner
	Jobs    queue.JobQueue
	Options queue.Options
}

var _ app.PhotoEnqueuer = PhotoEnqueuer{}

// EnqueuePhoto reports whether a job was inserted. A transaction-scoped
// advisory lock on the dedupe key serialises concurrent producers, so the
// existence check and the insert cannot interleave.
func (e PhotoEnqueuer) EnqueuePhoto(ctx context.Context, job app.PhotoJob) (bool, error) {
	if e.Pool == nil || e.Jobs == nil || ctx == nil {
		return false, errors.New("schedule: photo enqueuer dependencies are required")
	}
	if job.DedupeKey == "" || job.PhotoID <= 0 || job.KeyID <= 0 {
		return false, errors.New("schedule: photo job needs a photo, key and dedupe key")
	}
	args, err := json.Marshal(photoArgs{PhotoID: job.PhotoID, OwnerNSID: job.OwnerNSID, Facet: job.Facet, KeyID: job.KeyID, Class: string(job.Class), DedupeKey: job.DedupeKey})
	if err != nil {
		return false, fmt.Errorf("schedule: encode photo job: %w", err)
	}
	opts := e.Options
	if opts.Queue == "" {
		opts.Queue = PhotoQueue
	}
	tx, err := e.Pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("schedule: begin photo enqueue: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, job.DedupeKey); err != nil {
		return false, fmt.Errorf("schedule: lock photo dedupe key: %w", err)
	}
	var queued bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM river_job WHERE state IN ('available','pending','scheduled','retryable','running') AND args->'args'->>'dedupe_key' = $1)`, job.DedupeKey).Scan(&queued); err != nil {
		return false, fmt.Errorf("schedule: check photo dedupe key: %w", err)
	}
	if queued {
		return false, nil
	}
	if err := e.Jobs.Enqueue(ctx, tx, queue.Job{Kind: PhotoJobKind, Args: args}, opts); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("schedule: commit photo enqueue: %w", err)
	}
	return true, nil
}
