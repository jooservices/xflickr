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

// AccountSizesStore keeps OAuth-authorized URLs private to the account that
// proved access and resolves only that account owner's waiting downloads.
type AccountSizesStore interface {
	AccountSizes(ctx context.Context, photoID, accountID int64, version time.Time, visibility string) (catalog.SizesStatus, bool, error)
	SetAccountSizes(ctx context.Context, photoID, accountID, userID int64, sizes []byte, status catalog.SizesStatus, version time.Time, visibility string, checkedAt time.Time) error
	ResolveUserSizes(ctx context.Context, photoID, userID int64, status catalog.SizesStatus) error
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
	States   EntityStates
	Store    SizesStore
	Accounts AccountSizesStore
	Flickr   SizesClient
	Auth     crawldomain.SizesAuthenticator
	Users    KeyUser
	Now      func() time.Time
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
	return s.handleLoadedPhoto(ctx, job, photo, exists)
}

func (s Sizes) handleLoadedPhoto(ctx context.Context, job SizesJob, photo catalog.Photo, exists bool) error {
	restricted := exists && (photo.HasRestrictedPlaceholder() || photo.Visibility != catalog.VisibilityPublic)
	if shouldSkipSizes(photo, exists) && !restricted {
		if !exists {
			return nil
		}
		return s.reannounce(ctx, job.PhotoID, photo)
	}
	userID, err := s.keyUserID(ctx, job.KeyID)
	if err != nil {
		return err
	}
	access := EntityAccess{
		EntityRef: EntityRef{Kind: "photo", ID: fmt.Sprint(job.PhotoID)},
		Facet:     "sizes", UserID: userID, KeyID: job.KeyID, Version: photo.LastUpdate,
	}
	if restricted {
		return s.retryAuthenticated(ctx, job, photo, true, userID)
	}
	return s.handleAnonymousSizes(ctx, job, photo, access, userID)
}

func (s Sizes) handleAnonymousSizes(ctx context.Context, job SizesJob, photo catalog.Photo, access EntityAccess, userID int64) error {
	if s.States != nil && userID > 0 {
		suppressed, err := s.States.Suppressed(ctx, access, s.now())
		if err != nil {
			return fmt.Errorf("crawl: check photo %d sizes restriction: %w", job.PhotoID, err)
		}
		if suppressed {
			return s.retryAuthenticated(ctx, job, photo, true, userID)
		}
	}
	sizes, err := s.fetchAnonymous(ctx, job)
	if err == nil {
		return s.confirm(ctx, job.PhotoID, sizes)
	}
	if !sizesRestricted(err) {
		return s.resolveFailure(ctx, job.PhotoID, access, err)
	}
	if err := recordEntityFailure(ctx, s.States, access, err, s.now(), 0); err != nil {
		return err
	}
	return s.retryAuthenticated(ctx, job, photo, true, userID)
}

func (s Sizes) fetchAnonymous(ctx context.Context, job SizesJob) ([]flickr.PhotoSize, error) {
	if classified, ok := s.Flickr.(ClassifiedSizesClient); ok && job.Class != "" {
		return classified.GetSizesClass(ctx, job.KeyID, fmt.Sprint(job.PhotoID), quotadomain.Class(job.Class))
	}
	return s.Flickr.GetSizes(ctx, job.KeyID, fmt.Sprint(job.PhotoID))
}

func (s Sizes) retryAuthenticated(ctx context.Context, job SizesJob, photo catalog.Photo, restricted bool, userID int64) error {
	for {
		decision, err := s.decideAuth(ctx, job, photo, restricted, userID)
		if err != nil {
			return err
		}
		if decision.Kind == SizesAuthAccount {
			restricted, err := s.fetchAuthenticated(ctx, job, photo, decision.Candidate)
			if err != nil {
				return err
			}
			if restricted {
				continue
			}
			return nil
		}
		if decision.UserID <= 0 {
			return nil
		}
		if s.Accounts == nil {
			return errors.New("crawl: account-scoped sizes store and requesting user are required")
		}
		status := decision.ExistingStatus
		if status != catalog.SizesConfirmed {
			status = catalog.SizesUnavailable
		}
		return s.Accounts.ResolveUserSizes(ctx, job.PhotoID, decision.UserID, status)
	}
}

func (s Sizes) decideAuth(ctx context.Context, job SizesJob, photo catalog.Photo, restricted bool, userID int64) (SizesAuthDecision, error) {
	var candidates []crawldomain.SizesAuthCandidate
	if s.Auth != nil && userID > 0 {
		var err error
		candidates, err = s.Auth.SizesAuthCandidates(ctx, photo.OwnerNSID, userID)
		if err != nil {
			return SizesAuthDecision{}, fmt.Errorf("crawl: list sizes auth candidates for photo %d: %w", job.PhotoID, err)
		}
	}
	onDemand := job.Class == string(quotadomain.Interactive)
	candidates = eligibleSizesAuthCandidates(candidates, onDemand)
	blocked := map[int64]bool{}
	existingStatus := catalog.SizesStatus("")
	for _, candidate := range candidates {
		if s.Accounts != nil {
			status, exists, err := s.Accounts.AccountSizes(ctx, job.PhotoID, candidate.AccountID, photo.LastUpdate, string(photo.Visibility))
			if err != nil {
				return SizesAuthDecision{}, fmt.Errorf("crawl: load account sizes for photo %d: %w", job.PhotoID, err)
			}
			if exists {
				blocked[candidate.AccountID] = true
				if status == catalog.SizesConfirmed {
					return SizesAuthDecision{UserID: userID, ExistingStatus: status}, nil
				}
				existingStatus = status
			}
		}
		if s.States == nil {
			continue
		}
		access := EntityAccess{
			EntityRef: EntityRef{Kind: "photo", ID: fmt.Sprint(job.PhotoID)},
			Facet:     "sizes",
			UserID:    candidate.UserID,
			KeyID:     candidate.KeyID,
			AccountID: candidate.AccountID,
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
	decision := ChooseSizesAuth(SizesAuthInput{
		Placeholder: photo.HasRestrictedPlaceholder(),
		Restricted:  restricted,
		OnDemand:    onDemand,
		Candidates:  candidates,
		Blocked:     func(candidate crawldomain.SizesAuthCandidate) bool { return blocked[candidate.AccountID] },
	})
	decision.UserID = userID
	decision.ExistingStatus = existingStatus
	return decision, nil
}

func (s Sizes) keyUserID(ctx context.Context, keyID int64) (int64, error) {
	if s.Users == nil {
		return 0, nil
	}
	userID, err := s.Users.UserIDForKey(ctx, keyID)
	if err != nil {
		return 0, fmt.Errorf("crawl: resolve sizes key %d owner: %w", keyID, err)
	}
	return userID, nil
}

func (s Sizes) fetchAuthenticated(ctx context.Context, job SizesJob, photo catalog.Photo, candidate crawldomain.SizesAuthCandidate) (bool, error) {
	authenticated, ok := s.Flickr.(AuthenticatedSizesClient)
	if !ok {
		return false, errors.New("crawl: authenticated sizes client is required for restricted photos")
	}
	if s.Accounts == nil {
		return false, errors.New("crawl: account-scoped sizes store is required for restricted photos")
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
		AccountID: candidate.AccountID,
		Version:   photo.LastUpdate,
	}
	sizes, err := authenticated.GetSizesForAccount(ctx, candidate.KeyID, candidate.AccountID, fmt.Sprint(job.PhotoID), class)
	if err != nil {
		return s.resolveAccountFailure(ctx, job.PhotoID, candidate, access, photo, err)
	}
	return false, s.confirmAccount(ctx, job.PhotoID, candidate, photo, sizes)
}

func (s Sizes) confirmAccount(ctx context.Context, photoID int64, candidate crawldomain.SizesAuthCandidate, photo catalog.Photo, sizes []flickr.PhotoSize) error {
	encoded, err := json.Marshal(sizes)
	if err != nil {
		return fmt.Errorf("crawl: encode account sizes for photo %d: %w", photoID, err)
	}
	return s.Accounts.SetAccountSizes(ctx, photoID, candidate.AccountID, candidate.UserID, encoded, catalog.SizesConfirmed, photo.LastUpdate, string(photo.Visibility), s.now())
}

func (s Sizes) resolveAccountFailure(ctx context.Context, photoID int64, candidate crawldomain.SizesAuthCandidate, access EntityAccess, photo catalog.Photo, cause error) (bool, error) {
	if FailureState(access.EntityRef, cause) == "" {
		return false, s.recordFlickrError(ctx, photoID, cause)
	}
	if err := recordEntityFailure(ctx, s.States, access, cause, s.now(), 0); err != nil {
		return false, err
	}
	if err := s.Accounts.SetAccountSizes(ctx, photoID, candidate.AccountID, candidate.UserID, []byte("[]"), catalog.SizesUnavailable, photo.LastUpdate, string(photo.Visibility), s.now()); err != nil {
		return false, err
	}
	return true, nil
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
	if !exists {
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
