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

// Summary is a user's sharing choice with the number of distinct eligible
// public records that the user contributes and others can see. The count is
// zero while sharing is off.
type Summary struct {
	Setting           Setting
	SharedRecordCount int64
}

// Changed is published whenever the stored sharing choice changes.
type Changed struct {
	UserID    int64     `json:"user_id"`
	Enabled   bool      `json:"enabled"`
	ChangedAt time.Time `json:"changed_at"`
}

const ChangedTopic = "sharing.changed"
