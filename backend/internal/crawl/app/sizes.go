package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
	quotadomain "github.com/jooservices/xflickr/internal/quota/domain"
	"github.com/jooservices/xflickr/pkg/flickr"
)

// SizesStore reads a photo and stores terminal sizes outcomes.
type SizesStore interface {
	LoadSizes(ctx context.Context, photoID int64) (catalog.Photo, bool, error)
	SetSizes(ctx context.Context, photoID int64, sizes []byte, status catalog.SizesStatus, checkedAt time.Time) error
}

// SizesClient performs one photos.getSizes call using the selected key.
type SizesClient interface {
	GetSizes(ctx context.Context, keyID int64, photoID string) ([]flickr.PhotoSize, error)
}

type ClassifiedSizesClient interface {
	GetSizesClass(ctx context.Context, keyID int64, photoID string, class quotadomain.Class) ([]flickr.PhotoSize, error)
}

// QuotaWaitError tells the job runner to hold a sizes job until quota resets
// instead of failing it. It unwraps to flickr.ErrQuotaExceeded.
type QuotaWaitError struct {
	Wait  time.Duration
	Cause error
}

func (e *QuotaWaitError) Error() string { return e.Cause.Error() }

func (e *QuotaWaitError) Unwrap() error { return e.Cause }

// NewQuotaWaitError wraps cause with the delay before the next attempt. A
// non-positive wait falls back to the default quota wait.
func NewQuotaWaitError(cause error, wait time.Duration) *QuotaWaitError {
	if wait <= 0 {
		wait = DefaultTiming.QuotaWait
	}
	return &QuotaWaitError{Wait: wait, Cause: cause}
}

// Sizes handles one photo.sizes River job.
type Sizes struct {
	Store  SizesStore
	Flickr SizesClient
	Now    func() time.Time
}

// SizesJob is the relevant portion of the photo lane's durable job args.
type SizesJob struct {
	PhotoID int64  `json:"photo_id"`
	KeyID   int64  `json:"key_id"`
	Class   string `json:"class,omitempty"`
}

// ParseSizesJob validates a photo.sizes job payload.
func ParseSizesJob(args []byte) (SizesJob, error) {
	var job SizesJob
	if err := json.Unmarshal(args, &job); err != nil || job.PhotoID <= 0 || job.KeyID <= 0 {
		return SizesJob{}, errors.New("crawl: photo.sizes job needs photo_id and key_id")
	}
	if job.Class != "" && job.Class != string(quotadomain.Photo) && job.Class != string(quotadomain.Interactive) {
		return SizesJob{}, errors.New("crawl: photo.sizes job has an invalid quota class")
	}
	return job, nil
}

// Handle retrieves and confirms one photo's Flickr sizes. Unavailable photos
// are recorded as terminal so the photo lane will not retry them.
func (s Sizes) Handle(ctx context.Context, job SizesJob) error {
	if err := s.ready(ctx, job); err != nil {
		return err
	}
	photo, exists, err := s.Store.LoadSizes(ctx, job.PhotoID)
	if err != nil {
		return fmt.Errorf("crawl: load photo %d sizes state: %w", job.PhotoID, err)
	}
	if shouldSkipSizes(photo, exists) {
		return nil
	}
	var sizes []flickr.PhotoSize
	if classified, ok := s.Flickr.(ClassifiedSizesClient); ok && job.Class != "" {
		sizes, err = classified.GetSizesClass(ctx, job.KeyID, fmt.Sprint(job.PhotoID), quotadomain.Class(job.Class))
	} else {
		sizes, err = s.Flickr.GetSizes(ctx, job.KeyID, fmt.Sprint(job.PhotoID))
	}
	if err != nil {
		return s.recordFlickrError(ctx, job.PhotoID, err)
	}
	encoded, err := json.Marshal(sizes)
	if err != nil {
		return fmt.Errorf("crawl: encode sizes for photo %d: %w", job.PhotoID, err)
	}
	return s.Store.SetSizes(ctx, job.PhotoID, encoded, catalog.SizesConfirmed, s.now())
}

func (s Sizes) ready(ctx context.Context, job SizesJob) error {
	if ctx == nil || s.Store == nil || s.Flickr == nil || job.PhotoID <= 0 || job.KeyID <= 0 {
		return errors.New("crawl: photo.sizes dependencies and identifiers are required")
	}
	return ctx.Err()
}

func shouldSkipSizes(photo catalog.Photo, exists bool) bool {
	if !exists || photo.SizesStatus == catalog.SizesUnavailable {
		return true
	}
	return photo.SizesStatus == catalog.SizesConfirmed && photo.SizesCheckedAt != nil && !photo.SizesCheckedAt.Before(photo.LastUpdate)
}

func (s Sizes) recordFlickrError(ctx context.Context, photoID int64, cause error) error {
	if errors.Is(cause, flickr.ErrNotFound) || errors.Is(cause, flickr.ErrPermission) {
		return s.Store.SetSizes(ctx, photoID, []byte("[]"), catalog.SizesUnavailable, s.now())
	}
	if Classify(cause) == OutcomeWaitQuota {
		var waiting *QuotaWaitError
		if !errors.As(cause, &waiting) {
			cause = NewQuotaWaitError(cause, 0)
		}
	}
	return fmt.Errorf("crawl: get sizes for photo %d: %w", photoID, cause)
}

func (s Sizes) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
