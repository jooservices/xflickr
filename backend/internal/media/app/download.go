package app

import (
	"context"
	"errors"
	"strings"
	"time"

	catalogapp "github.com/jooservices/xflickr/internal/catalog/app"
	mediadomain "github.com/jooservices/xflickr/internal/media/domain"
	sharingdomain "github.com/jooservices/xflickr/internal/sharing/domain"
)

var (
	ErrInvalidDownloadRequest = errors.New("media: invalid download request")
	ErrNoFlickrKey            = errors.New("media: no valid Flickr key")
)

const MaxDownloadItems = 500

type DownloadRepository interface {
	Create(context.Context, mediadomain.Download, []DownloadItemCandidate) (mediadomain.Download, error)
}

type DownloadItemCandidate struct {
	PhotoID        int64
	OwnerNSID      string
	SizesConfirmed bool
}

type CreateDownloadInput struct {
	UserID   int64
	Kind     mediadomain.DownloadKind
	Subject  string
	PhotoIDs []int64
}

type Downloads struct {
	Catalog    catalogapp.DownloadCatalog
	Repository DownloadRepository
	Pressure   interface{ Check(context.Context) error }
	Now        func() time.Time
	TTL        time.Duration
}

func (s Downloads) Create(ctx context.Context, input CreateDownloadInput) (mediadomain.Download, error) {
	if ctx == nil || s.Repository == nil || input.UserID <= 0 || !validDownloadSelection(input) {
		return mediadomain.Download{}, ErrInvalidDownloadRequest
	}
	if s.Catalog.Photos == nil {
		return mediadomain.Download{}, ErrInvalidDownloadRequest
	}
	if s.Pressure != nil {
		if err := s.Pressure.Check(ctx); err != nil {
			return mediadomain.Download{}, err
		}
	}
	candidates, err := s.Catalog.Candidates(ctx, string(input.Kind), input.Subject, input.PhotoIDs)
	if err != nil {
		return mediadomain.Download{}, err
	}
	if len(candidates) > MaxDownloadItems {
		return mediadomain.Download{}, ErrInvalidDownloadRequest
	}
	visible, items, skipped := selectVisibleDownloadItems(input.UserID, candidates)
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	ttl := s.TTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	download := mediadomain.Download{UserID: input.UserID, Kind: input.Kind, Subject: input.Subject, Status: "pending", CreatedAt: now, ExpiresAt: now.Add(ttl), Items: items, Skipped: skipped}
	if input.Kind == mediadomain.DownloadTray {
		download.Subject = "tray"
	}
	if err := download.Validate(); err != nil {
		return mediadomain.Download{}, err
	}
	return s.Repository.Create(ctx, download, visible)
}

func selectVisibleDownloadItems(userID int64, candidates []catalogapp.DownloadCandidate) ([]DownloadItemCandidate, []mediadomain.DownloadItem, int) {
	visible := make([]DownloadItemCandidate, 0, len(candidates))
	items := make([]mediadomain.DownloadItem, 0, len(candidates))
	skipped := 0
	for _, candidate := range candidates {
		observers := make([]sharingdomain.Observer, 0, len(candidate.Observers))
		for _, observer := range candidate.Observers {
			observers = append(observers, sharingdomain.Observer{UserID: observer.UserID, SharingEnabled: observer.SharingEnabled})
		}
		if !sharingdomain.CanSee(sharingdomain.User{ID: userID}, sharingdomain.Record{Visibility: candidate.Visibility, Observers: observers}) {
			skipped++
			continue
		}
		visible = append(visible, DownloadItemCandidate{PhotoID: candidate.PhotoID, OwnerNSID: candidate.OwnerNSID, SizesConfirmed: candidate.SizesConfirmed})
		state := mediadomain.ItemWaitingSizes
		if candidate.SizesConfirmed {
			state = mediadomain.ItemPending
		}
		items = append(items, mediadomain.DownloadItem{PhotoID: candidate.PhotoID, State: state})
	}
	return visible, items, skipped
}

func validDownloadSelection(input CreateDownloadInput) bool {
	if strings.TrimSpace(input.Subject) != "" && len(input.PhotoIDs) == 0 {
		return input.Kind == mediadomain.DownloadContact || input.Kind == mediadomain.DownloadAlbum
	}
	if input.Kind != mediadomain.DownloadTray || strings.TrimSpace(input.Subject) != "" || len(input.PhotoIDs) == 0 || len(input.PhotoIDs) > MaxDownloadItems {
		return false
	}
	seen := make(map[int64]struct{}, len(input.PhotoIDs))
	for _, id := range input.PhotoIDs {
		if id <= 0 {
			return false
		}
		if _, ok := seen[id]; ok {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}
