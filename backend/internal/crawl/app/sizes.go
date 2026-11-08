package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
	crawldomain "github.com/jooservices/xflickr/internal/crawl/domain"
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

// ClassifiedSizesClient charges getSizes to an explicit quota class.
type ClassifiedSizesClient interface {
	GetSizesClass(ctx context.Context, keyID int64, photoID string, class quotadomain.Class) ([]flickr.PhotoSize, error)
}

// AuthenticatedSizesClient signs getSizes with a connected account token.
type AuthenticatedSizesClient interface {
	GetSizesForAccount(ctx context.Context, keyID, accountID int64, photoID string, class quotadomain.Class) ([]flickr.PhotoSize, error)
}

// KeyUser resolves the XFlickr user that owns an API key.
type KeyUser interface {
	UserIDForKey(ctx context.Context, keyID int64) (int64, error)
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
	States EntityStates
	Store  SizesStore
	Flickr SizesClient
	Auth   crawldomain.SizesAuthenticator
	Users  KeyUser
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
		if !exists {
			return nil
		}
		return s.reannounce(ctx, job.PhotoID, photo)
	}
	access := EntityAccess{EntityRef: EntityRef{Kind: "photo", ID: fmt.Sprint(job.PhotoID)}, Facet: "sizes", KeyID: job.KeyID, Version: photo.LastUpdate}
	placeholder := photo.HasRestrictedPlaceholder()
	if !placeholder {
		if s.States != nil {
			blocked, err := s.States.Suppressed(ctx, access, s.now())
			if err != nil {
				return err
			}
			if blocked {
				return s.Store.SetSizes(ctx, job.PhotoID, []byte("[]"), catalog.SizesUnavailable, s.now())
			}
		}
		sizes, err := s.fetchAnonymous(ctx, job)
		if err == nil {
			return s.confirm(ctx, job.PhotoID, sizes)
		}
		if !sizesRestricted(err) {
			return s.resolveFailure(ctx, job.PhotoID, access, err)
		}
		return s.retryAuthenticated(ctx, job, photo, true, err)
	}
	// Flickr lists restricted media with server 65535. Skip anonymous getSizes
	// and retry once with a connected account that may view the photo.
	return s.retryAuthenticated(ctx, job, photo, false, nil)
}

func (s Sizes) fetchAnonymous(ctx context.Context, job SizesJob) ([]flickr.PhotoSize, error) {
	if classified, ok := s.Flickr.(ClassifiedSizesClient); ok && job.Class != "" {
		return classified.GetSizesClass(ctx, job.KeyID, fmt.Sprint(job.PhotoID), quotadomain.Class(job.Class))
	}
	return s.Flickr.GetSizes(ctx, job.KeyID, fmt.Sprint(job.PhotoID))
}

func (s Sizes) retryAuthenticated(ctx context.Context, job SizesJob, photo catalog.Photo, restricted bool, anonymousErr error) error {
	decision, err := s.decideAuth(ctx, job, photo, restricted)
	if err != nil {
		return err
	}
	switch decision.Kind {
	case SizesAuthAccount:
		return s.fetchAuthenticated(ctx, job, photo, decision.Candidate)
	default:
		if anonymousErr != nil {
			access := EntityAccess{EntityRef: EntityRef{Kind: "photo", ID: fmt.Sprint(job.PhotoID)}, Facet: "sizes", KeyID: job.KeyID, Version: photo.LastUpdate}
			return s.resolveFailure(ctx, job.PhotoID, access, anonymousErr)
		}
		return s.Store.SetSizes(ctx, job.PhotoID, []byte("[]"), catalog.SizesUnavailable, s.now())
	}
}

func (s Sizes) decideAuth(ctx context.Context, job SizesJob, photo catalog.Photo, restricted bool) (SizesAuthDecision, error) {
	var candidates []crawldomain.SizesAuthCandidate
	if s.Auth != nil && s.Users != nil {
		userID, err := s.Users.UserIDForKey(ctx, job.KeyID)
		if err != nil {
			return SizesAuthDecision{}, fmt.Errorf("crawl: resolve sizes key %d owner: %w", job.KeyID, err)
		}
		candidates, err = s.Auth.SizesAuthCandidates(ctx, photo.OwnerNSID, userID)
		if err != nil {
			return SizesAuthDecision{}, fmt.Errorf("crawl: list sizes auth candidates for photo %d: %w", job.PhotoID, err)
		}
	}
	blocked := map[int64]bool{}
	for _, candidate := range candidates {
		if s.States == nil {
			break
		}
		access := EntityAccess{
			EntityRef: EntityRef{Kind: "photo", ID: fmt.Sprint(job.PhotoID)},
			Facet:     "sizes",
			UserID:    candidate.UserID,
			KeyID:     candidate.KeyID,
			Version:   photo.LastUpdate,
		}
		suppressed, err := s.States.Suppressed(ctx, access, s.now())
		if err != nil {
			return SizesAuthDecision{}, err
		}
		if suppressed {
			blocked[candidate.AccountID] = true
		}
	}
	return ChooseSizesAuth(SizesAuthInput{
		Placeholder: photo.HasRestrictedPlaceholder(),
		Restricted:  restricted,
		OnDemand:    job.Class == string(quotadomain.Interactive),
		Candidates:  candidates,
		Blocked:     func(candidate crawldomain.SizesAuthCandidate) bool { return blocked[candidate.AccountID] },
	}), nil
}

func (s Sizes) fetchAuthenticated(ctx context.Context, job SizesJob, photo catalog.Photo, candidate crawldomain.SizesAuthCandidate) error {
	authenticated, ok := s.Flickr.(AuthenticatedSizesClient)
	if !ok {
		return errors.New("crawl: authenticated sizes client is required for restricted photos")
	}
	class := quotadomain.Photo
	if job.Class != "" {
		class = quotadomain.Class(job.Class)
	}
	access := EntityAccess{
		EntityRef: EntityRef{Kind: "photo", ID: fmt.Sprint(job.PhotoID)},
		Facet:     "sizes",
		UserID:    candidate.UserID,
		KeyID:     candidate.KeyID,
		Version:   photo.LastUpdate,
	}
	sizes, err := authenticated.GetSizesForAccount(ctx, candidate.KeyID, candidate.AccountID, fmt.Sprint(job.PhotoID), class)
	if err != nil {
		return s.resolveFailure(ctx, job.PhotoID, access, err)
	}
	return s.confirm(ctx, job.PhotoID, sizes)
}

func (s Sizes) confirm(ctx context.Context, photoID int64, sizes []flickr.PhotoSize) error {
	encoded, err := json.Marshal(sizes)
	if err != nil {
		return fmt.Errorf("crawl: encode sizes for photo %d: %w", photoID, err)
	}
	return s.Store.SetSizes(ctx, photoID, encoded, catalog.SizesConfirmed, s.now())
}

func sizesRestricted(err error) bool {
	return errors.Is(err, flickr.ErrNotFound) || errors.Is(err, flickr.ErrPermission) || errors.Is(err, flickr.ErrResourceDenied)
}

func (s Sizes) ready(ctx context.Context, job SizesJob) error {
	if ctx == nil || s.Store == nil || s.Flickr == nil || job.PhotoID <= 0 || job.KeyID <= 0 {
		return errors.New("crawl: photo.sizes dependencies and identifiers are required")
	}
	return ctx.Err()
}

func shouldSkipSizes(photo catalog.Photo, exists bool) bool {
	if !exists || (photo.SizesStatus == catalog.SizesUnavailable && (photo.SizesCheckedAt == nil || !photo.SizesCheckedAt.Before(photo.LastUpdate))) {
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

// reannounce unblocks requests created after a prior confirmation event.
func (s Sizes) reannounce(ctx context.Context, photoID int64, photo catalog.Photo) error {
	at := s.now()
	if photo.SizesCheckedAt != nil {
		at = *photo.SizesCheckedAt
	}
	sizes := photo.Sizes
	if len(sizes) == 0 {
		sizes = []byte("[]")
	}
	return s.Store.SetSizes(ctx, photoID, sizes, photo.SizesStatus, at)
}

func (s Sizes) resolveFailure(ctx context.Context, photoID int64, access EntityAccess, cause error) error {
	if s.States != nil && FailureState(access.EntityRef, cause) != "" {
		if err := recordEntityFailure(ctx, s.States, access, cause, s.now(), 0); err != nil {
			return err
		}
		return s.Store.SetSizes(ctx, photoID, []byte("[]"), catalog.SizesUnavailable, s.now())
	}
	return s.recordFlickrError(ctx, photoID, cause)
}
