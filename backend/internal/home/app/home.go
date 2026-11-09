// Package app coordinates reads for the signed-in user's Home snapshot.
package app

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidUser    = errors.New("home: user id must be positive")
	ErrInvalidContext = errors.New("home: context is required")
	ErrReaderRequired = errors.New("home: snapshot reader is required")
)

// Snapshot is the user-scoped data returned by GET /me/home.
type Snapshot struct {
	GeneratedAt     time.Time     `json:"generated_at"`
	Contacts        ContactCounts `json:"contacts"`
	Photos          PhotoCounts   `json:"photos"`
	NewPhotos       []Photo       `json:"new_photos"`
	StarredContacts []Contact     `json:"starred_contacts"`
	Activity        []Activity    `json:"activity"`
}

// ContactCounts summarizes the current user's discovered people.
type ContactCounts struct {
	Total      int64 `json:"total"`
	Direct     int64 `json:"direct"`
	Discovered int64 `json:"discovered"`
}

// PhotoCounts summarizes visible photo records and their enrichments.
type PhotoCounts struct {
	Total      int64 `json:"total"`
	SizesReady int64 `json:"sizes_ready"`
	Captioned  int64 `json:"captioned"`
}

// Photo is a visible photo shown in the Home snapshot.
type Photo struct {
	ID           int64      `json:"id"`
	Title        string     `json:"title"`
	ThumbnailURL *string    `json:"thumbnail_url"`
	OwnerNSID    string     `json:"owner_nsid"`
	OwnerName    string     `json:"owner_name"`
	TakenAt      *time.Time `json:"taken_at"`
	Captioned    bool       `json:"captioned"`
}

// Contact is a starred person with visible photo and crawl state counts.
type Contact struct {
	NSID        string `json:"nsid"`
	Name        string `json:"name"`
	IconURL     string `json:"icon_url"`
	PhotosCount int64  `json:"photos_count"`
	SizesReady  int64  `json:"sizes_ready"`
	SizesTotal  int64  `json:"sizes_total"`
	CrawlState  string `json:"crawl_state"`
}

// Activity is a recent user-scoped event displayed in the Home activity list.
type Activity struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	MessageKey  string    `json:"message_key"`
	Count       int64     `json:"count"`
	ContactName string    `json:"contact_name"`
	OccurredAt  time.Time `json:"occurred_at"`
	Severity    string    `json:"severity"`
}

// Reader loads one coherent Home snapshot for a user.
type Reader interface {
	Load(context.Context, int64) (Snapshot, error)
}

// Service applies user validation before reading a Home snapshot.
type Service struct{ Reader Reader }

// Get returns the current user's Home snapshot.
func (s Service) Get(ctx context.Context, userID int64) (Snapshot, error) {
	if ctx == nil {
		return Snapshot{}, ErrInvalidContext
	}
	if userID <= 0 {
		return Snapshot{}, ErrInvalidUser
	}
	if s.Reader == nil {
		return Snapshot{}, ErrReaderRequired
	}
	snapshot, err := s.Reader.Load(ctx, userID)
	if err != nil {
		return Snapshot{}, fmt.Errorf("home: load snapshot: %w", err)
	}
	return snapshot, nil
}
