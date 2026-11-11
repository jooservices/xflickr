package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/jooservices/xflickr/internal/catalog/domain"
)

// CaptionRepository stores catalog enrichment records.
type CaptionRepository interface {
	UpsertEnrichment(context.Context, domain.PhotoEnrichment) error
}

// CaptionService maps caption results to catalog enrichment records.
type CaptionService struct {
	repository CaptionRepository
}

// NewCaptionService creates a service backed by the catalog enrichment repository.
func NewCaptionService(r CaptionRepository) (*CaptionService, error) {
	if r == nil {
		return nil, errors.New("catalog: caption repository is required")
	}
	return &CaptionService{repository: r}, nil
}

// SaveCaption stores a caption as a versioned catalog enrichment.
func (s *CaptionService) SaveCaption(ctx context.Context, photoID int64, model, version string, payload []byte) error {
	if s == nil || s.repository == nil {
		return errors.New("catalog: caption service is not configured")
	}
	enrichment := domain.PhotoEnrichment{
		PhotoID:      photoID,
		Kind:         domain.EnrichmentCaption,
		Model:        model,
		ModelVersion: version,
		Payload:      payload,
	}
	if err := s.repository.UpsertEnrichment(ctx, enrichment); err != nil {
		return fmt.Errorf("catalog: save caption enrichment: %w", err)
	}
	return nil
}
