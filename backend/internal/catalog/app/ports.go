// Package app defines catalog repository ports.
package app

import (
	"context"
	"time"

	"github.com/jooservices/xflickr/internal/catalog/domain"
)

// Repository persists Flickr people and photos.
type Repository interface {
	UpsertPeople(context.Context, []domain.Person) error
	UpsertPhotos(context.Context, []domain.Photo) error
	SetSizes(context.Context, int64, []byte, domain.SizesStatus, time.Time) error
	MarkTombstone(context.Context, int64, time.Time) error
}

// PhotoRef names a photo and the person who owns it.
type PhotoRef struct {
	ID        int64
	OwnerNSID string
}

// PhotoSelector lists photos waiting for getSizes, best interest first. Skip
// drops the first candidates so a caller can page past photos it cannot use.
type PhotoSelector interface {
	Select(ctx context.Context, limit, skip int) ([]PhotoRef, error)
}
