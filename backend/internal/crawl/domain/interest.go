// Package domain contains crawl discovery rules.
package domain

import (
	"errors"
	"strings"
	"time"
)

var ErrInvalidInterest = errors.New("crawl: invalid interest")

// Interest identifies one user's interest in a Flickr person. Depth belongs
// to the user-person pair, so another user's discovery never changes it.
type Interest struct {
	UserID        int64
	PersonNSID    string
	Depth         int
	Via           string
	Starred       bool
	SelectedUntil *time.Time
	CreatedAt     time.Time
}

// Validate checks values that must be safe to persist.
func (interest Interest) Validate() error {
	if interest.UserID <= 0 || strings.TrimSpace(interest.PersonNSID) == "" || interest.Depth < 0 || strings.TrimSpace(interest.Via) == "" {
		return ErrInvalidInterest
	}
	return nil
}

// InterestDepth is the part needed to decide whether a person can be expanded.
type InterestDepth struct {
	UserID int64
	Depth  int
}

// KeepLowest returns the stored depth after considering a candidate depth.
func KeepLowest(existing, candidate int) int {
	if existing < candidate {
		return existing
	}
	return candidate
}

// IsExpandable reports whether at least one user's interest is shallower than
// that user's effective maximum depth.
func IsExpandable(interests []InterestDepth, maxDepthByUser map[int64]int) bool {
	for _, interest := range interests {
		if maxDepth, ok := maxDepthByUser[interest.UserID]; ok && interest.Depth < maxDepth {
			return true
		}
	}
	return false
}
