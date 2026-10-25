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
