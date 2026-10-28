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
	UpsertEnrichment(context.Context, domain.PhotoEnrichment) error
	GetEnrichment(context.Context, int64, domain.EnrichmentKind, string) (domain.PhotoEnrichment, bool, error)
	DeleteEnrichment(context.Context, int64, domain.EnrichmentKind, string) error
	CreateAsset(context.Context, domain.Asset) (domain.Asset, error)
	GetAsset(context.Context, int64) (domain.Asset, bool, error)
	UpdateAsset(context.Context, domain.Asset) error
	DeleteAsset(context.Context, int64) error
	ExpiredAssets(context.Context, int) ([]domain.Asset, error)
}

// PhotoRef names a photo and the person who owns it.
type PhotoRef struct {
	ID        int64
	OwnerNSID string
	Facet     string
}

// PhotoSelector lists photos waiting for getSizes, best interest first. Skip
// drops the first candidates so a caller can page past photos it cannot use.
// Optional facets are supplied in priority order and returned only after sizes.
type PhotoSelector interface {
	Select(ctx context.Context, limit, skip int, optionalFacets []string) ([]PhotoRef, error)
}

// Observation identifies a record discovered through one connected Flickr account.
type Observation struct {
	UserID          int64
	FlickrAccountID int64
	RecordKind      string
	RecordID        string
}

// Observer records catalog provenance in the caller's transaction.
type Observer interface {
	Observe(context.Context, Observation) error
}
