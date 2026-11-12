package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/klauspost/compress/zstd"

	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres/gen"
	"github.com/jooservices/xflickr/internal/catalog/domain"
)

// LoadSizes returns the current photo version and its sizes check metadata.
func (r *Repository) LoadSizes(ctx context.Context, id int64) (domain.Photo, bool, error) {
	if r == nil || r.db == nil || ctx == nil || id <= 0 {
		return domain.Photo{}, false, errors.New("catalog: photo sizes database, context and id are required")
	}
	var photo domain.Photo
	var status string
	var checked pgtype.Timestamptz
	err := r.db.QueryRow(ctx, `SELECT id, last_update, sizes_status, sizes_checked_at, server, sizes FROM photos WHERE id=$1`, id).Scan(&photo.ID, &photo.LastUpdate, &status, &checked, &photo.Server, &photo.Sizes)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Photo{}, false, nil
	}
	if err != nil {
		return domain.Photo{}, false, fmt.Errorf("catalog: load photo sizes state: %w", err)
	}
	photo.SizesStatus = domain.SizesStatus(status)
	if checked.Valid {
		photo.SizesCheckedAt = &checked.Time
	}
	return photo, true, nil
}

// Two statements instead of INSERT ... ON CONFLICT DO UPDATE (JOOXF-415): the conflict form locks and
// rewrites a known row every time the crawler sees it again, and last_seen_at is indexed, so no update could
// be HOT. The insert only adds new rows; the update touches a row only when its content changed, its
// tombstone state changed, or last_seen_at is more than an hour old.
const photoInsertSQL = `INSERT INTO photos (
    id, owner_nsid, secret, server, title, description, tags, taken_at,
    uploaded_at, last_update, visibility, license, media, geo, sizes,
    sizes_status, sizes_checked_at, raw, content_hash, tombstoned_at, last_seen_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
    $16, $17, $18, $19, $20, $21
) ON CONFLICT (id) DO NOTHING`

const photoUpdateSQL = `UPDATE photos SET
    owner_nsid = $2, secret = $3, server = $4, title = $5, description = $6, tags = $7,
    taken_at = $8, uploaded_at = $9, last_update = $10, visibility = $11, license = $12,
    media = $13, geo = $14, raw = $15, content_hash = $16, tombstoned_at = $17, last_seen_at = $18
WHERE id = $1
  AND (last_update IS DISTINCT FROM $10
       OR content_hash IS DISTINCT FROM $16
       OR tombstoned_at IS DISTINCT FROM $17
       OR last_seen_at IS NULL
       OR last_seen_at < $18::timestamptz - interval '1 hour')`

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
	seenAt := toPgTime(time.Now().UTC())
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
			TombstonedAt: pgTime(photo.TombstonedAt), LastSeenAt: seenAt,
		})
	}
	prepared := orderPhotoParams(params)
	batch := &pgx.Batch{}
	for _, item := range prepared {
		batch.Queue(photoInsertSQL, photoArgs(item)...)
	}
	for _, item := range prepared {
		batch.Queue(photoUpdateSQL, photoUpdateArgs(item)...)
	}
	return executeBatch(ctx, r.db, batch, "photos")
}

// orderPhotoParams sorts photos by id and keeps the last payload of a duplicate id, so every writer
// locks rows in the same order and one batch never touches a row twice.
func orderPhotoParams(params []gen.UpsertPhotoParams) []gen.UpsertPhotoParams {
	ordered := append([]gen.UpsertPhotoParams(nil), params...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	unique := ordered[:0]
	for _, item := range ordered {
		if n := len(unique); n > 0 && unique[n-1].ID == item.ID {
			unique[n-1] = item
			continue
		}
		unique = append(unique, item)
	}
	return unique
}

// photoUpdateArgs omits the sizes columns: a re-crawl never rewrites sizes (SetSizes owns them).
func photoUpdateArgs(p gen.UpsertPhotoParams) []any {
	return []any{
		p.ID, p.OwnerNsid, p.Secret, p.Server, p.Title, p.Description, p.Tags, p.TakenAt,
		p.UploadedAt, p.LastUpdate, p.Visibility, p.License, p.Media, p.Geo,
		p.Raw, p.ContentHash, p.TombstonedAt, p.LastSeenAt,
	}
}

func photoArgs(p gen.UpsertPhotoParams) []any {
	return []any{
		p.ID, p.OwnerNsid, p.Secret, p.Server, p.Title, p.Description, p.Tags, p.TakenAt,
		p.UploadedAt, p.LastUpdate, p.Visibility, p.License, p.Media, p.Geo, p.Sizes,
		p.SizesStatus, p.SizesCheckedAt, p.Raw, p.ContentHash, p.TombstonedAt, p.LastSeenAt,
	}
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
