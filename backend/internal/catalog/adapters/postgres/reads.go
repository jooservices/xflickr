package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/jooservices/xflickr/internal/catalog/app"
	sharingapp "github.com/jooservices/xflickr/internal/sharing/app"
	sharingdomain "github.com/jooservices/xflickr/internal/sharing/domain"
)

var _ app.ReadRepository = (*Repository)(nil)

const photoReadColumns = `photos.id, photos.owner_nsid, photos.title, photos.description,
photos.tags, photos.taken_at, photos.uploaded_at, photos.last_update, photos.visibility,
photos.sizes_status, photos.sizes,
CASE WHEN EXISTS (SELECT 1 FROM photo_enrichments e WHERE e.photo_id=photos.id AND e.kind='caption') THEN 'ready' ELSE 'none' END`

type rowScanner interface{ Scan(...any) error }

func validateRead(ctx context.Context, r *Repository, userID int64, predicate sharingapp.SQLPredicate, limit int) error {
	if ctx == nil || r == nil || r.db == nil || userID <= 0 || limit < 1 || limit > app.MaxPageSize {
		return errors.New("catalog: invalid read request")
	}
	expected := sharingapp.Filter(sharingdomain.User{ID: userID}).SQL
	if predicate.Expression != expected.Expression || len(predicate.Args) != 1 {
		return errors.New("catalog: invalid visibility predicate")
	}
	actor, ok := predicate.Args[0].(int64)
	if !ok || actor != userID {
		return errors.New("catalog: visibility predicate does not match actor")
	}
	return nil
}

func scanPhoto(row rowScanner) (app.Photo, error) {
	var photo app.Photo
	var takenAt, uploadedAt pgtype.Timestamptz
	var tags []string
	var sizes []byte
	err := row.Scan(&photo.ID, &photo.OwnerNSID, &photo.Title, &photo.Description,
		&tags, &takenAt, &uploadedAt, &photo.LastUpdate, &photo.Visibility,
		&photo.SizesStatus, &sizes, &photo.AIStatus)
	if err != nil {
		return app.Photo{}, err
	}
	photo.Labels = tags
	if photo.Labels == nil {
		photo.Labels = []string{}
	}
	if err := json.Unmarshal(sizes, &photo.Sizes); err != nil {
		return app.Photo{}, fmt.Errorf("catalog: decode photo sizes: %w", err)
	}
	if photo.Sizes == nil {
		photo.Sizes = []app.Size{}
	}
	if takenAt.Valid {
		photo.TakenAt = &takenAt.Time
	}
	if uploadedAt.Valid {
		photo.UploadedAt = &uploadedAt.Time
	}
	return photo, nil
}

func (r *Repository) ListPeople(ctx context.Context, userID int64, predicate sharingapp.SQLPredicate, cursor *app.PersonCursor, limit int) (app.PeoplePage, error) {
	if err := validateRead(ctx, r, userID, predicate, limit); err != nil {
		return app.PeoplePage{}, err
	}
	query := `SELECT people.nsid, people.username, people.realname, people.icon,
(SELECT count(*) FROM photos WHERE photos.owner_nsid=people.nsid AND photos.tombstoned_at IS NULL AND ` + predicate.Expression + `)
FROM people
WHERE ((EXISTS (SELECT 1 FROM observations o WHERE o.user_id=$1 AND o.record_kind='person' AND o.record_id=people.nsid))
OR EXISTS (SELECT 1 FROM photos WHERE photos.owner_nsid=people.nsid AND photos.tombstoned_at IS NULL AND ` + predicate.Expression + `))`
	args := append([]any(nil), predicate.Args...)
	if cursor != nil {
		if cursor.NSID == "" {
			return app.PeoplePage{}, app.ErrInvalidCursor
		}
		query += ` AND people.nsid > $2 ORDER BY people.nsid ASC LIMIT $3`
		args = append(args, cursor.NSID, limit+1)
	} else {
		query += ` ORDER BY people.nsid ASC LIMIT $2`
		args = append(args, limit+1)
	}
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return app.PeoplePage{}, fmt.Errorf("catalog: list visible people: %w", err)
	}
	defer rows.Close()
	page := app.PeoplePage{Items: make([]app.Person, 0, limit)}
	for rows.Next() {
		var person app.Person
		if err := rows.Scan(&person.NSID, &person.Username, &person.Realname, &person.Icon, &person.PhotosCount); err != nil {
			return app.PeoplePage{}, fmt.Errorf("catalog: scan visible person: %w", err)
		}
		page.Items = append(page.Items, person)
	}
	if err := rows.Err(); err != nil {
		return app.PeoplePage{}, fmt.Errorf("catalog: list visible people: %w", err)
	}
	if len(page.Items) > limit {
		last := page.Items[limit-1]
		page.Items = page.Items[:limit]
		page.NextCursor = &app.PersonCursor{NSID: last.NSID}
	}
	return page, nil
}

func (r *Repository) GetPerson(ctx context.Context, userID int64, nsid string, predicate sharingapp.SQLPredicate) (app.Person, bool, error) {
	if err := validateRead(ctx, r, userID, predicate, app.DefaultPageSize); err != nil || strings.TrimSpace(nsid) == "" {
		if err == nil {
			err = errors.New("catalog: person nsid is required")
		}
		return app.Person{}, false, err
	}
	query := `SELECT people.nsid, people.username, people.realname, people.icon,
(SELECT count(*) FROM photos WHERE photos.owner_nsid=people.nsid AND photos.tombstoned_at IS NULL AND ` + predicate.Expression + `)
FROM people WHERE people.nsid=$2 AND (
EXISTS (SELECT 1 FROM observations o WHERE o.user_id=$1 AND o.record_kind='person' AND o.record_id=people.nsid)
OR EXISTS (SELECT 1 FROM photos WHERE photos.owner_nsid=people.nsid AND photos.tombstoned_at IS NULL AND ` + predicate.Expression + `))`
	var person app.Person
	err := r.db.QueryRow(ctx, query, append(append([]any(nil), predicate.Args...), nsid)...).Scan(&person.NSID, &person.Username, &person.Realname, &person.Icon, &person.PhotosCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Person{}, false, nil
	}
	if err != nil {
		return app.Person{}, false, fmt.Errorf("catalog: get visible person: %w", err)
	}
	return person, true, nil
}

func (r *Repository) ListPersonPhotos(ctx context.Context, userID int64, nsid string, predicate sharingapp.SQLPredicate, cursor *app.PhotoCursor, limit int) (app.PhotosPage, error) {
	if err := validateRead(ctx, r, userID, predicate, limit); err != nil {
		return app.PhotosPage{}, err
	}
	if _, ok, err := r.GetPerson(ctx, userID, nsid, predicate); err != nil {
		return app.PhotosPage{}, err
	} else if !ok {
		return app.PhotosPage{}, app.ErrNotFound
	}
	query := `SELECT ` + photoReadColumns + ` FROM photos WHERE photos.owner_nsid=$2 AND photos.tombstoned_at IS NULL AND ` + predicate.Expression
	args := append([]any(nil), predicate.Args...)
	if cursor != nil {
		if cursor.LastUpdate.IsZero() || cursor.ID <= 0 {
			return app.PhotosPage{}, app.ErrInvalidCursor
		}
		query += ` AND (photos.last_update,photos.id)<($3,$4) ORDER BY photos.last_update DESC,photos.id DESC LIMIT $5`
		args = append(args, nsid, cursor.LastUpdate, cursor.ID, limit+1)
	} else {
		query += ` ORDER BY photos.last_update DESC,photos.id DESC LIMIT $3`
		args = append(args, nsid, limit+1)
	}
	return collectPhotoPage(ctx, r, query, args, limit)
}

func (r *Repository) ListVisibleFavorites(ctx context.Context, userID int64, predicate sharingapp.SQLPredicate, cursor *app.FavoriteCursor, limit int) (app.FavoritesPage, error) {
	if err := validateRead(ctx, r, userID, predicate, limit); err != nil {
		return app.FavoritesPage{}, err
	}
	query := `SELECT ` + photoReadColumns + `, favorites.fave_date
FROM favorites JOIN flickr_accounts ON flickr_accounts.nsid=favorites.person_nsid
JOIN photos ON photos.id=favorites.photo_id
WHERE flickr_accounts.user_id=$1 AND flickr_accounts.status<>'revoked' AND favorites.removed_at IS NULL
AND photos.tombstoned_at IS NULL AND ` + predicate.Expression
	args := append([]any(nil), predicate.Args...)
	if cursor != nil {
		if cursor.FaveDate.IsZero() || cursor.PhotoID <= 0 {
			return app.FavoritesPage{}, app.ErrInvalidCursor
		}
		query += ` AND (favorites.fave_date,photos.id)<($2,$3) ORDER BY favorites.fave_date DESC,photos.id DESC LIMIT $4`
		args = append(args, cursor.FaveDate, cursor.PhotoID, limit+1)
	} else {
		query += ` ORDER BY favorites.fave_date DESC,photos.id DESC LIMIT $2`
		args = append(args, limit+1)
	}
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return app.FavoritesPage{}, fmt.Errorf("catalog: list visible favorites: %w", err)
	}
	defer rows.Close()
	page := app.FavoritesPage{Items: make([]app.Favorite, 0, limit)}
	for rows.Next() {
		var favorite app.Favorite
		var faveDate pgtype.Timestamptz
		photo, err := scanPhotoWithFavorite(rows, &faveDate)
		if err != nil {
			return app.FavoritesPage{}, fmt.Errorf("catalog: scan visible favorite: %w", err)
		}
		favorite.Photo = photo
		favorite.FaveDate = faveDate.Time
		page.Items = append(page.Items, favorite)
	}
	if err := rows.Err(); err != nil {
		return app.FavoritesPage{}, fmt.Errorf("catalog: list visible favorites: %w", err)
	}
	if len(page.Items) > limit {
		last := page.Items[limit-1]
		page.Items = page.Items[:limit]
		page.NextCursor = &app.FavoriteCursor{FaveDate: last.FaveDate, PhotoID: last.Photo.ID}
	}
	return page, nil
}

func scanPhotoWithFavorite(row rowScanner, faveDate *pgtype.Timestamptz) (app.Photo, error) {
	var photo app.Photo
	var takenAt, uploadedAt pgtype.Timestamptz
	var tags []string
	var sizes []byte
	err := row.Scan(&photo.ID, &photo.OwnerNSID, &photo.Title, &photo.Description,
		&tags, &takenAt, &uploadedAt, &photo.LastUpdate, &photo.Visibility,
		&photo.SizesStatus, &sizes, &photo.AIStatus, faveDate)
	if err != nil {
		return app.Photo{}, err
	}
	photo.Labels = tags
	if err := json.Unmarshal(sizes, &photo.Sizes); err != nil {
		return app.Photo{}, err
	}
	if photo.Labels == nil {
		photo.Labels = []string{}
	}
	if photo.Sizes == nil {
		photo.Sizes = []app.Size{}
	}
	if takenAt.Valid {
		photo.TakenAt = &takenAt.Time
	}
	if uploadedAt.Valid {
		photo.UploadedAt = &uploadedAt.Time
	}
	return photo, nil
}

func (r *Repository) ListVisibleAlbums(ctx context.Context, userID int64, predicate sharingapp.SQLPredicate, cursor *app.AlbumCursor, limit int) (app.AlbumsPage, error) {
	if err := validateRead(ctx, r, userID, predicate, limit); err != nil {
		return app.AlbumsPage{}, err
	}
	query := `SELECT albums.id,albums.owner_nsid,albums.title,albums.description,
CASE WHEN albums.primary_photo_id IS NOT NULL AND EXISTS (SELECT 1 FROM photos WHERE photos.id=albums.primary_photo_id AND photos.tombstoned_at IS NULL AND ` + predicate.Expression + `) THEN albums.primary_photo_id ELSE NULL END,
count(*),albums.created_at,albums.updated_at
FROM albums JOIN flickr_accounts ON flickr_accounts.nsid=albums.owner_nsid
JOIN album_photos ON album_photos.album_id=albums.id
JOIN photos ON photos.id=album_photos.photo_id AND photos.tombstoned_at IS NULL
WHERE flickr_accounts.user_id=$1 AND flickr_accounts.status<>'revoked' AND ` + predicate.Expression
	args := append([]any(nil), predicate.Args...)
	if cursor != nil {
		if cursor.ID == "" {
			return app.AlbumsPage{}, app.ErrInvalidCursor
		}
		if cursor.UpdatedAt == nil {
			query += ` AND albums.updated_at IS NULL AND albums.id<$2`
			args = append(args, cursor.ID, limit+1)
			query += ` GROUP BY albums.id ORDER BY albums.updated_at DESC NULLS LAST,albums.id DESC LIMIT $3`
		} else {
			query += ` AND ((albums.updated_at < $2) OR albums.updated_at IS NULL OR (albums.updated_at=$2 AND albums.id<$3))`
			args = append(args, *cursor.UpdatedAt, cursor.ID, limit+1)
			query += ` GROUP BY albums.id ORDER BY albums.updated_at DESC NULLS LAST,albums.id DESC LIMIT $4`
		}
	} else {
		query += ` GROUP BY albums.id ORDER BY albums.updated_at DESC NULLS LAST,albums.id DESC LIMIT $2`
		args = append(args, limit+1)
	}
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return app.AlbumsPage{}, fmt.Errorf("catalog: list visible albums: %w", err)
	}
	defer rows.Close()
	page := app.AlbumsPage{Items: make([]app.Album, 0, limit)}
	for rows.Next() {
		var album app.Album
		var primary pgtype.Int8
		var created, updated pgtype.Timestamptz
		if err := rows.Scan(&album.ID, &album.OwnerNSID, &album.Title, &album.Description, &primary, &album.VisiblePhotosCount, &created, &updated); err != nil {
			return app.AlbumsPage{}, fmt.Errorf("catalog: scan visible album: %w", err)
		}
		if primary.Valid {
			album.PrimaryPhotoID = &primary.Int64
		}
		if created.Valid {
			album.CreatedAt = &created.Time
		}
		if updated.Valid {
			album.UpdatedAt = &updated.Time
		}
		page.Items = append(page.Items, album)
	}
	if err := rows.Err(); err != nil {
		return app.AlbumsPage{}, fmt.Errorf("catalog: list visible albums: %w", err)
	}
	if len(page.Items) > limit {
		last := page.Items[limit-1]
		page.Items = page.Items[:limit]
		page.NextCursor = &app.AlbumCursor{UpdatedAt: last.UpdatedAt, ID: last.ID}
	}
	return page, nil
}

func (r *Repository) ListVisibleAlbumPhotos(ctx context.Context, userID int64, albumID string, predicate sharingapp.SQLPredicate, cursor *app.AlbumPhotoCursor, limit int) (app.AlbumPhotosPage, error) {
	if err := validateRead(ctx, r, userID, predicate, limit); err != nil {
		return app.AlbumPhotosPage{}, err
	}
	if strings.TrimSpace(albumID) == "" {
		return app.AlbumPhotosPage{}, app.ErrNotFound
	}
	visibleAlbumSQL := `SELECT EXISTS (SELECT 1 FROM albums
JOIN flickr_accounts ON flickr_accounts.nsid=albums.owner_nsid
WHERE albums.id=$2 AND flickr_accounts.user_id=$1 AND flickr_accounts.status<>'revoked'
AND EXISTS (SELECT 1 FROM album_photos WHERE album_photos.album_id=albums.id
AND EXISTS (SELECT 1 FROM photos WHERE photos.id=album_photos.photo_id AND photos.tombstoned_at IS NULL AND ` + predicate.Expression + `)))`
	var visible bool
	if err := r.db.QueryRow(ctx, visibleAlbumSQL, predicate.Args[0], albumID).Scan(&visible); err != nil {
		return app.AlbumPhotosPage{}, fmt.Errorf("catalog: check visible album: %w", err)
	}
	if !visible {
		return app.AlbumPhotosPage{}, app.ErrNotFound
	}
	query := `SELECT ` + photoReadColumns + `,album_photos.position FROM album_photos JOIN photos ON photos.id=album_photos.photo_id
WHERE album_photos.album_id=$2 AND photos.tombstoned_at IS NULL AND ` + predicate.Expression
	args := append([]any(nil), predicate.Args...)
	if cursor != nil {
		if cursor.Position < 0 || cursor.PhotoID <= 0 {
			return app.AlbumPhotosPage{}, app.ErrInvalidCursor
		}
		query += ` AND (album_photos.position,photos.id)>($3,$4) ORDER BY album_photos.position,photos.id LIMIT $5`
		args = append(args, albumID, cursor.Position, cursor.PhotoID, limit+1)
	} else {
		query += ` ORDER BY album_photos.position,photos.id LIMIT $3`
		args = append(args, albumID, limit+1)
	}
	return collectAlbumPhotoPage(ctx, r, query, args, limit)
}

func (r *Repository) GetPhoto(ctx context.Context, userID, photoID int64, predicate sharingapp.SQLPredicate) (app.Photo, bool, error) {
	if err := validateRead(ctx, r, userID, predicate, app.DefaultPageSize); err != nil || photoID <= 0 {
		if err == nil {
			err = errors.New("catalog: photo id is required")
		}
		return app.Photo{}, false, err
	}
	query := `SELECT ` + photoReadColumns + ` FROM photos WHERE photos.id=$2 AND photos.tombstoned_at IS NULL AND ` + predicate.Expression
	photo, err := scanPhoto(r.db.QueryRow(ctx, query, append(append([]any(nil), predicate.Args...), photoID)...))
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Photo{}, false, nil
	}
	if err != nil {
		return app.Photo{}, false, fmt.Errorf("catalog: get visible photo: %w", err)
	}
	return photo, true, nil
}

func collectPhotoPage(ctx context.Context, r *Repository, query string, args []any, limit int) (app.PhotosPage, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return app.PhotosPage{}, fmt.Errorf("catalog: list visible photos: %w", err)
	}
	defer rows.Close()
	page := app.PhotosPage{Items: make([]app.Photo, 0, limit)}
	for rows.Next() {
		photo, err := scanPhoto(rows)
		if err != nil {
			return app.PhotosPage{}, fmt.Errorf("catalog: scan visible photo: %w", err)
		}
		page.Items = append(page.Items, photo)
	}
	if err := rows.Err(); err != nil {
		return app.PhotosPage{}, fmt.Errorf("catalog: list visible photos: %w", err)
	}
	if len(page.Items) > limit {
		last := page.Items[limit-1]
		page.Items = page.Items[:limit]
		page.NextCursor = &app.PhotoCursor{LastUpdate: last.LastUpdate, ID: last.ID}
	}
	return page, nil
}

func collectAlbumPhotoPage(ctx context.Context, r *Repository, query string, args []any, limit int) (app.AlbumPhotosPage, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return app.AlbumPhotosPage{}, fmt.Errorf("catalog: list visible album photos: %w", err)
	}
	defer rows.Close()
	page := app.AlbumPhotosPage{Items: make([]app.Photo, 0, limit)}
	positions := make([]int, 0, limit+1)
	for rows.Next() {
		photo, position, err := scanPhotoWithPosition(rows)
		if err != nil {
			return app.AlbumPhotosPage{}, fmt.Errorf("catalog: scan visible album photo: %w", err)
		}
		page.Items = append(page.Items, photo)
		positions = append(positions, position)
	}
	if err := rows.Err(); err != nil {
		return app.AlbumPhotosPage{}, fmt.Errorf("catalog: list visible album photos: %w", err)
	}
	if len(page.Items) > limit {
		last := page.Items[limit-1]
		position := positions[limit-1]
		page.Items = page.Items[:limit]
		page.NextCursor = &app.AlbumPhotoCursor{Position: position, PhotoID: last.ID}
	}
	return page, nil
}

func scanPhotoWithPosition(row rowScanner) (app.Photo, int, error) {
	var photo app.Photo
	var takenAt, uploadedAt pgtype.Timestamptz
	var tags []string
	var sizes []byte
	var position int
	err := row.Scan(&photo.ID, &photo.OwnerNSID, &photo.Title, &photo.Description,
		&tags, &takenAt, &uploadedAt, &photo.LastUpdate, &photo.Visibility,
		&photo.SizesStatus, &sizes, &photo.AIStatus, &position)
	if err != nil {
		return app.Photo{}, 0, err
	}
	if err := json.Unmarshal(sizes, &photo.Sizes); err != nil {
		return app.Photo{}, 0, fmt.Errorf("catalog: decode photo sizes: %w", err)
	}
	photo.Labels = tags
	if photo.Labels == nil {
		photo.Labels = []string{}
	}
	if photo.Sizes == nil {
		photo.Sizes = []app.Size{}
	}
	if takenAt.Valid {
		photo.TakenAt = &takenAt.Time
	}
	if uploadedAt.Valid {
		photo.UploadedAt = &uploadedAt.Time
	}
	return photo, position, nil
}
