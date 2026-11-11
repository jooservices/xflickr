package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jooservices/xflickr/internal/catalog/domain"
)

var (
	ErrInvalidEnrichmentFreshness  = errors.New("catalog: invalid enrichment freshness request")
	ErrEnrichmentReaderUnavailable = errors.New("catalog: enrichment reader is required")
)

// EnrichmentReader reads an enrichment by its unique photo, kind, and model key.
type EnrichmentReader interface {
	GetEnrichment(context.Context, int64, domain.EnrichmentKind, string) (domain.PhotoEnrichment, bool, error)
}

// EnrichmentFreshness checks whether a photo needs the requested model version.
type EnrichmentFreshness struct {
	Reader EnrichmentReader
}

// NeedsEnrichment reports whether the stored result is missing or has an old version.
func (s EnrichmentFreshness) NeedsEnrichment(ctx context.Context, photoID int64, kind string, model string, version string) (bool, error) {
	if ctx == nil || photoID <= 0 || !validFreshnessKind(kind) || strings.TrimSpace(model) == "" || strings.TrimSpace(version) == "" {
		return false, ErrInvalidEnrichmentFreshness
	}
	if s.Reader == nil {
		return false, ErrEnrichmentReaderUnavailable
	}

	enrichment, found, err := s.Reader.GetEnrichment(ctx, photoID, domain.EnrichmentKind(kind), model)
	if err != nil {
		return false, fmt.Errorf("catalog: check enrichment freshness: %w", err)
	}
	return !found || enrichment.ModelVersion != version, nil
}

func validFreshnessKind(kind string) bool {
	return kind == string(domain.EnrichmentCaption) || kind == string(domain.EnrichmentEmbeddingMeta)
}
