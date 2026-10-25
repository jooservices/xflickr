package domain

import (
	"encoding/json"
	"errors"
)

// Work item states the engine reads. They mirror the schedule backlog.
const (
	StateReady   = "ready"
	StateRunning = "running"
	StateDone    = "done"
	StateFailed  = "failed"
	StatePending = "pending"
)

// Cursor is the page progress stored in the work item's JSON cursor. It also
// carries the request, so one item pages one Flickr method to its end.
type Cursor struct {
	Page         int               `json:"page"`
	Method       string            `json:"method"`
	Params       map[string]string `json:"params,omitempty"`
	CheckpointAt int64             `json:"checkpoint_at,omitempty"`
}

// ErrInvalidCursor means a stored cursor cannot drive a request.
var ErrInvalidCursor = errors.New("crawl: invalid cursor")

// ParseCursor decodes a stored cursor. A missing page means the first page.
func ParseCursor(raw []byte) (Cursor, error) {
	var cursor Cursor
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return Cursor{}, ErrInvalidCursor
	}
	if cursor.Page == 0 {
		cursor.Page = 1
	}
	if cursor.Page < 1 || cursor.Method == "" {
		return Cursor{}, ErrInvalidCursor
	}
	return cursor, nil
}

// NewCursor returns the first-page cursor for a spec.
func NewCursor(spec WorkSpec) Cursor {
	return Cursor{Page: 1, Method: spec.Method, Params: spec.Params, CheckpointAt: spec.CheckpointAt}
}

// Next returns the cursor for the following page.
func (c Cursor) Next() Cursor {
	c.Page++
	return c
}

// Item is a loaded work item: one task is one API page.
type Item struct {
	ID       int64
	Kind     string
	Subject  string
	UserID   int64
	Depth    int
	State    string
	Attempts int
	Cursor   Cursor
}

// Page is one fetched Flickr page handed to a facet.
type Page struct {
	Item    Item
	Number  int
	Payload json.RawMessage
}
