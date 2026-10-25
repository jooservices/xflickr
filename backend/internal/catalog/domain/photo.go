package domain

import (
	"errors"
	"time"
)

// SizesStatus describes the confidence of a Flickr sizes response.
type SizesStatus string

const (
	SizesNone        SizesStatus = "none"
	SizesProvisional SizesStatus = "provisional"
	SizesConfirmed   SizesStatus = "confirmed"
	SizesUnavailable SizesStatus = "unavailable"
)

// Visibility is Flickr's photo visibility classification.
type Visibility string

const (
	VisibilityPublic  Visibility = "public"
	VisibilityFriends Visibility = "friends"
	VisibilityFamily  Visibility = "family"
	VisibilityPrivate Visibility = "private"
)

var ErrInvalidPhoto = errors.New("catalog: invalid photo")

// Photo is the catalog representation. JSON fields contain their encoded JSON
// representation; Raw is uncompressed JSON at the domain boundary.
type Photo struct {
	ID             int64
	OwnerNSID      string
	Secret         string
	Server         string
	Title          string
	Description    string
	Tags           []string
	TakenAt        *time.Time
	UploadedAt     *time.Time
	LastUpdate     time.Time
	Visibility     Visibility
	License        string
	Media          string
	Geo            []byte
	Sizes          []byte
	SizesStatus    SizesStatus
	SizesCheckedAt *time.Time
	Raw            []byte
	ContentHash    []byte
	TombstonedAt   *time.Time
}

// Validate checks fields whose values are constrained by the catalog schema.
func (p Photo) Validate() error {
	if p.ID <= 0 || p.OwnerNSID == "" || p.LastUpdate.IsZero() || len(p.Raw) == 0 {
		return ErrInvalidPhoto
	}
	switch p.Visibility {
	case VisibilityPublic, VisibilityFriends, VisibilityFamily, VisibilityPrivate:
	default:
		return ErrInvalidPhoto
	}
	switch p.SizesStatus {
	case "", SizesNone, SizesProvisional, SizesConfirmed, SizesUnavailable:
	default:
		return ErrInvalidPhoto
	}
	return nil
}

// CanReplaceSizes enforces that provisional Flickr size data cannot downgrade
// a confirmed record.
func CanReplaceSizes(current, incoming SizesStatus) bool {
	return current != SizesConfirmed || incoming == SizesConfirmed
}
