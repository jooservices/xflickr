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

// PhotoJobKind is the queue job kind of a size confirmation job.
const PhotoJobKind = "photo.sizes"

// PhotoQueue is the queue photo-lane jobs wait in.
const PhotoQueue = "sync"

// PhotoFacetJobKind is the queue job kind of an optional photo facet.
const PhotoFacetJobKind = "photo.facet"

// PhotoExtrasQueue runs optional facets separately so they cannot sit ahead
// of size confirmation work.
const PhotoExtrasQueue = "sync-low"

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
	Pool     photoTxBeginner
	Jobs     queue.JobQueue
	Options  queue.Options
	ReadyCap int
}

var _ app.PhotoEnqueuer = PhotoEnqueuer{}

// EnqueuePhoto reports whether a job was inserted. A transaction-scoped
// advisory lock on the dedupe key serialises concurrent producers, so the
// existence check and the insert cannot interleave.
//
//nolint:cyclop // Transaction setup, dedupe, ready-cap, insertion, and commit must remain atomic here.
func (e PhotoEnqueuer) EnqueuePhoto(ctx context.Context, job app.PhotoJob) (bool, error) {
	if e.Pool == nil || e.Jobs == nil || ctx == nil {
		return false, errors.New("schedule: photo enqueuer dependencies are required")
	}
	if job.DedupeKey == "" || job.PhotoID <= 0 || job.KeyID <= 0 {
		return false, errors.New("schedule: photo job needs a photo, key and dedupe key")
	}
	facet := job.Facet
	if facet == "" {
		facet = app.PhotoFacet
	}
	kind, queueName := PhotoJobKind, PhotoQueue
	if facet != app.PhotoFacet {
		if !isOptionalPhotoFacet(facet) {
			return false, errors.New("schedule: photo job has an unsupported facet")
		}
		kind, queueName = PhotoFacetJobKind, PhotoExtrasQueue
	}
	args, err := json.Marshal(photoArgs{PhotoID: job.PhotoID, OwnerNSID: job.OwnerNSID, Facet: facet, KeyID: job.KeyID, Class: string(job.Class), DedupeKey: job.DedupeKey})
	if err != nil {
		return false, fmt.Errorf("schedule: encode photo job: %w", err)
	}
	opts := e.Options
	if kind == PhotoFacetJobKind || opts.Queue == "" {
		opts.Queue = queueName
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
	if e.ReadyCap > 0 {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('photo-lane.ready-cap', 0))`); err != nil {
			return false, fmt.Errorf("schedule: lock photo size capacity: %w", err)
		}
		var sizeActive, active int
		if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE args->>'kind'=$1), count(*)
			FROM river_job
			WHERE args->>'kind' IN ($1,$2) AND args->>'queue' IN ($3,$4)
			AND state IN ('available','pending','scheduled','retryable','running')`, PhotoJobKind, PhotoFacetJobKind, PhotoQueue, PhotoExtrasQueue).Scan(&sizeActive, &active); err != nil {
			return false, fmt.Errorf("schedule: count photo size jobs: %w", err)
		}
		// Optional work may use unclaimed capacity, but it cannot consume the
		// size-job reservation. Sizes remain enqueueable up to the configured cap
		// even when the lower-priority queue is full.
		if (kind == PhotoJobKind && sizeActive >= e.ReadyCap) || (kind == PhotoFacetJobKind && active >= e.ReadyCap) {
			return false, nil
		}
	}
	if err := e.Jobs.Enqueue(ctx, tx, queue.Job{Kind: kind, Args: args}, opts); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("schedule: commit photo enqueue: %w", err)
	}
	return true, nil
}

func isOptionalPhotoFacet(facet string) bool {
	switch facet {
	case app.PhotoInfoFacet, app.PhotoExifFacet, app.PhotoContextsFacet, app.PhotoCommentsFacet, app.PhotoFavoritesFacet:
		return true
	default:
		return false
	}
}
