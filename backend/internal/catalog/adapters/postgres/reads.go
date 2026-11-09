package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/jooservices/xflickr/internal/catalog/app"
	sharingapp "github.com/jooservices/xflickr/internal/sharing/app"
	sharingdomain "github.com/jooservices/xflickr/internal/sharing/domain"
)

var _ app.ReadRepository = (*Repository)(nil)

const (
	contactAlbumPhotoProbeLimit = 128
	photoReadColumns            = `photos.id, photos.owner_nsid, photos.title, photos.description,
photos.tags, photos.taken_at, photos.uploaded_at, photos.last_update, photos.visibility,
photos.sizes_status, photos.sizes,
CASE WHEN EXISTS (SELECT 1 FROM photo_enrichments e WHERE e.photo_id=photos.id AND e.kind='caption') THEN 'ready' ELSE 'none' END`
)

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
	scanned, err := r.scanFavoriteCandidates(ctx, predicate, cursor, limit+2)
	if err != nil {
		return app.FavoritesPage{}, err
	}
	return r.visibleFavoritePage(ctx, predicate, scanned, limit)
}

func (r *Repository) scanFavoriteCandidates(ctx context.Context, predicate sharingapp.SQLPredicate, cursor *app.FavoriteCursor, fetchLimit int) ([]favoriteCandidate, error) {
	query := `WITH account_favorites AS MATERIALIZED (
SELECT account_favorites.flickr_account_id, account_favorites.photo_id, account_favorites.fave_date, account_favorites.removed_at
FROM (SELECT id, nsid FROM flickr_accounts WHERE user_id=$1 AND status<>'revoked') accounts
CROSS JOIN LATERAL (
SELECT accounts.id AS flickr_account_id, favorites.photo_id, favorites.fave_date, favorites.removed_at
FROM favorites
WHERE favorites.person_nsid=accounts.nsid`
	args := append([]any(nil), predicate.Args...)
	pageLimit := "$2"
	accountPageLimit := pageLimit
	scanRows := "account_favorites"
	if cursor != nil {
		if cursor.FaveDate.IsZero() || cursor.PhotoID <= 0 || cursor.FlickrAccountID < 0 {
			return nil, app.ErrInvalidCursor
		}
		query += ` AND (favorites.fave_date,favorites.photo_id)<=($2,$3)`
		args = append(args, cursor.FaveDate, cursor.PhotoID, cursor.FlickrAccountID)
		accountPageLimit = "$5"
		pageLimit = "$6"
		scanRows = "cursor_favorites"
	}
	query += ` ORDER BY favorites.fave_date DESC,favorites.photo_id DESC,accounts.id DESC LIMIT ` + accountPageLimit + `
 ) account_favorites
),`
	if cursor != nil {
		query += ` cursor_favorites AS MATERIALIZED (
SELECT flickr_account_id, photo_id, fave_date, removed_at FROM account_favorites
WHERE (fave_date,photo_id)<($2,$3) OR ((fave_date,photo_id)=($2,$3) AND flickr_account_id<$4)
),`
		args = append(args, fetchLimit+1, fetchLimit)
	} else {
		args = append(args, fetchLimit)
	}
	query += ` scan_page AS MATERIALIZED (
SELECT flickr_account_id, photo_id, fave_date, removed_at FROM ` + scanRows + `
ORDER BY fave_date DESC,photo_id DESC,flickr_account_id DESC LIMIT ` + pageLimit + `
)

SELECT flickr_account_id, photo_id, fave_date, removed_at FROM scan_page
ORDER BY fave_date DESC,photo_id DESC,flickr_account_id DESC`
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("catalog: scan favorite batch: %w", err)
	}
	defer rows.Close()
	scanned := make([]favoriteCandidate, 0, fetchLimit)
	for rows.Next() {
		var candidate favoriteCandidate
		if err := rows.Scan(&candidate.FlickrAccountID, &candidate.PhotoID, &candidate.FaveDate, &candidate.RemovedAt); err != nil {
			return nil, fmt.Errorf("catalog: scan favorite batch: %w", err)
		}
		scanned = append(scanned, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: scan favorite batch: %w", err)
	}
	return scanned, nil
}

func (r *Repository) visibleFavoritePage(ctx context.Context, predicate sharingapp.SQLPredicate, scanned []favoriteCandidate, limit int) (app.FavoritesPage, error) {
	scanLimit := limit + 1
	more := len(scanned) > scanLimit
	if more {
		scanned = scanned[:scanLimit]
	}
	page := app.FavoritesPage{Items: make([]app.Favorite, 0, limit)}
	visibleCandidates := make([]favoriteCandidate, 0, limit)
	if len(scanned) == 0 {
		return page, nil
	}

	photos, err := r.visibleFavoritePhotos(ctx, predicate, favoritePhotoIDs(scanned))
	if err != nil {
		return app.FavoritesPage{}, err
	}
	for _, candidate := range scanned {
		if candidate.RemovedAt.Valid {
			continue
		}
		photo, ok := photos[candidate.PhotoID]
		if !ok {
			continue
		}
		page.Items = append(page.Items, app.Favorite{Photo: photo, FaveDate: candidate.FaveDate.Time})
		visibleCandidates = append(visibleCandidates, candidate)
	}
	if len(page.Items) > limit {
		last := visibleCandidates[limit-1]
		page.Items = page.Items[:limit]
		page.NextCursor = &app.FavoriteCursor{FaveDate: last.FaveDate.Time, PhotoID: last.PhotoID, FlickrAccountID: last.FlickrAccountID}
	} else if more {
		last := scanned[len(scanned)-1]
		page.NextCursor = &app.FavoriteCursor{FaveDate: last.FaveDate.Time, PhotoID: last.PhotoID, FlickrAccountID: last.FlickrAccountID}
	}
	return page, nil
}

func favoritePhotoIDs(candidates []favoriteCandidate) []int64 {
	photoIDs := make([]int64, 0, len(candidates))
	seenPhotoIDs := make(map[int64]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate.RemovedAt.Valid {
			continue
		}
		if _, ok := seenPhotoIDs[candidate.PhotoID]; ok {
			continue
		}
		seenPhotoIDs[candidate.PhotoID] = struct{}{}
		photoIDs = append(photoIDs, candidate.PhotoID)
	}
	return photoIDs
}

type favoriteCandidate struct {
	FlickrAccountID int64
	PhotoID         int64
	FaveDate        pgtype.Timestamptz
	RemovedAt       pgtype.Timestamptz
}

func (r *Repository) visibleFavoritePhotos(ctx context.Context, predicate sharingapp.SQLPredicate, photoIDs []int64) (map[int64]app.Photo, error) {
	if len(photoIDs) == 0 {
		return map[int64]app.Photo{}, nil
	}
	query := `SELECT ` + photoReadColumns + ` FROM photos
WHERE photos.id=ANY($2) AND photos.tombstoned_at IS NULL AND ` + predicate.Expression
	args := append(append([]any(nil), predicate.Args...), photoIDs)
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("catalog: load favorite photos: %w", err)
	}
	defer rows.Close()
	photos := make(map[int64]app.Photo, len(photoIDs))
	for rows.Next() {
		photo, err := scanPhoto(rows)
		if err != nil {
			return nil, fmt.Errorf("catalog: scan favorite photo: %w", err)
		}
		photos[photo.ID] = photo
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: load favorite photos: %w", err)
	}
	return photos, nil
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

// ListContactAlbums reads contact album metadata backed by a photo visible to the user.
func (r *Repository) ListContactAlbums(ctx context.Context, userID int64, ownerNSID string, predicate sharingapp.SQLPredicate, cursor *app.AlbumCursor, limit int) (app.ContactAlbumsPage, error) {
	if err := validateRead(ctx, r, userID, predicate, limit); err != nil {
		return app.ContactAlbumsPage{}, err
	}
	if len(ownerNSID) > 255 || strings.TrimSpace(ownerNSID) != ownerNSID {
		return app.ContactAlbumsPage{}, app.ErrInvalidPage
	}
	if cursor != nil && (cursor.ID == "" || cursor.OwnerNSID != ownerNSID || cursor.ProbePosition < 0 ||
		cursor.ProbePhotoID < 0 || (cursor.ProbePhotoID == 0 && cursor.ProbePosition != 0)) {
		return app.ContactAlbumsPage{}, app.ErrInvalidCursor
	}
	owners, err := r.listContactAlbumOwners(ctx, userID, ownerNSID)
	if err != nil {
		return app.ContactAlbumsPage{}, err
	}
	args := append([]any(nil), predicate.Args...)
	args = append(args, ownerNSID)
	args = append(args, "", (*time.Time)(nil), 0, int64(0), limit+1, contactAlbumPhotoProbeLimit+1)
	query := `WITH contact_owners AS MATERIALIZED (
SELECT DISTINCT contacts.contact_nsid
FROM flickr_accounts
JOIN contacts ON contacts.person_nsid=flickr_accounts.nsid
WHERE flickr_accounts.user_id=$1 AND flickr_accounts.status<>'revoked'
AND ($2='' OR contacts.contact_nsid=$2)
), candidate_albums AS MATERIALIZED (
SELECT albums.id,albums.owner_nsid,albums.title,albums.updated_at
FROM contact_owners
JOIN LATERAL (
SELECT a.id,a.owner_nsid,a.title,a.updated_at
FROM albums a
WHERE a.owner_nsid=contact_owners.contact_nsid
AND ($3='' OR ($6>0 AND a.id=$3)
OR ($4::timestamptz IS NOT NULL AND (a.updated_at<$4::timestamptz OR a.updated_at IS NULL OR (a.updated_at=$4::timestamptz AND a.id<$3)))
OR ($4::timestamptz IS NULL AND a.updated_at IS NULL AND a.id<$3))
ORDER BY a.updated_at DESC NULLS LAST,a.id DESC LIMIT $7
) AS albums ON true
ORDER BY albums.updated_at DESC NULLS LAST,albums.id DESC LIMIT $7
), photo_candidates AS MATERIALIZED (
SELECT candidates.id AS album_id,members.position,members.photo_id,
row_number() OVER (PARTITION BY candidates.id ORDER BY members.position,members.photo_id) AS probe_number
FROM candidate_albums candidates
CROSS JOIN LATERAL (
SELECT album_photos.position,album_photos.photo_id
FROM album_photos
WHERE album_photos.album_id=candidates.id
AND ($6=0 OR candidates.id<>$3 OR (album_photos.position,album_photos.photo_id)>($5,$6))
ORDER BY album_photos.position,album_photos.photo_id LIMIT $8
) AS members
), photo_visibility AS (
SELECT photo_candidates.album_id,
bool_or(photos.tombstoned_at IS NULL AND ` + predicate.Expression + `) AS visible
FROM photo_candidates
JOIN photos ON photos.id=photo_candidates.photo_id
WHERE photo_candidates.probe_number<=` + fmt.Sprint(contactAlbumPhotoProbeLimit) + `
GROUP BY photo_candidates.album_id
), photo_probe_state AS (
SELECT album_id,bool_or(probe_number>` + fmt.Sprint(contactAlbumPhotoProbeLimit) + `) AS has_more,
(array_agg(position ORDER BY probe_number DESC) FILTER (WHERE probe_number<=` + fmt.Sprint(contactAlbumPhotoProbeLimit) + `))[1] AS last_position,
(array_agg(photo_id ORDER BY probe_number DESC) FILTER (WHERE probe_number<=` + fmt.Sprint(contactAlbumPhotoProbeLimit) + `))[1] AS last_photo_id
FROM photo_candidates GROUP BY album_id
)
SELECT candidates.id,candidates.owner_nsid,candidates.title,candidates.updated_at,
COALESCE(photo_visibility.visible,false),
COALESCE(photo_probe_state.has_more,false),
photo_probe_state.last_position,photo_probe_state.last_photo_id
FROM candidate_albums candidates
LEFT JOIN photo_visibility ON photo_visibility.album_id=candidates.id
LEFT JOIN photo_probe_state ON photo_probe_state.album_id=candidates.id
ORDER BY candidates.updated_at DESC NULLS LAST,candidates.id DESC`
	if cursor != nil {
		args[2] = cursor.ID
		args[3] = cursor.UpdatedAt
		args[4] = cursor.ProbePosition
		args[5] = cursor.ProbePhotoID
	}
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return app.ContactAlbumsPage{}, fmt.Errorf("catalog: list contact albums: %w", err)
	}
	defer rows.Close()
	page := app.ContactAlbumsPage{Items: make([]app.ContactAlbum, 0, limit), ContactOwners: owners}
	var lastCompleted *app.AlbumCursor
	candidateCount := 0
	for rows.Next() {
		candidateCount++
		var album app.ContactAlbum
		var updated pgtype.Timestamptz
		var visible, hasMore bool
		var probePosition pgtype.Int4
		var probePhotoID pgtype.Int8
		if err := rows.Scan(&album.ID, &album.OwnerNSID, &album.Title, &updated, &visible, &hasMore, &probePosition, &probePhotoID); err != nil {
			return app.ContactAlbumsPage{}, fmt.Errorf("catalog: scan contact album: %w", err)
		}
		if updated.Valid {
			album.UpdatedAt = &updated.Time
		}
		anchor := &app.AlbumCursor{ID: album.ID, UpdatedAt: album.UpdatedAt, OwnerNSID: ownerNSID}
		if len(page.Items) == limit {
			page.NextCursor = lastCompleted
			break
		}
		if visible {
			page.Items = append(page.Items, album)
			lastCompleted = anchor
			continue
		}
		if hasMore {
			if !probePosition.Valid || !probePhotoID.Valid {
				return app.ContactAlbumsPage{}, errors.New("catalog: contact album photo probe is missing its continuation key")
			}
			anchor.ProbePosition = int(probePosition.Int32)
			anchor.ProbePhotoID = probePhotoID.Int64
			page.NextCursor = anchor
			break
		}
		lastCompleted = anchor
	}
	if err := rows.Err(); err != nil {
		return app.ContactAlbumsPage{}, fmt.Errorf("catalog: list contact albums: %w", err)
	}
	if page.NextCursor == nil && lastCompleted != nil && candidateCount == limit+1 {
		// The candidate window is deliberately bounded; a full window can continue.
		page.NextCursor = lastCompleted
	}
	return page, nil
}

// listContactAlbumOwners returns the user's own contacts for the album owner filter: the first 20000 in
// NSID order (Flickr allows at most 5000 contacts per account, so this covers four accounts in full) plus the contact that is currently selected, if it is one of the user's contacts (a point
// lookup on the contacts primary key). The selected contact is therefore never missing from the list,
// whatever its position, so a deep link from the Contacts screen is not reset to "All". The list is
// deliberately NOT derived from albums: whether a contact has albums at all would otherwise be visible
// even when every photo of those albums is hidden by the sharing policy.
func (r *Repository) listContactAlbumOwners(ctx context.Context, userID int64, selected string) ([]string, error) {
	rows, err := r.db.Query(ctx, `WITH my_accounts AS MATERIALIZED (
SELECT nsid FROM flickr_accounts WHERE user_id=$1 AND status<>'revoked'
), first_contacts AS (
SELECT DISTINCT contacts.contact_nsid
FROM my_accounts
JOIN contacts ON contacts.person_nsid=my_accounts.nsid
ORDER BY contacts.contact_nsid
LIMIT 20000
), selected_contact AS (
SELECT contacts.contact_nsid
FROM my_accounts
JOIN contacts ON contacts.person_nsid=my_accounts.nsid AND contacts.contact_nsid=$2
WHERE $2<>''
LIMIT 1
)
SELECT contact_nsid FROM first_contacts
UNION
SELECT contact_nsid FROM selected_contact
ORDER BY contact_nsid`, userID, selected)
	if err != nil {
		return nil, fmt.Errorf("catalog: list contact album owners: %w", err)
	}
	defer rows.Close()
	owners := make([]string, 0)
	for rows.Next() {
		var owner string
		if err := rows.Scan(&owner); err != nil {
			return nil, fmt.Errorf("catalog: scan contact album owner: %w", err)
		}
		owners = append(owners, owner)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: list contact album owners: %w", err)
	}
	return owners, nil
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

// ListContacts returns distinct contacts of the caller's connected Flickr
// accounts, ordered by contact NSID.
func (r *Repository) ListContacts(ctx context.Context, userID int64, cursor *app.ContactCursor, limit int) (app.ContactsPage, error) {
	if ctx == nil || r == nil || r.db == nil || userID <= 0 || limit < 1 || limit > app.MaxPageSize {
		return app.ContactsPage{}, errors.New("catalog: invalid read request")
	}
	cursorNSID := ""
	if cursor != nil {
		if cursor.NSID == "" {
			return app.ContactsPage{}, app.ErrInvalidCursor
		}
		cursorNSID = cursor.NSID
	}
	query := `WITH contact_page AS MATERIALIZED (
	SELECT DISTINCT c.contact_nsid
	FROM contacts c
	WHERE c.contact_nsid > $2 AND EXISTS (
		SELECT 1 FROM flickr_accounts fa
		WHERE fa.user_id=$1 AND fa.status='ok' AND fa.nsid=c.person_nsid
	)
	ORDER BY c.contact_nsid ASC
	LIMIT $3
)
SELECT p.nsid, p.username, p.realname, p.icon, p.photos_count,
	(SELECT c.person_nsid FROM contacts c
	 WHERE c.contact_nsid=contact_page.contact_nsid AND EXISTS (
		SELECT 1 FROM flickr_accounts fa
		WHERE fa.user_id=$1 AND fa.status='ok' AND fa.nsid=c.person_nsid
	 )
	 ORDER BY c.person_nsid ASC LIMIT 1)
FROM contact_page
JOIN people p ON p.nsid=contact_page.contact_nsid
ORDER BY contact_page.contact_nsid ASC`
	args := []any{userID, cursorNSID, limit + 1}
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return app.ContactsPage{}, fmt.Errorf("catalog: list contacts: %w", err)
	}
	defer rows.Close()
	page := app.ContactsPage{Items: make([]app.Contact, 0, limit)}
	for rows.Next() {
		var item app.Contact
		if err := rows.Scan(&item.NSID, &item.Username, &item.Realname, &item.Icon, &item.PhotosCount, &item.AccountNSID); err != nil {
			return app.ContactsPage{}, fmt.Errorf("catalog: scan contact: %w", err)
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return app.ContactsPage{}, fmt.Errorf("catalog: list contacts: %w", err)
	}
	if len(page.Items) > limit {
		last := page.Items[limit-1]
		page.Items = page.Items[:limit]
		page.NextCursor = &app.ContactCursor{NSID: last.NSID}
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
