// Package catalog stores AI captions through the catalog application API.
package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	aiapp "github.com/jooservices/xflickr/internal/ai/app"
)

// CaptionWriter persists a caption through the catalog application boundary.
type CaptionWriter interface {
	SaveCaption(context.Context, int64, string, string, []byte) error
}

// Store adapts the catalog caption writer to the AI caption store port.
type Store struct {
	writer CaptionWriter
}

// NewStore creates a caption store that writes through the catalog application API.
func NewStore(w CaptionWriter) (*Store, error) {
	if w == nil {
		return nil, errors.New("ai: catalog caption writer is required")
	}
	return &Store{writer: w}, nil
}

// SaveCaption encodes a validated caption and persists its payload and provenance.
func (s *Store) SaveCaption(ctx context.Context, photoID int64, caption aiapp.Caption) error {
	if s == nil || s.writer == nil {
		return errors.New("ai: catalog caption store is not configured")
	}
	payload, err := json.Marshal(caption)
	if err != nil {
		return fmt.Errorf("ai: encode caption: %w", err)
	}
	if err := s.writer.SaveCaption(ctx, photoID, caption.Model, caption.Version, payload); err != nil {
		return fmt.Errorf("ai: persist caption for photo %d: %w", photoID, err)
	}
	return nil
}

var _ aiapp.CaptionStore = (*Store)(nil)
