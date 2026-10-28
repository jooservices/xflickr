package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres/gen"
	"github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/pkg/flickr"
)

// LoadPhotoFacetSource loads the photo version and current sizes state. Extra
// facets are gated on a successful size confirmation for this same version.
func (r *Repository) LoadPhotoFacetSource(ctx context.Context, id int64) (domain.PhotoFacetSource, bool, error) {
	if r == nil || r.db == nil || ctx == nil || id <= 0 {
		return domain.PhotoFacetSource{}, false, errors.New("catalog: photo facet database, context and id are required")
	}
	var source domain.PhotoFacetSource
	var status string
	var checked pgtype.Timestamptz
	err := r.db.QueryRow(ctx, `SELECT secret, last_update, sizes_status, sizes_checked_at FROM photos WHERE id=$1`, id).
		Scan(&source.Secret, &source.LastUpdate, &status, &checked)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PhotoFacetSource{}, false, nil
	}
	if err != nil {
		return domain.PhotoFacetSource{}, false, fmt.Errorf("catalog: load photo facet source: %w", err)
	}
	source.SizesStatus = domain.SizesStatus(status)
	if checked.Valid {
		source.SizesCheckedAt = &checked.Time
	}
	return source, true, nil
}

// SetPhotoFacet replaces one photo's optional facet data and records the
// photo version only after the data writes succeed.
func (r *Repository) SetPhotoFacet(ctx context.Context, photoID int64, facet string, data []byte, photoUpdatedAt, fetchedAt time.Time) error {
	if r == nil || r.db == nil || ctx == nil || photoID <= 0 || photoUpdatedAt.IsZero() || fetchedAt.IsZero() || !jsonDocument(data) {
		return errors.New("catalog: invalid photo facet update")
	}
	database := r.db
	var tx pgx.Tx
	if beginner, ok := database.(interface {
		Begin(context.Context) (pgx.Tx, error)
	}); ok {
		var err error
		tx, err = beginner.Begin(ctx)
		if err != nil {
			return fmt.Errorf("catalog: begin photo facet transaction: %w", err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		database = tx
	}
	if err := writePhotoFacet(ctx, database, photoID, facet, data, fetchedAt); err != nil {
		return fmt.Errorf("catalog: store photo %d %s facet: %w", photoID, facet, err)
	}
	_, err := database.Exec(ctx, `INSERT INTO photo_facet_status (photo_id, facet, photo_updated_at, fetched_at)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (photo_id,facet) DO UPDATE SET photo_updated_at=EXCLUDED.photo_updated_at, fetched_at=EXCLUDED.fetched_at
		WHERE (photo_facet_status.photo_updated_at,photo_facet_status.fetched_at) IS DISTINCT FROM (EXCLUDED.photo_updated_at,EXCLUDED.fetched_at)`,
		photoID, facet, photoUpdatedAt, fetchedAt)
	if err != nil {
		return fmt.Errorf("catalog: mark photo %d %s facet complete: %w", photoID, facet, err)
	}
	if tx != nil {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("catalog: commit photo %d %s facet: %w", photoID, facet, err)
		}
	}
	return nil
}

func writePhotoFacet(ctx context.Context, database gen.DBTX, photoID int64, facet string, data []byte, fetchedAt time.Time) error {
	var err error
	switch facet {
	case "info":
		_, err = database.Exec(ctx, `INSERT INTO photo_info (photo_id, data, fetched_at) VALUES ($1,$2,$3)
			ON CONFLICT (photo_id) DO UPDATE SET data=EXCLUDED.data,fetched_at=EXCLUDED.fetched_at`, photoID, data, fetchedAt)
	case "exif":
		err = setPhotoExif(ctx, database, photoID, data, fetchedAt)
	case "contexts":
		err = setPhotoContexts(ctx, database, photoID, data, fetchedAt)
	case "comments":
		err = setPhotoComments(ctx, database, photoID, data, fetchedAt)
	case "photo_favorites":
		err = setPhotoFavorites(ctx, database, photoID, data, fetchedAt)
	default:
		return fmt.Errorf("catalog: unsupported photo facet %q", facet)
	}
	if err != nil {
		return err
	}
	return nil
}

func setPhotoExif(ctx context.Context, database gen.DBTX, photoID int64, data []byte, fetchedAt time.Time) error {
	var exif flickr.PhotoExif
	if err := json.Unmarshal(data, &exif); err != nil {
		return fmt.Errorf("decode EXIF: %w", err)
	}
	_, err := database.Exec(ctx, `INSERT INTO photo_exif (photo_id, camera, data, fetched_at) VALUES ($1,$2,$3,$4)
		ON CONFLICT (photo_id) DO UPDATE SET camera=EXCLUDED.camera,data=EXCLUDED.data,fetched_at=EXCLUDED.fetched_at`, photoID, exif.Camera, data, fetchedAt)
	return err
}

func setPhotoContexts(ctx context.Context, database gen.DBTX, photoID int64, data []byte, fetchedAt time.Time) error {
	var contexts flickr.PhotoContexts
	if err := json.Unmarshal(data, &contexts); err != nil {
		return fmt.Errorf("decode contexts: %w", err)
	}
	if _, err := database.Exec(ctx, `DELETE FROM photo_contexts WHERE photo_id=$1`, photoID); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	for _, item := range contexts.Sets {
		if item.ID == "" {
			return errors.New("context set id is required")
		}
		batch.Queue(`INSERT INTO photo_contexts (photo_id,context_type,context_id,title,url,fetched_at) VALUES ($1,'set',$2,$3,$4,$5)
			ON CONFLICT (photo_id,context_type,context_id) DO UPDATE SET title=EXCLUDED.title,url=EXCLUDED.url,fetched_at=EXCLUDED.fetched_at`,
			photoID, string(item.ID), string(item.Title), string(item.URL), fetchedAt)
	}
	for _, item := range contexts.Pools {
		if item.ID == "" {
			return errors.New("context pool id is required")
		}
		batch.Queue(`INSERT INTO photo_contexts (photo_id,context_type,context_id,title,url,fetched_at) VALUES ($1,'pool',$2,$3,$4,$5)
			ON CONFLICT (photo_id,context_type,context_id) DO UPDATE SET title=EXCLUDED.title,url=EXCLUDED.url,fetched_at=EXCLUDED.fetched_at`,
			photoID, string(item.ID), string(item.Title), string(item.URL), fetchedAt)
	}
	return executeBatch(ctx, database, batch, "photo contexts")
}

func setPhotoComments(ctx context.Context, database gen.DBTX, photoID int64, data []byte, fetchedAt time.Time) error {
	var items []flickr.PhotoComment
	if err := json.Unmarshal(data, &items); err != nil {
		return fmt.Errorf("decode comments: %w", err)
	}
	if _, err := database.Exec(ctx, `DELETE FROM photo_comments WHERE photo_id=$1`, photoID); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	for _, item := range items {
		if item.ID == "" {
			return errors.New("photo comment id is required")
		}
		batch.Queue(`INSERT INTO photo_comments (photo_id,comment_id,author_nsid,author_name,date_created,permalink,body,fetched_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			photoID, item.ID, item.Author, item.AuthorName, unixTimeValue(string(item.DateCreate)), item.Permalink, string(item.Text), fetchedAt)
	}
	return executeBatch(ctx, database, batch, "photo comments")
}

func setPhotoFavorites(ctx context.Context, database gen.DBTX, photoID int64, data []byte, fetchedAt time.Time) error {
	var items []flickr.PhotoFavorite
	if err := json.Unmarshal(data, &items); err != nil {
		return fmt.Errorf("decode photo favorites: %w", err)
	}
	if _, err := database.Exec(ctx, `DELETE FROM photo_favorites WHERE photo_id=$1`, photoID); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	for _, item := range items {
		if item.NSID == "" {
			return errors.New("photo favorite nsid is required")
		}
		batch.Queue(`INSERT INTO photo_favorites (photo_id,person_nsid,username,realname,fave_date,fetched_at)
			VALUES ($1,$2,$3,$4,$5,$6)`,
			photoID, item.NSID, item.Username, item.RealName, unixTimeValue(string(item.FaveDate)), fetchedAt)
	}
	return executeBatch(ctx, database, batch, "photo favorites")
}

func unixTimeValue(value string) *time.Time {
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || seconds <= 0 {
		return nil
	}
	parsed := time.Unix(seconds, 0).UTC()
	return &parsed
}
