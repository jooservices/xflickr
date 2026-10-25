package app

import (
	"context"
	"errors"
	"fmt"

	catalogapp "github.com/jooservices/xflickr/internal/catalog/app"
	quotaapp "github.com/jooservices/xflickr/internal/quota/app"
	quotadomain "github.com/jooservices/xflickr/internal/quota/domain"
)

// PhotoFacet is the facet fetched for every photo in the photo lane.
const PhotoFacet = "sizes"

// maxPhotoPages bounds how far Next pages past photos it cannot enqueue, so a
// tick never scans an unbounded backlog.
const maxPhotoPages = 8

// PhotoJob is one photo-lane job. DedupeKey is "photo:<id>:<facet>".
type PhotoJob struct {
	PhotoID   int64
	OwnerNSID string
	Facet     string
	KeyID     int64
	Class     quotadomain.Class
	DedupeKey string
}

// PhotoEnqueuer queues a photo job. It reports false, without error, when a
// job with the same dedupe key is already waiting in the ready queue.
type PhotoEnqueuer interface {
	EnqueuePhoto(context.Context, PhotoJob) (bool, error)
}

// PhotoKeyChooser picks the key that pays for a call about a person.
type PhotoKeyChooser interface {
	Choose(ctx context.Context, personNSID string, class quotadomain.Class) (quotaapp.KeyID, error)
}

// PhotoLane turns the catalog into photo-lane jobs without a backlog table:
// photos with unconfirmed sizes are read straight from the catalog.
type PhotoLane struct {
	Selector catalogapp.PhotoSelector
	Keys     PhotoKeyChooser
	Queue    PhotoEnqueuer
}

// PhotoDedupeKey is the ready-queue dedupe key of a photo facet.
func PhotoDedupeKey(photoID int64, facet string) string {
	return fmt.Sprintf("photo:%d:%s", photoID, facet)
}

// Next enqueues up to n photos in priority order and returns their ids. Each
// photo is charged to a key chosen from the users interested in its owner.
// Photos already queued and photos with no eligible key are passed over.
func (l PhotoLane) Next(ctx context.Context, n int) ([]int64, error) {
	if ctx == nil || l.Selector == nil || l.Keys == nil || l.Queue == nil {
		return nil, errors.New("schedule: photo lane dependencies are required")
	}
	if n < 1 {
		return nil, nil
	}
	ids := make([]int64, 0, n)
	skip := 0
	for page := 0; page < maxPhotoPages && len(ids) < n; page++ {
		refs, err := l.Selector.Select(ctx, n-len(ids), skip)
		if err != nil {
			return ids, err
		}
		if len(refs) == 0 {
			break
		}
		skip += len(refs)
		for _, ref := range refs {
			queued, err := l.enqueue(ctx, ref)
			if err != nil {
				return ids, err
			}
			if queued {
				ids = append(ids, ref.ID)
			}
		}
	}
	return ids, nil
}

func (l PhotoLane) enqueue(ctx context.Context, ref catalogapp.PhotoRef) (bool, error) {
	key, err := l.Keys.Choose(ctx, ref.OwnerNSID, quotadomain.Photo)
	if errors.Is(err, quotaapp.ErrNoKey) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("schedule: choose key for photo %d: %w", ref.ID, err)
	}
	queued, err := l.Queue.EnqueuePhoto(ctx, PhotoJob{
		PhotoID: ref.ID, OwnerNSID: ref.OwnerNSID, Facet: PhotoFacet, KeyID: int64(key),
		Class: quotadomain.Photo, DedupeKey: PhotoDedupeKey(ref.ID, PhotoFacet),
	})
	if err != nil {
		return false, fmt.Errorf("schedule: enqueue photo %d: %w", ref.ID, err)
	}
	return queued, nil
}
