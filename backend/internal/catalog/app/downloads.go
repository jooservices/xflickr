package app

import (
	"context"
	"errors"
)

type DownloadObserver struct {
	UserID         int64
	SharingEnabled bool
}

type DownloadCandidate struct {
	PhotoID        int64
	OwnerNSID      string
	Visibility     string
	Observers      []DownloadObserver
	SizesConfirmed bool
}

type DownloadPhotoRepository interface {
	DownloadCandidates(context.Context, int64, string, string, []int64) ([]DownloadCandidate, error)
}

type DownloadCatalog struct{ Photos DownloadPhotoRepository }

func (c DownloadCatalog) Candidates(ctx context.Context, userID int64, kind, subject string, ids []int64) ([]DownloadCandidate, error) {
	if ctx == nil || c.Photos == nil {
		return nil, errors.New("catalog: download photo repository and context are required")
	}
	if userID <= 0 {
		return nil, errors.New("catalog: download user is required")
	}
	return c.Photos.DownloadCandidates(ctx, userID, kind, subject, ids)
}
