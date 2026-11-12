package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	sharingapp "github.com/jooservices/xflickr/internal/sharing/app"
	sharingdomain "github.com/jooservices/xflickr/internal/sharing/domain"
)

var (
	ErrNotFound        = errors.New("catalog: record not found")
	ErrInvalidPage     = errors.New("catalog: invalid page request")
	ErrInvalidCursor   = errors.New("catalog: invalid cursor")
	ErrReadUnavailable = errors.New("catalog: read repository and context are required")
)

const (
	DefaultPageSize = 50
	MaxPageSize     = 100
)

type PersonCursor struct {
	NSID string `json:"nsid"`
}
type PhotoCursor struct {
	LastUpdate time.Time `json:"last_update"`
	ID         int64     `json:"id"`
}
type FavoriteCursor struct {
	FaveDate        time.Time `json:"fave_date"`
	PhotoID         int64     `json:"photo_id"`
	FlickrAccountID int64     `json:"flickr_account_id"`
}
type AlbumCursor struct {
	UpdatedAt     *time.Time `json:"updated_at"`
	ID            string     `json:"id"`
	OwnerNSID     string     `json:"owner_nsid,omitempty"`
	ProbePosition int        `json:"probe_position,omitempty"`
	ProbePhotoID  int64      `json:"probe_photo_id,omitempty"`
}
type AlbumPhotoCursor struct {
	Position int   `json:"position"`
	PhotoID  int64 `json:"photo_id"`
}

type Person struct {
	NSID        string
	Username    string
	Realname    string
	Icon        string
	PhotosCount int64
}

type Size struct {
	Label  string `json:"label"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Source string `json:"source,omitempty"`
	URL    string `json:"url,omitempty"`
	Media  string `json:"media,omitempty"`
}

type Photo struct {
	ID          int64
	OwnerNSID   string
	Title       string
	Description string
	Labels      []string
	TakenAt     *time.Time
	UploadedAt  *time.Time
	LastUpdate  time.Time
	Visibility  string
	SizesStatus string
	Sizes       []Size
	AIStatus    string
}

type Favorite struct {
	Photo    Photo
	FaveDate time.Time
}

type Album struct {
	ID                 string
	OwnerNSID          string
	Title              string
	Description        string
	PrimaryPhotoID     *int64
	VisiblePhotosCount int64
	CreatedAt          *time.Time
	UpdatedAt          *time.Time
}

// ContactAlbum contains visible metadata for an album owned by one of the user's contacts.
type ContactAlbum struct {
	ID        string
	OwnerNSID string
	Title     string
	UpdatedAt *time.Time
}

type PeoplePage struct {
	Items      []Person
	NextCursor *PersonCursor
}
type PhotosPage struct {
	Items      []Photo
	NextCursor *PhotoCursor
}
type AlbumPhotosPage struct {
	Items      []Photo
	NextCursor *AlbumPhotoCursor
}
type FavoritesPage struct {
	Items      []Favorite
	NextCursor *FavoriteCursor
}
type AlbumsPage struct {
	Items      []Album
	NextCursor *AlbumCursor
}

// ContactAlbumsPage contains one contact album page and its next keyset cursor.
type ContactAlbumsPage struct {
	Items         []ContactAlbum
	ContactOwners []string
	NextCursor    *AlbumCursor
}

// ContactCursor identifies the last contact in a keyset page.
type ContactCursor struct {
	NSID string `json:"nsid"`
}

// Contact is one stored contact relationship visible to a user.
type Contact struct {
	NSID        string
	Username    string
	Realname    string
	Icon        string
	AccountNSID string
}

// ContactsPage contains one user's visible contacts and the next keyset cursor.
type ContactsPage struct {
	Items      []Contact
	NextCursor *ContactCursor
}

// PersonPhotosQuery selects the photos of one person that a viewer may see.
type PersonPhotosQuery struct {
	UserID    int64
	NSID      string
	Predicate sharingapp.SQLPredicate
	Cursor    *PhotoCursor
	Limit     int
}

// ReadRepository performs catalog reads with a predicate from the reusable
// sharing policy. Its SQL predicate is constructed by ReadService, never from
// request input.
type ReadRepository interface {
	ListPeople(context.Context, int64, sharingapp.SQLPredicate, *PersonCursor, int) (PeoplePage, error)
	GetPerson(context.Context, int64, string, sharingapp.SQLPredicate) (Person, bool, error)
	ListPersonPhotos(context.Context, PersonPhotosQuery) (PhotosPage, error)
	ListVisibleFavorites(context.Context, int64, sharingapp.SQLPredicate, *FavoriteCursor, int) (FavoritesPage, error)
	ListVisibleAlbums(context.Context, int64, sharingapp.SQLPredicate, *AlbumCursor, int) (AlbumsPage, error)
	ListContactAlbums(context.Context, int64, string, sharingapp.SQLPredicate, *AlbumCursor, int) (ContactAlbumsPage, error)
	ListVisibleAlbumPhotos(context.Context, int64, string, sharingapp.SQLPredicate, *AlbumPhotoCursor, int) (AlbumPhotosPage, error)
	ListContacts(context.Context, int64, *ContactCursor, int) (ContactsPage, error)
	GetPhoto(context.Context, int64, int64, sharingapp.SQLPredicate) (Photo, bool, error)
}

// ReadService applies the shared visibility policy and validates opaque
// keyset cursors before any repository query runs.
type ReadService struct{ Repository ReadRepository }

func (s ReadService) ListPeople(ctx context.Context, userID int64, cursor string, limit int) (PeoplePage, string, error) {
	if err := s.validate(ctx, userID); err != nil {
		return PeoplePage{}, "", err
	}
	parsed, err := decodeCursor[PersonCursor](cursor, "people")
	if err != nil {
		return PeoplePage{}, "", err
	}
	limit, err = normalizeLimit(limit)
	if err != nil {
		return PeoplePage{}, "", err
	}
	page, err := s.Repository.ListPeople(ctx, userID, visibility(userID), parsed, limit)
	if err != nil {
		return PeoplePage{}, "", err
	}
	next, err := encodeCursor("people", page.NextCursor)
	return page, next, err
}

func (s ReadService) GetPerson(ctx context.Context, userID int64, nsid string) (Person, error) {
	if err := s.validate(ctx, userID); err != nil {
		return Person{}, err
	}
	if strings.TrimSpace(nsid) == "" {
		return Person{}, ErrNotFound
	}
	person, ok, err := s.Repository.GetPerson(ctx, userID, nsid, visibility(userID))
	if err != nil {
		return Person{}, err
	}
	if !ok {
		return Person{}, ErrNotFound
	}
	return person, nil
}

// PersonPhotosRequest is a request for a person's photos. Cursor is the opaque
// page token that ReadService decodes.
type PersonPhotosRequest struct {
	UserID int64
	NSID   string
	Cursor string
	Limit  int
}

func (s ReadService) ListPersonPhotos(ctx context.Context, req PersonPhotosRequest) (PhotosPage, string, error) {
	if err := s.validate(ctx, req.UserID); err != nil {
		return PhotosPage{}, "", err
	}
	if strings.TrimSpace(req.NSID) == "" {
		return PhotosPage{}, "", ErrNotFound
	}
	parsed, err := decodeCursor[PhotoCursor](req.Cursor, "person-photos")
	if err != nil {
		return PhotosPage{}, "", err
	}
	limit, err := normalizeLimit(req.Limit)
	if err != nil {
		return PhotosPage{}, "", err
	}
	page, err := s.Repository.ListPersonPhotos(ctx, PersonPhotosQuery{UserID: req.UserID, NSID: req.NSID, Predicate: visibility(req.UserID), Cursor: parsed, Limit: limit})
	if err != nil {
		return PhotosPage{}, "", err
	}
	next, err := encodeCursor("person-photos", page.NextCursor)
	return page, next, err
}

func (s ReadService) ListFavorites(ctx context.Context, userID int64, cursor string, limit int) (FavoritesPage, string, error) {
	if err := s.validate(ctx, userID); err != nil {
		return FavoritesPage{}, "", err
	}
	parsed, err := decodeCursor[FavoriteCursor](cursor, "favorites")
	if err != nil {
		return FavoritesPage{}, "", err
	}
	limit, err = normalizeLimit(limit)
	if err != nil {
		return FavoritesPage{}, "", err
	}
	page, err := s.Repository.ListVisibleFavorites(ctx, userID, visibility(userID), parsed, limit)
	if err != nil {
		return FavoritesPage{}, "", err
	}
	next, err := encodeCursor("favorites", page.NextCursor)
	return page, next, err
}

func (s ReadService) ListAlbums(ctx context.Context, userID int64, cursor string, limit int) (AlbumsPage, string, error) {
	if err := s.validate(ctx, userID); err != nil {
		return AlbumsPage{}, "", err
	}
	parsed, err := decodeCursor[AlbumCursor](cursor, "albums")
	if err != nil {
		return AlbumsPage{}, "", err
	}
	limit, err = normalizeLimit(limit)
	if err != nil {
		return AlbumsPage{}, "", err
	}
	page, err := s.Repository.ListVisibleAlbums(ctx, userID, visibility(userID), parsed, limit)
	if err != nil {
		return AlbumsPage{}, "", err
	}
	next, err := encodeCursor("albums", page.NextCursor)
	return page, next, err
}

// ListContactAlbums returns contact albums with at least one photo visible to the signed-in user.
func (s ReadService) ListContactAlbums(ctx context.Context, userID int64, ownerNSID, cursor string, limit int) (ContactAlbumsPage, string, error) {
	if err := s.validate(ctx, userID); err != nil {
		return ContactAlbumsPage{}, "", err
	}
	parsed, err := decodeCursor[AlbumCursor](cursor, "my-albums")
	if err != nil {
		return ContactAlbumsPage{}, "", err
	}
	if parsed != nil && parsed.OwnerNSID != ownerNSID {
		return ContactAlbumsPage{}, "", ErrInvalidCursor
	}
	limit, err = normalizeLimit(limit)
	if err != nil {
		return ContactAlbumsPage{}, "", err
	}
	page, err := s.Repository.ListContactAlbums(ctx, userID, ownerNSID, visibility(userID), parsed, limit)
	if err != nil {
		return ContactAlbumsPage{}, "", err
	}
	next, err := encodeCursor("my-albums", page.NextCursor)
	return page, next, err
}

func (s ReadService) ListAlbumPhotos(ctx context.Context, userID int64, albumID, cursor string, limit int) (AlbumPhotosPage, string, error) {
	if err := s.validate(ctx, userID); err != nil {
		return AlbumPhotosPage{}, "", err
	}
	parsed, err := decodeCursor[AlbumPhotoCursor](cursor, "album-photos")
	if err != nil {
		return AlbumPhotosPage{}, "", err
	}
	limit, err = normalizeLimit(limit)
	if err != nil {
		return AlbumPhotosPage{}, "", err
	}
	page, err := s.Repository.ListVisibleAlbumPhotos(ctx, userID, albumID, visibility(userID), parsed, limit)
	if err != nil {
		return AlbumPhotosPage{}, "", err
	}
	next, err := encodeCursor("album-photos", page.NextCursor)
	return page, next, err
}

// ListContacts returns people linked as contacts of the user's connected
// Flickr accounts, ordered by NSID with an opaque keyset cursor.
func (s ReadService) ListContacts(ctx context.Context, userID int64, cursor string, limit int) (ContactsPage, string, error) {
	if err := s.validate(ctx, userID); err != nil {
		return ContactsPage{}, "", err
	}
	parsed, err := decodeCursor[ContactCursor](cursor, "contacts")
	if err != nil {
		return ContactsPage{}, "", err
	}
	limit, err = normalizeLimit(limit)
	if err != nil {
		return ContactsPage{}, "", err
	}
	page, err := s.Repository.ListContacts(ctx, userID, parsed, limit)
	if err != nil {
		return ContactsPage{}, "", err
	}
	next, err := encodeCursor("contacts", page.NextCursor)
	return page, next, err
}

func (s ReadService) GetPhoto(ctx context.Context, userID, photoID int64) (Photo, error) {
	if err := s.validate(ctx, userID); err != nil {
		return Photo{}, err
	}
	if photoID <= 0 {
		return Photo{}, ErrNotFound
	}
	photo, ok, err := s.Repository.GetPhoto(ctx, userID, photoID, visibility(userID))
	if err != nil {
		return Photo{}, err
	}
	if !ok {
		return Photo{}, ErrNotFound
	}
	return photo, nil
}

func (s ReadService) validate(ctx context.Context, userID int64) error {
	if ctx == nil || s.Repository == nil || userID <= 0 {
		return ErrReadUnavailable
	}
	return nil
}

func visibility(userID int64) sharingapp.SQLPredicate {
	return sharingapp.Filter(sharingdomain.User{ID: userID}).SQL
}

func normalizeLimit(limit int) (int, error) {
	if limit < 1 || limit > MaxPageSize {
		return 0, ErrInvalidPage
	}
	return limit, nil
}

type cursorEnvelope[T any] struct {
	Version int    `json:"v"`
	Kind    string `json:"k"`
	Value   T      `json:"c"`
}

func decodeCursor[T any](raw, kind string) (*T, error) {
	if raw == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(b) > 1024 {
		return nil, ErrInvalidCursor
	}
	var envelope cursorEnvelope[T]
	if json.Unmarshal(b, &envelope) != nil || envelope.Version != 1 || envelope.Kind != kind {
		return nil, ErrInvalidCursor
	}
	if err := validateCursor(envelope.Value); err != nil {
		return nil, err
	}
	return &envelope.Value, nil
}

func encodeCursor[T any](kind string, cursor *T) (string, error) {
	if cursor == nil {
		return "", nil
	}
	b, err := json.Marshal(cursorEnvelope[T]{Version: 1, Kind: kind, Value: *cursor})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func validateCursor(cursor any) error {
	valid := false
	switch value := cursor.(type) {
	case PersonCursor:
		valid = value.NSID != ""
	case PhotoCursor:
		valid = !value.LastUpdate.IsZero() && value.ID > 0
	case FavoriteCursor:
		// FlickrAccountID 0 is a cursor issued before accounts were part of the key: it keeps
		// its old meaning (continue strictly after the date/photo pair) so paging that was in
		// flight during the deployment is not interrupted. Negative ids are never valid.
		if value.FaveDate.IsZero() || value.PhotoID <= 0 || value.FlickrAccountID < 0 {
			valid = false
		} else {
			valid = true
		}
	case AlbumCursor:
		valid = value.ID != "" && value.ProbePosition >= 0 && value.ProbePhotoID >= 0 &&
			(value.ProbePhotoID != 0 || value.ProbePosition == 0)
	case AlbumPhotoCursor:
		valid = value.Position >= 0 && value.PhotoID > 0
	case ContactCursor:
		valid = value.NSID != ""
	}
	if !valid {
		return ErrInvalidCursor
	}
	return nil
}
