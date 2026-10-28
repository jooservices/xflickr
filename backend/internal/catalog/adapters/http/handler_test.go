package http

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/catalog/app"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	sharingapp "github.com/jooservices/xflickr/internal/sharing/app"
)

type readRepoStub struct {
	person      app.Person
	personOK    bool
	people      app.PeoplePage
	photos      app.PhotosPage
	favorites   app.FavoritesPage
	albums      app.AlbumsPage
	albumPhotos app.AlbumPhotosPage
	photo       app.Photo
	photoOK     bool
	listErr     error
}

func (s *readRepoStub) ListPeople(context.Context, int64, sharingapp.SQLPredicate, *app.PersonCursor, int) (app.PeoplePage, error) {
	return s.people, s.listErr
}

func (s *readRepoStub) GetPerson(context.Context, int64, string, sharingapp.SQLPredicate) (app.Person, bool, error) {
	return s.person, s.personOK, s.listErr
}

func (s *readRepoStub) ListPersonPhotos(context.Context, int64, string, sharingapp.SQLPredicate, *app.PhotoCursor, int) (app.PhotosPage, error) {
	return s.photos, s.listErr
}

func (s *readRepoStub) ListVisibleFavorites(context.Context, int64, sharingapp.SQLPredicate, *app.FavoriteCursor, int) (app.FavoritesPage, error) {
	return s.favorites, s.listErr
}

func (s *readRepoStub) ListVisibleAlbums(context.Context, int64, sharingapp.SQLPredicate, *app.AlbumCursor, int) (app.AlbumsPage, error) {
	return s.albums, s.listErr
}

func (s *readRepoStub) ListVisibleAlbumPhotos(context.Context, int64, string, sharingapp.SQLPredicate, *app.AlbumPhotoCursor, int) (app.AlbumPhotosPage, error) {
	return s.albumPhotos, s.listErr
}

func (s *readRepoStub) GetPhoto(context.Context, int64, int64, sharingapp.SQLPredicate) (app.Photo, bool, error) {
	return s.photo, s.photoOK, s.listErr
}

func TestHandlerMapsAllCatalogReadResponses(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	photo := app.Photo{ID: 22, OwnerNSID: "owner", Title: "title", Description: "description", Labels: []string{"travel"}, Visibility: "public", SizesStatus: "confirmed", Sizes: []app.Size{{Label: "Large", Width: 1600, Height: 1200, URL: "https://example.invalid/large.jpg"}, {Label: "Original"}}, AIStatus: "ready"}
	person := app.Person{NSID: "owner", Username: "owner-name", Realname: "Owner", Icon: "icon", PhotosCount: 1}
	repo := &readRepoStub{
		person: person, personOK: true,
		people:      app.PeoplePage{Items: []app.Person{person}, NextCursor: &app.PersonCursor{NSID: "owner"}},
		photos:      app.PhotosPage{Items: []app.Photo{photo}, NextCursor: &app.PhotoCursor{LastUpdate: now, ID: photo.ID}},
		favorites:   app.FavoritesPage{Items: []app.Favorite{{Photo: photo, FaveDate: now}}},
		albums:      app.AlbumsPage{Items: []app.Album{{ID: "album", OwnerNSID: "owner", Title: "Album", VisiblePhotosCount: 1}}},
		albumPhotos: app.AlbumPhotosPage{Items: []app.Photo{photo}, NextCursor: &app.AlbumPhotoCursor{Position: 0, PhotoID: photo.ID}},
		photo:       photo, photoOK: true,
	}
	handler := Handler{Reads: app.ReadService{Repository: repo}, ActorID: func(context.Context) (int64, bool) { return 71, true }}
	ctx := context.Background()

	peopleResponse, err := handler.ListPeople(ctx, gen.ListPeopleRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	people := peopleResponse.(gen.ListPeople200JSONResponse)
	if len(people.Items) != 1 || people.Items[0].Nsid != "owner" || people.NextCursor == nil {
		t.Fatalf("people response=%+v", people)
	}

	personResponse, err := handler.GetPerson(ctx, gen.GetPersonRequestObject{Nsid: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if got := personResponse.(gen.GetPerson200JSONResponse); got.Nsid != "owner" || got.PhotosCount != 1 {
		t.Fatalf("person response=%+v", got)
	}

	personPhotosResponse, err := handler.ListPersonPhotos(ctx, gen.ListPersonPhotosRequestObject{Nsid: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	personPhotos := personPhotosResponse.(gen.ListPersonPhotos200JSONResponse)
	if len(personPhotos.Items) != 1 || personPhotos.Items[0].AiStatus != gen.CatalogPhotoAiStatus("ready") || personPhotos.Items[0].SizesStatus != gen.CatalogPhotoSizesStatus("confirmed") || personPhotos.Items[0].Sizes[0].Label != "Large" || personPhotos.Items[0].Sizes[0].Width == nil || *personPhotos.Items[0].Sizes[0].Width != 1600 || personPhotos.Items[0].Sizes[1].Width != nil || personPhotos.NextCursor == nil {
		t.Fatalf("person photos=%+v", personPhotos)
	}

	favoritesResponse, err := handler.ListFavorites(ctx, gen.ListFavoritesRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	favorites := favoritesResponse.(gen.ListFavorites200JSONResponse)
	if len(favorites.Items) != 1 || favorites.Items[0].FavoriteAt != now || favorites.Items[0].Photo.ID != photo.ID {
		t.Fatalf("favorites=%+v", favorites)
	}

	albumsResponse, err := handler.ListAlbums(ctx, gen.ListAlbumsRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	albums := albumsResponse.(gen.ListAlbums200JSONResponse)
	if len(albums.Items) != 1 || albums.Items[0].ID != "album" || albums.Items[0].PhotosCount != 1 {
		t.Fatalf("albums=%+v", albums)
	}

	albumPhotosResponse, err := handler.ListAlbumPhotos(ctx, gen.ListAlbumPhotosRequestObject{ID: "album"})
	if err != nil {
		t.Fatal(err)
	}
	albumPhotos := albumPhotosResponse.(gen.ListAlbumPhotos200JSONResponse)
	if len(albumPhotos.Items) != 1 || albumPhotos.Items[0].ID != photo.ID || albumPhotos.NextCursor == nil {
		t.Fatalf("album photos=%+v", albumPhotos)
	}

	photoResponse, err := handler.GetPhoto(ctx, gen.GetPhotoRequestObject{ID: photo.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got := photoResponse.(gen.GetPhoto200JSONResponse); got.ID != photo.ID || got.Labels[0] != "travel" {
		t.Fatalf("photo response=%+v", got)
	}
}

func TestHandlerHidesMissingRecordsAndRejectsBadPages(t *testing.T) {
	repo := &readRepoStub{}
	handler := Handler{Reads: app.ReadService{Repository: repo}, ActorID: func(context.Context) (int64, bool) { return 71, true }}
	if response, err := handler.GetPerson(context.Background(), gen.GetPersonRequestObject{Nsid: "secret"}); err != nil || response.(gen.GetPerson404JSONResponse).Code != "not_found" {
		t.Fatalf("missing person response=%#v err=%v", response, err)
	}
	if response, err := handler.GetPhoto(context.Background(), gen.GetPhotoRequestObject{ID: 999}); err != nil || response.(gen.GetPhoto404JSONResponse).Code != "not_found" {
		t.Fatalf("missing photo response=%#v err=%v", response, err)
	}
	repo.listErr = app.ErrNotFound
	if response, err := handler.ListAlbumPhotos(context.Background(), gen.ListAlbumPhotosRequestObject{ID: "secret"}); err != nil || response.(gen.ListAlbumPhotos404JSONResponse).Code != "not_found" {
		t.Fatalf("missing album response=%#v err=%v", response, err)
	}
	repo.listErr = nil
	badCursor := "not-a-cursor"
	if response, err := handler.ListPeople(context.Background(), gen.ListPeopleRequestObject{Params: gen.ListPeopleParams{Cursor: &badCursor}}); err != nil || response.(gen.ListPeople400JSONResponse).Code != "invalid_page" {
		t.Fatalf("bad page response=%#v err=%v", response, err)
	}
	if response, err := handler.ListPersonPhotos(context.Background(), gen.ListPersonPhotosRequestObject{Nsid: "owner", Params: gen.ListPersonPhotosParams{Cursor: &badCursor}}); err != nil || response.(gen.ListPersonPhotos400JSONResponse).Code != "invalid_page" {
		t.Fatalf("bad person photo page response=%#v err=%v", response, err)
	}
	if response, err := handler.ListFavorites(context.Background(), gen.ListFavoritesRequestObject{Params: gen.ListFavoritesParams{Cursor: &badCursor}}); err != nil || response.(gen.ListFavorites400JSONResponse).Code != "invalid_page" {
		t.Fatalf("bad favorite page response=%#v err=%v", response, err)
	}
	if response, err := handler.ListAlbums(context.Background(), gen.ListAlbumsRequestObject{Params: gen.ListAlbumsParams{Cursor: &badCursor}}); err != nil || response.(gen.ListAlbums400JSONResponse).Code != "invalid_page" {
		t.Fatalf("bad album page response=%#v err=%v", response, err)
	}
	if response, err := handler.ListAlbumPhotos(context.Background(), gen.ListAlbumPhotosRequestObject{ID: "album", Params: gen.ListAlbumPhotosParams{Cursor: &badCursor}}); err != nil || response.(gen.ListAlbumPhotos400JSONResponse).Code != "invalid_page" {
		t.Fatalf("bad album photo page response=%#v err=%v", response, err)
	}
	unauthenticated := Handler{Reads: app.ReadService{Repository: repo}}
	if response, err := unauthenticated.ListPeople(context.Background(), gen.ListPeopleRequestObject{}); err != nil || response.(gen.ListPeople401JSONResponse).Code != "unauthenticated" {
		t.Fatalf("people auth response=%#v err=%v", response, err)
	}
	if response, err := unauthenticated.GetPerson(context.Background(), gen.GetPersonRequestObject{Nsid: "owner"}); err != nil || response.(gen.GetPerson401JSONResponse).Code != "unauthenticated" {
		t.Fatalf("person auth response=%#v err=%v", response, err)
	}
	if response, err := unauthenticated.ListPersonPhotos(context.Background(), gen.ListPersonPhotosRequestObject{Nsid: "owner"}); err != nil || response.(gen.ListPersonPhotos401JSONResponse).Code != "unauthenticated" {
		t.Fatalf("person photos auth response=%#v err=%v", response, err)
	}
	if response, err := unauthenticated.ListFavorites(context.Background(), gen.ListFavoritesRequestObject{}); err != nil || response.(gen.ListFavorites401JSONResponse).Code != "unauthenticated" {
		t.Fatalf("favorites auth response=%#v err=%v", response, err)
	}
	if response, err := unauthenticated.ListAlbums(context.Background(), gen.ListAlbumsRequestObject{}); err != nil || response.(gen.ListAlbums401JSONResponse).Code != "unauthenticated" {
		t.Fatalf("albums auth response=%#v err=%v", response, err)
	}
	if response, err := unauthenticated.ListAlbumPhotos(context.Background(), gen.ListAlbumPhotosRequestObject{ID: "album"}); err != nil || response.(gen.ListAlbumPhotos401JSONResponse).Code != "unauthenticated" {
		t.Fatalf("album photos auth response=%#v err=%v", response, err)
	}
	if response, err := unauthenticated.GetPhoto(context.Background(), gen.GetPhotoRequestObject{ID: 1}); err != nil || response.(gen.GetPhoto401JSONResponse).Code != "unauthenticated" {
		t.Fatalf("auth response=%#v err=%v", response, err)
	}

	repo.listErr = errors.New("database failed")
	if _, err := handler.ListFavorites(context.Background(), gen.ListFavoritesRequestObject{}); !errors.Is(err, repo.listErr) {
		t.Fatalf("repository error=%v", err)
	}
	if _, err := handler.ListPeople(context.Background(), gen.ListPeopleRequestObject{}); !errors.Is(err, repo.listErr) {
		t.Fatalf("people repository error=%v", err)
	}
	if _, err := handler.GetPerson(context.Background(), gen.GetPersonRequestObject{Nsid: "owner"}); !errors.Is(err, repo.listErr) {
		t.Fatalf("person repository error=%v", err)
	}
	if _, err := handler.ListPersonPhotos(context.Background(), gen.ListPersonPhotosRequestObject{Nsid: "owner"}); !errors.Is(err, repo.listErr) {
		t.Fatalf("person photo repository error=%v", err)
	}
	if _, err := handler.ListAlbums(context.Background(), gen.ListAlbumsRequestObject{}); !errors.Is(err, repo.listErr) {
		t.Fatalf("album repository error=%v", err)
	}
	if _, err := handler.ListAlbumPhotos(context.Background(), gen.ListAlbumPhotosRequestObject{ID: "album"}); !errors.Is(err, repo.listErr) {
		t.Fatalf("album photo repository error=%v", err)
	}
	if _, err := handler.GetPhoto(context.Background(), gen.GetPhotoRequestObject{ID: 1}); !errors.Is(err, repo.listErr) {
		t.Fatalf("photo repository error=%v", err)
	}
}
