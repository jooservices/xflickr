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
	DownloadCandidates(context.Context, string, string, []int64) ([]DownloadCandidate, error)
}

type DownloadCatalog struct{ Photos DownloadPhotoRepository }

func (c DownloadCatalog) Candidates(ctx context.Context, kind, subject string, ids []int64) ([]DownloadCandidate, error) {
	if ctx == nil || c.Photos == nil {
		return nil, errors.New("catalog: download photo repository and context are required")
	}
	return c.Photos.DownloadCandidates(ctx, kind, subject, ids)
}
