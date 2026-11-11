// Package app provides the Home snapshot use case.
package app

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrInvalidUser = errors.New("home: invalid user")

// ContactDepthCounts contains the user's direct and discovered contacts.
type ContactDepthCounts struct {
	Direct     int64
	Discovered int64
}

// ContactDepthReader reads contact counts scoped to one XFlickr user.
type ContactDepthReader interface {
	ContactDepths(ctx context.Context, userID int64) (ContactDepthCounts, error)
}

// ContactCounts is the Home API's contact summary.
type ContactCounts struct {
	Total      int64
	Direct     int64
	Discovered int64
}

// PhotoCounts contains nullable metrics deferred to the statistics slice.
type PhotoCounts struct {
	Total      *int64 `json:"total"`
	SizesReady *int64 `json:"sizes_ready"`
	Captioned  *int64 `json:"captioned"`
}

// Section names a Home section unavailable in this API slice.
type Section string

const (
	SectionPhotos          Section = "photos"
	SectionStarredContacts Section = "starred_contacts"
	SectionActivity        Section = "activity"
)

// Snapshot contains the Home data currently available from durable sources.
type Snapshot struct {
	GeneratedAt         time.Time
	Contacts            ContactCounts
	Photos              PhotoCounts
	UnavailableSections []Section
}

// Service builds a user's Home snapshot.
type Service struct {
	reader ContactDepthReader
	now    func() time.Time
}

// New creates a Home snapshot service.
func New(reader ContactDepthReader, now func() time.Time) (Service, error) {
	if reader == nil || now == nil {
		return Service{}, errors.New("home: contact reader and clock are required")
	}
	return Service{reader: reader, now: now}, nil
}

// Snapshot returns contact counts and identifies sections without a durable source.
func (s Service) Snapshot(ctx context.Context, userID int64) (Snapshot, error) {
	if userID <= 0 {
		return Snapshot{}, ErrInvalidUser
	}
	counts, err := s.reader.ContactDepths(ctx, userID)
	if err != nil {
		return Snapshot{}, fmt.Errorf("home: read contact depths: %w", err)
	}
	return Snapshot{
		GeneratedAt: s.now().UTC(),
		Contacts: ContactCounts{
			Total:      counts.Direct + counts.Discovered,
			Direct:     counts.Direct,
			Discovered: counts.Discovered,
		},
		UnavailableSections: []Section{SectionPhotos, SectionStarredContacts, SectionActivity},
	}, nil
}
