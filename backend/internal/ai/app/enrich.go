package app

import (
	"context"
	"errors"
	"fmt"
)

// ErrInvalidPhotoID reports a photo identifier that is not positive.
var ErrInvalidPhotoID = errors.New("ai: photo id must be positive")

// CaptionStore persists a validated caption for one photo.
type CaptionStore interface {
	SaveCaption(context.Context, int64, Caption) error
}

// Enricher describes one supplied photo and stores its caption.
type Enricher struct {
	captioner Captioner
	store     CaptionStore
}

// NewEnricher creates an enricher with a caption provider and caption store.
func NewEnricher(c Captioner, s CaptionStore) (*Enricher, error) {
	if c == nil || s == nil {
		return nil, fmt.Errorf("%w: captioner and caption store are required", ErrInvalidConfiguration)
	}
	return &Enricher{captioner: c, store: s}, nil
}

// CaptionPhoto describes an image and stores the validated caption.
func (e *Enricher) CaptionPhoto(ctx context.Context, photoID int64, image []byte) error {
	if e == nil || e.captioner == nil || e.store == nil {
		return fmt.Errorf("%w: enricher is not configured", ErrInvalidConfiguration)
	}
	if ctx == nil {
		return fmt.Errorf("%w: context is required", ErrInvalidConfiguration)
	}
	if photoID <= 0 {
		return ErrInvalidPhotoID
	}
	if len(image) == 0 {
		return ErrInvalidImage
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("ai: caption photo %d: %w", photoID, err)
	}

	caption, err := e.captioner.Describe(ctx, image)
	if err != nil {
		return fmt.Errorf("ai: describe photo %d: %w", photoID, err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("ai: caption photo %d: %w", photoID, err)
	}
	if err := caption.Validate(); err != nil {
		return fmt.Errorf("ai: validate caption for photo %d: %w", photoID, err)
	}
	if err := e.store.SaveCaption(ctx, photoID, caption); err != nil {
		return fmt.Errorf("ai: save caption for photo %d: %w", photoID, err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("ai: caption photo %d: %w", photoID, err)
	}
	return nil
}
