package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/catalog/domain"
)

// UpsertEnrichment inserts or replaces an AI result identified by photo, kind, and model.
func (r *Repository) UpsertEnrichment(ctx context.Context, enrichment domain.PhotoEnrichment) error {
	if r == nil || r.db == nil || ctx == nil || !validEnrichment(enrichment) {
		return errors.New("catalog: invalid photo enrichment")
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO photo_enrichments (photo_id, kind, model, model_version, payload)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (photo_id, kind, model) DO UPDATE
		SET model_version = EXCLUDED.model_version, payload = EXCLUDED.payload`,
		enrichment.PhotoID, string(enrichment.Kind), enrichment.Model, enrichment.ModelVersion, enrichment.Payload)
	if err != nil {
		return fmt.Errorf("catalog: upsert photo enrichment: %w", err)
	}
	return nil
}

// GetEnrichment loads one AI result by its unique key.
func (r *Repository) GetEnrichment(ctx context.Context, photoID int64, kind domain.EnrichmentKind, model string) (domain.PhotoEnrichment, bool, error) {
	if r == nil || r.db == nil || ctx == nil || photoID <= 0 || !validEnrichmentKind(kind) || model == "" {
		return domain.PhotoEnrichment{}, false, errors.New("catalog: invalid photo enrichment key")
	}
	var result domain.PhotoEnrichment
	err := r.db.QueryRow(ctx, `
		SELECT photo_id, kind, model, model_version, payload, created_at
		FROM photo_enrichments WHERE photo_id = $1 AND kind = $2 AND model = $3`, photoID, string(kind), model).
		Scan(&result.PhotoID, &result.Kind, &result.Model, &result.ModelVersion, &result.Payload, &result.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PhotoEnrichment{}, false, nil
	}
	if err != nil {
		return domain.PhotoEnrichment{}, false, fmt.Errorf("catalog: get photo enrichment: %w", err)
	}
	return result, true, nil
}

// DeleteEnrichment removes one AI result by its unique key.
func (r *Repository) DeleteEnrichment(ctx context.Context, photoID int64, kind domain.EnrichmentKind, model string) error {
	if r == nil || r.db == nil || ctx == nil || photoID <= 0 || !validEnrichmentKind(kind) || model == "" {
		return errors.New("catalog: invalid photo enrichment key")
	}
	_, err := r.db.Exec(ctx, `DELETE FROM photo_enrichments WHERE photo_id = $1 AND kind = $2 AND model = $3`, photoID, string(kind), model)
	if err != nil {
		return fmt.Errorf("catalog: delete photo enrichment: %w", err)
	}
	return nil
}

func validEnrichment(e domain.PhotoEnrichment) bool {
	return e.PhotoID > 0 && validEnrichmentKind(e.Kind) && e.Model != "" && e.ModelVersion != "" && jsonDocument(e.Payload)
}

func validEnrichmentKind(kind domain.EnrichmentKind) bool {
	return kind == domain.EnrichmentCaption || kind == domain.EnrichmentEmbeddingMeta
}
