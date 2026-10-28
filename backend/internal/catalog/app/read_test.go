package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	sharingapp "github.com/jooservices/xflickr/internal/sharing/app"
)

type readRepositoryStub struct {
	people        PeoplePage
	person        Person
	personOK      bool
	photos        PhotosPage
	favorites     FavoritesPage
	albums        AlbumsPage
	albumPhotos   AlbumPhotosPage
	photo         Photo
	photoOK       bool
	err           error
	seenUser      int64
	seenLimit     int
	seenPredicate sharingapp.SQLPredicate
}

func (s *readRepositoryStub) record(userID int64, predicate sharingapp.SQLPredicate, limit int) error {
	s.seenUser, s.seenPredicate, s.seenLimit = userID, predicate, limit
	return s.err
}

func (s *readRepositoryStub) ListPeople(_ context.Context, userID int64, predicate sharingapp.SQLPredicate, _ *PersonCursor, limit int) (PeoplePage, error) {
	return s.people, s.record(userID, predicate, limit)
}

func (s *readRepositoryStub) GetPerson(_ context.Context, userID int64, _ string, predicate sharingapp.SQLPredicate) (Person, bool, error) {
	return s.person, s.personOK, s.record(userID, predicate, DefaultPageSize)
}

func (s *readRepositoryStub) ListPersonPhotos(_ context.Context, userID int64, _ string, predicate sharingapp.SQLPredicate, _ *PhotoCursor, limit int) (PhotosPage, error) {
	return s.photos, s.record(userID, predicate, limit)
}

func (s *readRepositoryStub) ListVisibleFavorites(_ context.Context, userID int64, predicate sharingapp.SQLPredicate, _ *FavoriteCursor, limit int) (FavoritesPage, error) {
	return s.favorites, s.record(userID, predicate, limit)
}

func (s *readRepositoryStub) ListVisibleAlbums(_ context.Context, userID int64, predicate sharingapp.SQLPredicate, _ *AlbumCursor, limit int) (AlbumsPage, error) {
	return s.albums, s.record(userID, predicate, limit)
}

func (s *readRepositoryStub) ListVisibleAlbumPhotos(_ context.Context, userID int64, _ string, predicate sharingapp.SQLPredicate, _ *AlbumPhotoCursor, limit int) (AlbumPhotosPage, error) {
	return s.albumPhotos, s.record(userID, predicate, limit)
}

func (s *readRepositoryStub) GetPhoto(_ context.Context, userID, _ int64, predicate sharingapp.SQLPredicate) (Photo, bool, error) {
	return s.photo, s.photoOK, s.record(userID, predicate, DefaultPageSize)
}

func TestReadServiceAppliesPolicyAndEncodesKeysets(t *testing.T) {
	now := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	repo := &readRepositoryStub{people: PeoplePage{Items: []Person{{NSID: "person"}}, NextCursor: &PersonCursor{NSID: "person"}}}
	service := ReadService{Repository: repo}
	page, cursor, err := service.ListPeople(context.Background(), 42, "", DefaultPageSize)
	if err != nil || len(page.Items) != 1 || cursor == "" {
		t.Fatalf("page=%+v cursor=%q err=%v", page, cursor, err)
	}
	if repo.seenUser != 42 || repo.seenLimit != DefaultPageSize || len(repo.seenPredicate.Args) != 1 || repo.seenPredicate.Args[0] != int64(42) || repo.seenPredicate.Expression == "" {
		t.Fatalf("repository scope=%+v", repo)
	}
	decoded, err := decodeCursor[PersonCursor](cursor, "people")
	if err != nil || decoded.NSID != "person" {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}

	repo.photos = PhotosPage{Items: []Photo{{ID: 9}}, NextCursor: &PhotoCursor{LastUpdate: now, ID: 9}}
	photoPage, photoCursor, err := service.ListPersonPhotos(context.Background(), 42, "person", "", 100)
	if err != nil || len(photoPage.Items) != 1 || photoCursor == "" || repo.seenLimit != 100 {
		t.Fatalf("photoPage=%+v cursor=%q err=%v", photoPage, photoCursor, err)
	}

	repo.albums = AlbumsPage{NextCursor: &AlbumCursor{ID: "album"}}
	_, albumCursor, err := service.ListAlbums(context.Background(), 42, "", 10)
	if err != nil || albumCursor == "" {
		t.Fatalf("album cursor=%q err=%v", albumCursor, err)
	}
	repo.albumPhotos = AlbumPhotosPage{NextCursor: &AlbumPhotoCursor{Position: 3, PhotoID: 9}}
	_, albumPhotoCursor, err := service.ListAlbumPhotos(context.Background(), 42, "album", "", 10)
	if err != nil || albumPhotoCursor == "" {
		t.Fatalf("album photo cursor=%q err=%v", albumPhotoCursor, err)
	}
	repo.person = Person{NSID: "person"}
	repo.personOK = true
	if got, err := service.GetPerson(context.Background(), 42, "person"); err != nil || got.NSID != repo.person.NSID {
		t.Fatalf("get person=%+v err=%v", got, err)
	}
	repo.favorites = FavoritesPage{Items: []Favorite{{Photo: Photo{ID: 9}, FaveDate: now}}, NextCursor: &FavoriteCursor{FaveDate: now, PhotoID: 9}}
	_, favoriteCursor, err := service.ListFavorites(context.Background(), 42, "", 10)
	if err != nil || favoriteCursor == "" {
		t.Fatalf("favorite cursor=%q err=%v", favoriteCursor, err)
	}
	repo.photoOK = true
	repo.photo = Photo{ID: 9}
	if got, err := service.GetPhoto(context.Background(), 42, 9); err != nil || got.ID != 9 {
		t.Fatalf("get photo=%+v err=%v", got, err)
	}
}

func TestReadServiceRejectsInvalidPageAndCursorWithoutReading(t *testing.T) {
	repo := &readRepositoryStub{}
	service := ReadService{Repository: repo}
	if _, _, err := service.ListPeople(context.Background(), 1, "not-base64", 10); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("invalid cursor error=%v", err)
	}
	if _, _, err := service.ListPeople(context.Background(), 1, "", MaxPageSize+1); !errors.Is(err, ErrInvalidPage) {
		t.Fatalf("invalid limit error=%v", err)
	}
	if _, _, err := service.ListPeople(context.Background(), 1, "", 0); !errors.Is(err, ErrInvalidPage) {
		t.Fatalf("zero limit error=%v", err)
	}
	if _, _, err := service.ListPersonPhotos(context.Background(), 1, " \t ", "", 10); !errors.Is(err, ErrNotFound) {
		t.Fatalf("blank NSID error=%v", err)
	}
	if repo.seenUser != 0 {
		t.Fatalf("invalid request reached repository: %+v", repo)
	}
	if _, err := (ReadService{}).GetPerson(context.Background(), 1, "person"); !errors.Is(err, ErrReadUnavailable) {
		t.Fatalf("missing repository error=%v", err)
	}
	if _, _, err := service.ListPeople(nil, 1, "", 10); !errors.Is(err, ErrReadUnavailable) {
		t.Fatalf("nil context error=%v", err)
	}
	if _, err := service.GetPhoto(context.Background(), 0, 1); !errors.Is(err, ErrReadUnavailable) {
		t.Fatalf("invalid actor error=%v", err)
	}
}

func TestCursorEnvelopeRejectsWrongKindMalformedValuesAndOversize(t *testing.T) {
	validOtherKind, _ := json.Marshal(cursorEnvelope[PersonCursor]{Version: 1, Kind: "albums", Value: PersonCursor{NSID: "p"}})
	wrongKind := base64.RawURLEncoding.EncodeToString(validOtherKind)
	if _, err := decodeCursor[PersonCursor](wrongKind, "people"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("wrong kind error=%v", err)
	}
	invalidPerson, _ := json.Marshal(cursorEnvelope[PersonCursor]{Version: 1, Kind: "people"})
	if _, err := decodeCursor[PersonCursor](base64.RawURLEncoding.EncodeToString(invalidPerson), "people"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatal("empty cursor value accepted")
	}
	if _, err := decodeCursor[PhotoCursor](base64.RawURLEncoding.EncodeToString([]byte("{")), "person-photos"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("malformed cursor error=%v", err)
	}
	if _, err := decodeCursor[PersonCursor](base64.RawURLEncoding.EncodeToString(make([]byte, 1025)), "people"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("large cursor error=%v", err)
	}
	if _, err := decodeCursor[PersonCursor]("***", "people"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("bad base64 error=%v", err)
	}
	invalidFavorite, _ := json.Marshal(cursorEnvelope[FavoriteCursor]{Version: 1, Kind: "favorites", Value: FavoriteCursor{PhotoID: 1}})
	if _, err := decodeCursor[FavoriteCursor](base64.RawURLEncoding.EncodeToString(invalidFavorite), "favorites"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("invalid favorite value error=%v", err)
	}
	invalidAlbum, _ := json.Marshal(cursorEnvelope[AlbumCursor]{Version: 1, Kind: "albums", Value: AlbumCursor{}})
	if _, err := decodeCursor[AlbumCursor](base64.RawURLEncoding.EncodeToString(invalidAlbum), "albums"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("invalid album value error=%v", err)
	}
	invalidAlbumPhoto, _ := json.Marshal(cursorEnvelope[AlbumPhotoCursor]{Version: 1, Kind: "album-photos", Value: AlbumPhotoCursor{Position: -1, PhotoID: 1}})
	if _, err := decodeCursor[AlbumPhotoCursor](base64.RawURLEncoding.EncodeToString(invalidAlbumPhoto), "album-photos"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("invalid album photo value error=%v", err)
	}
}

func TestReadServicePropagatesRepositoryErrorsAndNotFound(t *testing.T) {
	ctx := context.Background()
	repo := &readRepositoryStub{personOK: false, photoOK: false}
	service := ReadService{Repository: repo}
	if _, err := service.GetPerson(ctx, 3, "private"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing person error=%v", err)
	}
	if _, err := service.GetPhoto(ctx, 3, 8); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing photo error=%v", err)
	}
	if _, err := service.GetPerson(ctx, 3, " "); !errors.Is(err, ErrNotFound) {
		t.Fatalf("blank person error=%v", err)
	}
	if _, _, err := service.ListPersonPhotos(ctx, 3, " \t ", "", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("blank person photos NSID error=%v", err)
	}
	if _, err := service.GetPhoto(ctx, 3, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid photo id error=%v", err)
	}
	boom := errors.New("read failed")
	repo.err = boom
	if _, _, err := service.ListPeople(ctx, 3, "", 1); !errors.Is(err, boom) {
		t.Fatalf("list people error=%v", err)
	}
	if _, err := service.GetPerson(ctx, 3, "person"); !errors.Is(err, boom) {
		t.Fatalf("get person error=%v", err)
	}
	if _, _, err := service.ListPersonPhotos(ctx, 3, "person", "", 1); !errors.Is(err, boom) {
		t.Fatalf("person photos error=%v", err)
	}
	if _, _, err := service.ListFavorites(ctx, 3, "", 1); !errors.Is(err, boom) {
		t.Fatalf("favorites error=%v", err)
	}
	if _, _, err := service.ListAlbums(ctx, 3, "", 1); !errors.Is(err, boom) {
		t.Fatalf("albums error=%v", err)
	}
	if _, _, err := service.ListAlbumPhotos(ctx, 3, "album", "", 1); !errors.Is(err, boom) {
		t.Fatalf("album photos error=%v", err)
	}
	if _, err := service.GetPhoto(ctx, 3, 8); !errors.Is(err, boom) {
		t.Fatalf("get photo error=%v", err)
	}
}
