package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Album stores Flickr photoset metadata.
type Album struct {
	ID, OwnerNSID, Title, Description string
	PrimaryPhotoID                    *int64
	PhotosCount                       int64
	CreatedAt, UpdatedAt, SeenAt      time.Time
}

// AlbumPhoto is a photo's position in an album.
type AlbumPhoto struct {
	AlbumID  string
	PhotoID  int64
	Position int
	SeenAt   time.Time
}

// AlbumPhotoCursor continues an ascending position/photo-id keyset page.
type AlbumPhotoCursor struct {
	Position int
	PhotoID  int64
}

// AlbumPhotoPage contains one page and a cursor when more rows may follow.
type AlbumPhotoPage struct {
	Items      []AlbumPhoto
	NextCursor *AlbumPhotoCursor
}

// UpsertAlbumsBatch inserts or refreshes album metadata.
func (r *Repository) UpsertAlbumsBatch(ctx context.Context, albums []Album) error {
	if err := validateBatchSize(len(albums)); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	for _, a := range albums {
		if a.ID == "" || a.OwnerNSID == "" || a.PhotosCount < 0 {
			return errors.New("catalog: invalid album")
		}
		batch.Queue(`INSERT INTO albums (id,owner_nsid,primary_photo_id,title,description,photos_count,created_at,updated_at,seen_at) VALUES ($1,$2,CASE WHEN $3::bigint IS NOT NULL AND EXISTS (SELECT 1 FROM photos WHERE id=$3) THEN $3::bigint ELSE NULL END,$4,$5,$6,$7,$8,$9) ON CONFLICT (id) DO UPDATE SET owner_nsid=EXCLUDED.owner_nsid, primary_photo_id=EXCLUDED.primary_photo_id, title=EXCLUDED.title, description=EXCLUDED.description, photos_count=EXCLUDED.photos_count, created_at=EXCLUDED.created_at, updated_at=EXCLUDED.updated_at, seen_at=EXCLUDED.seen_at WHERE (albums.owner_nsid,albums.primary_photo_id,albums.title,albums.description,albums.photos_count,albums.created_at,albums.updated_at,albums.seen_at) IS DISTINCT FROM (EXCLUDED.owner_nsid,EXCLUDED.primary_photo_id,EXCLUDED.title,EXCLUDED.description,EXCLUDED.photos_count,EXCLUDED.created_at,EXCLUDED.updated_at,EXCLUDED.seen_at)`, a.ID, a.OwnerNSID, a.PrimaryPhotoID, a.Title, a.Description, a.PhotosCount, nullableTime(a.CreatedAt), nullableTime(a.UpdatedAt), seenAt(a.SeenAt))
	}
	return executeBatch(ctx, r.db, batch, "albums")
}

// UpsertAlbumPhotosBatch inserts or refreshes album membership rows.
func (r *Repository) UpsertAlbumPhotosBatch(ctx context.Context, photos []AlbumPhoto) error {
	if err := validateBatchSize(len(photos)); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	for _, p := range photos {
		if p.AlbumID == "" || p.PhotoID <= 0 || p.Position < 0 {
			return errors.New("catalog: invalid album photo")
		}
		batch.Queue(`INSERT INTO album_photos (album_id,photo_id,position,seen_at) VALUES ($1,$2,$3,$4) ON CONFLICT (album_id,photo_id) DO UPDATE SET position=EXCLUDED.position,seen_at=EXCLUDED.seen_at WHERE (album_photos.position,album_photos.seen_at) IS DISTINCT FROM (EXCLUDED.position,EXCLUDED.seen_at)`, p.AlbumID, p.PhotoID, p.Position, seenAt(p.SeenAt))
	}
	return executeBatch(ctx, r.db, batch, "album photos")
}

// ListAlbums lists albums for a person in stable newest-update-first order.
func (r *Repository) ListAlbums(ctx context.Context, person string) ([]Album, error) {
	if person == "" {
		return nil, errors.New("catalog: person is required")
	}
	rows, err := r.db.Query(ctx, `SELECT id,owner_nsid,primary_photo_id,title,description,photos_count,created_at,updated_at,seen_at FROM albums WHERE owner_nsid=$1 ORDER BY updated_at DESC NULLS LAST,id DESC`, person)
	if err != nil {
		return nil, fmt.Errorf("catalog: list albums: %w", err)
	}
	defer rows.Close()
	items := make([]Album, 0)
	for rows.Next() {
		var a Album
		var primary *int64
		var created, updated *time.Time
		if err := rows.Scan(&a.ID, &a.OwnerNSID, &primary, &a.Title, &a.Description, &a.PhotosCount, &created, &updated, &a.SeenAt); err != nil {
			return nil, fmt.Errorf("catalog: scan album: %w", err)
		}
		a.PrimaryPhotoID = primary
		if created != nil {
			a.CreatedAt = *created
		}
		if updated != nil {
			a.UpdatedAt = *updated
		}
		items = append(items, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: list albums: %w", err)
	}
	return items, nil
}

// ListAlbumPhotos returns at most limit photos in position/photo-id keyset order.
func (r *Repository) ListAlbumPhotos(ctx context.Context, album string, cursor *AlbumPhotoCursor, limit int) (AlbumPhotoPage, error) {
	if album == "" || limit < 1 || limit > 500 {
		return AlbumPhotoPage{}, errors.New("catalog: invalid album photos page request")
	}
	var rows pgx.Rows
	var err error
	if cursor == nil {
		rows, err = r.db.Query(ctx, `SELECT album_id,photo_id,position,seen_at FROM album_photos WHERE album_id=$1 ORDER BY position,photo_id LIMIT $2`, album, limit+1)
	} else {
		if cursor.Position < 0 || cursor.PhotoID <= 0 {
			return AlbumPhotoPage{}, errors.New("catalog: invalid album photos cursor")
		}
		rows, err = r.db.Query(ctx, `SELECT album_id,photo_id,position,seen_at FROM album_photos WHERE album_id=$1 AND (position,photo_id)>($2,$3) ORDER BY position,photo_id LIMIT $4`, album, cursor.Position, cursor.PhotoID, limit+1)
	}
	if err != nil {
		return AlbumPhotoPage{}, fmt.Errorf("catalog: list album photos: %w", err)
	}
	defer rows.Close()
	items := make([]AlbumPhoto, 0, limit+1)
	for rows.Next() {
		var p AlbumPhoto
		if err := rows.Scan(&p.AlbumID, &p.PhotoID, &p.Position, &p.SeenAt); err != nil {
			return AlbumPhotoPage{}, fmt.Errorf("catalog: scan album photo: %w", err)
		}
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return AlbumPhotoPage{}, fmt.Errorf("catalog: list album photos: %w", err)
	}
	page := AlbumPhotoPage{Items: items}
	if len(items) > limit {
		last := items[limit-1]
		page.Items = items[:limit]
		page.NextCursor = &AlbumPhotoCursor{Position: last.Position, PhotoID: last.PhotoID}
	}
	return page, nil
}

func nullableTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
