// Package app coordinates search use cases.
package app

import (
	"context"
	"errors"
	"time"
)

// ErrInvalidDocument indicates a search document that cannot be indexed.
var ErrInvalidDocument = errors.New("search: invalid document")

// Document contains the mapped fields for one photo search document.
type Document struct {
	ID             int64      `json:"-"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	Tags           []string   `json:"tags"`
	Caption        string     `json:"caption"`
	Embedding      []float32  `json:"embedding,omitempty"`
	Owner          string     `json:"owner"`
	Kind           string     `json:"kind"`
	SizesStatus    string     `json:"sizes_status"`
	AIStatus       string     `json:"ai_status"`
	UploadedAt     *time.Time `json:"uploaded_at,omitempty"`
	VisibleTo      []string   `json:"visible_to"`
	Visibility     string     `json:"visibility"`
	PubliclyShared bool       `json:"publicly_shared"`
}

// Upserter writes or replaces one photo search document.
type Upserter interface {
	Upsert(context.Context, Document) error
}
