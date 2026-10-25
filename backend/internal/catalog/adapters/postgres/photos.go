package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres/gen"
	"github.com/jooservices/xflickr/internal/catalog/domain"
)

// UpsertPhotos inserts or refreshes at most 500 photos in one pgx batch.
func (r *Repository) UpsertPhotos(ctx context.Context, photos []domain.Photo) error {
	if err := validateBatchSize(len(photos)); err != nil {
		return err
	}
	if len(photos) == 0 {
		return nil
	}
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		return fmt.Errorf("catalog: create zstd encoder: %w", err)
	}
	defer encoder.Close()
	params := make([]gen.UpsertPhotoParams, 0, len(photos))
	for _, photo := range photos {
		if err := photo.Validate(); err != nil {
			return err
		}
		status := photo.SizesStatus
		if status == "" {
			status = domain.SizesNone
		}
		tags := photo.Tags
		if tags == nil {
			tags = []string{}
		}
		compressedRaw := encoder.EncodeAll(photo.Raw, nil)
		raw, err := json.Marshal(struct {
			Encoding string `json:"encoding"`
			Data     string `json:"data"`
		}{Encoding: "zstd", Data: base64.StdEncoding.EncodeToString(compressedRaw)})
		if err != nil {
			return fmt.Errorf("catalog: encode compressed raw payload: %w", err)
		}
		hash := sha256.Sum256(photo.Raw)
		params = append(params, gen.UpsertPhotoParams{
			ID: photo.ID, OwnerNsid: photo.OwnerNSID, Secret: photo.Secret, Server: photo.Server,
			Title: photo.Title, Description: photo.Description, Tags: tags,
			TakenAt: pgTime(photo.TakenAt), UploadedAt: pgTime(photo.UploadedAt),
			LastUpdate: toPgTime(photo.LastUpdate), Visibility: string(photo.Visibility),
			License: photo.License, Media: photo.Media, Geo: jsonOrObject(photo.Geo),
			Sizes: jsonOrArray(photo.Sizes), SizesStatus: string(status),
			SizesCheckedAt: pgTime(photo.SizesCheckedAt), Raw: raw, ContentHash: hash[:],
			TombstonedAt: pgTime(photo.TombstonedAt),
		})
	}
	results := r.queries.UpsertPhoto(ctx, params)
	var batchErr error
	results.Exec(func(_ int, err error) {
		if err != nil && batchErr == nil {
			batchErr = err
		}
	})
	if batchErr != nil {
		return fmt.Errorf("catalog: upsert photos: %w", batchErr)
	}
	return nil
}

// SetSizes stores a sizes response unless it would downgrade confirmed data.
func (r *Repository) SetSizes(ctx context.Context, id int64, sizes []byte, status domain.SizesStatus, checkedAt time.Time) error {
	if id <= 0 || !validSizesStatus(string(status)) || !jsonDocument(sizes) || checkedAt.IsZero() {
		return errors.New("catalog: invalid sizes update")
	}
	return r.queries.SetSizes(ctx, gen.SetSizesParams{ID: id, Sizes: sizes, SizesStatus: string(status), SizesCheckedAt: toPgTime(checkedAt)})
}

// MarkTombstone marks an existing photo once. Repeating the call is a no-op.
func (r *Repository) MarkTombstone(ctx context.Context, id int64, at time.Time) error {
	if id <= 0 || at.IsZero() {
		return errors.New("catalog: invalid tombstone")
	}
	return r.queries.MarkTombstone(ctx, gen.MarkTombstoneParams{ID: id, TombstonedAt: toPgTime(at)})
}
