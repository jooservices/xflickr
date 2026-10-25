package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/catalog/domain"
)

// CreateAsset stores metadata for a temporary photo asset and returns its generated id.
func (r *Repository) CreateAsset(ctx context.Context, asset domain.Asset) (domain.Asset, error) {
	if r == nil || r.db == nil || ctx == nil || !validAsset(asset, false) {
		return domain.Asset{}, errors.New("catalog: invalid asset")
	}
	err := r.db.QueryRow(ctx, `
		INSERT INTO assets (photo_id, size_label, sha256, bytes, blob_key, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at`, asset.PhotoID, asset.SizeLabel, asset.SHA256, asset.Bytes, asset.BlobKey, asset.ExpiresAt).
		Scan(&asset.ID, &asset.CreatedAt)
	if err != nil {
		return domain.Asset{}, fmt.Errorf("catalog: create asset: %w", err)
	}
	return asset, nil
}

// GetAsset loads an asset by id.
func (r *Repository) GetAsset(ctx context.Context, id int64) (domain.Asset, bool, error) {
	if r == nil || r.db == nil || ctx == nil || id <= 0 {
		return domain.Asset{}, false, errors.New("catalog: invalid asset id")
	}
	asset, err := scanAsset(r.db.QueryRow(ctx, `SELECT id, photo_id, size_label, sha256, bytes, blob_key, expires_at, created_at FROM assets WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Asset{}, false, nil
	}
	if err != nil {
		return domain.Asset{}, false, fmt.Errorf("catalog: get asset: %w", err)
	}
	return asset, true, nil
}

// UpdateAsset replaces mutable metadata for an existing asset.
func (r *Repository) UpdateAsset(ctx context.Context, asset domain.Asset) error {
	if r == nil || r.db == nil || ctx == nil || !validAsset(asset, true) {
		return errors.New("catalog: invalid asset")
	}
	_, err := r.db.Exec(ctx, `
		UPDATE assets SET photo_id=$2, size_label=$3, sha256=$4, bytes=$5, blob_key=$6, expires_at=$7
		WHERE id=$1`, asset.ID, asset.PhotoID, asset.SizeLabel, asset.SHA256, asset.Bytes, asset.BlobKey, asset.ExpiresAt)
	if err != nil {
		return fmt.Errorf("catalog: update asset: %w", err)
	}
	return nil
}

// DeleteAsset removes an asset row by id.
func (r *Repository) DeleteAsset(ctx context.Context, id int64) error {
	if r == nil || r.db == nil || ctx == nil || id <= 0 {
		return errors.New("catalog: invalid asset id")
	}
	_, err := r.db.Exec(ctx, `DELETE FROM assets WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("catalog: delete asset: %w", err)
	}
	return nil
}

// ExpiredAssets returns up to limit assets whose expiry has passed, oldest first.
func (r *Repository) ExpiredAssets(ctx context.Context, limit int) ([]domain.Asset, error) {
	if r == nil || r.db == nil || ctx == nil || limit <= 0 {
		return nil, errors.New("catalog: expired asset limit must be positive")
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, photo_id, size_label, sha256, bytes, blob_key, expires_at, created_at
		FROM assets WHERE expires_at <= now() ORDER BY expires_at, id LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("catalog: list expired assets: %w", err)
	}
	defer rows.Close()
	assets := make([]domain.Asset, 0)
	for rows.Next() {
		asset, err := scanAsset(rows)
		if err != nil {
			return nil, fmt.Errorf("catalog: scan expired asset: %w", err)
		}
		assets = append(assets, asset)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: list expired assets: %w", err)
	}
	return assets, nil
}

type assetScanner interface{ Scan(...any) error }

func scanAsset(scanner assetScanner) (domain.Asset, error) {
	var asset domain.Asset
	err := scanner.Scan(&asset.ID, &asset.PhotoID, &asset.SizeLabel, &asset.SHA256, &asset.Bytes, &asset.BlobKey, &asset.ExpiresAt, &asset.CreatedAt)
	return asset, err
}

func validAsset(a domain.Asset, requireID bool) bool {
	return (!requireID || a.ID > 0) && a.PhotoID > 0 && a.SizeLabel != "" && len(a.SHA256) == 32 && a.Bytes >= 0 && a.BlobKey != "" && !a.ExpiresAt.IsZero()
}
