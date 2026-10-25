package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// WorkItem represents one deduplicated piece of work, such as a person facet.
type WorkItem struct {
	ID           int64
	Kind         string
	SubjectKey   string
	UserID       *int64
	KeyID        *int64
	State        State
	Cursor       json.RawMessage
	PriorityBase int
	Attempts     int
	NotBefore    time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastError    *string
	Reopen       bool
}

// Validate checks the fields required to safely persist a work item.
func (item WorkItem) Validate() error {
	if strings.TrimSpace(item.Kind) == "" {
		return errors.New("schedule: work item kind is required")
	}
	if strings.TrimSpace(item.SubjectKey) == "" {
		return errors.New("schedule: work item subject key is required")
	}
	if item.State != "" && !knownState(item.State) {
		return errors.New("schedule: work item state is invalid")
	}
	if item.Attempts < 0 {
		return errors.New("schedule: work item attempts cannot be negative")
	}
	if len(item.Cursor) > 0 && !json.Valid(item.Cursor) {
		return errors.New("schedule: work item cursor must be valid JSON")
	}
	return nil
}
