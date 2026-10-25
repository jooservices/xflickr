// Package domain contains sharing settings and their events.
package domain

import (
	"errors"
	"time"
)

// Setting is a user's opt-in choice for sharing eligible records.
type Setting struct {
	UserID    int64
	Enabled   bool
	ChangedAt time.Time
}

func (s Setting) Validate() error {
	if s.UserID <= 0 {
		return errors.New("sharing: user id is required")
	}
	return nil
}

// Changed is published whenever the stored sharing choice changes.
type Changed struct {
	UserID    int64     `json:"user_id"`
	Enabled   bool      `json:"enabled"`
	ChangedAt time.Time `json:"changed_at"`
}

const ChangedTopic = "sharing.changed"
