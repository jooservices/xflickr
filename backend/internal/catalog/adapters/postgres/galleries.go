package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Gallery stores Flickr gallery metadata.
type Gallery struct {
	ID, OwnerNSID, Title, Description string
	PrimaryPhotoID                    *int64
	PhotosCount                       int64
	CreatedAt, UpdatedAt, SeenAt      time.Time
}

// GalleryPhoto records a photo's position in a gallery.
type GalleryPhoto struct {
	GalleryID string
	PhotoID   int64
	Position  int
	SeenAt    time.Time
}

// UpsertGalleriesBatch inserts or refreshes gallery metadata.
func (r *Repository) UpsertGalleriesBatch(ctx context.Context, galleries []Gallery) error {
	if err := validateBatchSize(len(galleries)); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	for _, g := range galleries {
		if g.ID == "" || g.OwnerNSID == "" || g.PhotosCount < 0 {
			return errors.New("catalog: invalid gallery")
		}
		batch.Queue(`INSERT INTO galleries (id,owner_nsid,title,description,primary_photo_id,photos_count,created_at,updated_at,seen_at) VALUES ($1,$2,$3,$4,CASE WHEN $5::bigint IS NOT NULL AND EXISTS (SELECT 1 FROM photos WHERE id=$5) THEN $5::bigint ELSE NULL END,$6,$7,$8,$9) ON CONFLICT (id) DO UPDATE SET owner_nsid=EXCLUDED.owner_nsid,title=EXCLUDED.title,description=EXCLUDED.description,primary_photo_id=EXCLUDED.primary_photo_id,photos_count=EXCLUDED.photos_count,created_at=EXCLUDED.created_at,updated_at=EXCLUDED.updated_at,seen_at=EXCLUDED.seen_at WHERE (galleries.owner_nsid,galleries.title,galleries.description,galleries.primary_photo_id,galleries.photos_count,galleries.created_at,galleries.updated_at,galleries.seen_at) IS DISTINCT FROM (EXCLUDED.owner_nsid,EXCLUDED.title,EXCLUDED.description,EXCLUDED.primary_photo_id,EXCLUDED.photos_count,EXCLUDED.created_at,EXCLUDED.updated_at,EXCLUDED.seen_at)`, g.ID, g.OwnerNSID, g.Title, g.Description, g.PrimaryPhotoID, g.PhotosCount, nullableTime(g.CreatedAt), nullableTime(g.UpdatedAt), seenAt(g.SeenAt))
	}
	return executeBatch(ctx, r.db, batch, "galleries")
}

// UpsertGalleryPhotosBatch inserts or refreshes gallery membership rows.
func (r *Repository) UpsertGalleryPhotosBatch(ctx context.Context, photos []GalleryPhoto) error {
	if err := validateBatchSize(len(photos)); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	for _, p := range photos {
		if p.GalleryID == "" || p.PhotoID <= 0 || p.Position < 0 {
			return errors.New("catalog: invalid gallery photo")
		}
		batch.Queue(`INSERT INTO gallery_photos (gallery_id,photo_id,position,seen_at) VALUES ($1,$2,$3,$4) ON CONFLICT (gallery_id,photo_id) DO UPDATE SET position=EXCLUDED.position,seen_at=EXCLUDED.seen_at WHERE (gallery_photos.position,gallery_photos.seen_at) IS DISTINCT FROM (EXCLUDED.position,EXCLUDED.seen_at)`, p.GalleryID, p.PhotoID, p.Position, seenAt(p.SeenAt))
	}
	return executeBatch(ctx, r.db, batch, "gallery photos")
}
