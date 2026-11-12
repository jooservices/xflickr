// Package app defines catalog repository ports.
package app

import (
	"context"
)

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
