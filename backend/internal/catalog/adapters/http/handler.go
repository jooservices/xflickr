// Package http adapts catalog read operations to the generated HTTP contract.
package http

import (
	"context"
	"errors"

	"github.com/jooservices/xflickr/internal/catalog/app"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

// Handler implements authenticated catalog read endpoints. ActorID is
// populated by the identity middleware from the signed-in session.
type Handler struct {
	Reads   app.ReadService
	ActorID func(context.Context) (int64, bool)
}

func (h Handler) actor(ctx context.Context) (int64, bool) {
	if h.ActorID == nil {
		return 0, false
	}
	return h.ActorID(ctx)
}

func (h Handler) ListPeople(ctx context.Context, request gen.ListPeopleRequestObject) (gen.ListPeopleResponseObject, error) {
	userID, ok := h.actor(ctx)
	if !ok {
		return gen.ListPeople401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	page, next, err := h.Reads.ListPeople(ctx, userID, stringParam(request.Params.Cursor), intParam(request.Params.Limit))
	if err != nil {
		return pageFailure[gen.ListPeopleResponseObject](err, func(e gen.Error) gen.ListPeopleResponseObject { return gen.ListPeople400JSONResponse(e) })
	}
	items := make([]gen.CatalogPerson, 0, len(page.Items))
	for _, person := range page.Items {
		items = append(items, gen.CatalogPerson{Nsid: person.NSID, Username: person.Username, Realname: person.Realname, Icon: person.Icon, PhotosCount: person.PhotosCount})
	}
	return gen.ListPeople200JSONResponse(gen.CatalogPersonPage{Items: items, NextCursor: optionalString(next)}), nil
}

func (h Handler) GetPerson(ctx context.Context, request gen.GetPersonRequestObject) (gen.GetPersonResponseObject, error) {
	userID, ok := h.actor(ctx)
	if !ok {
		return gen.GetPerson401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	person, err := h.Reads.GetPerson(ctx, userID, request.Nsid)
	if errors.Is(err, app.ErrNotFound) {
		return gen.GetPerson404JSONResponse(notFoundError()), nil
	}
	if err != nil {
		return nil, err
	}
	return gen.GetPerson200JSONResponse(gen.CatalogPerson{Nsid: person.NSID, Username: person.Username, Realname: person.Realname, Icon: person.Icon, PhotosCount: person.PhotosCount}), nil
}

func (h Handler) ListPersonPhotos(ctx context.Context, request gen.ListPersonPhotosRequestObject) (gen.ListPersonPhotosResponseObject, error) {
	userID, ok := h.actor(ctx)
	if !ok {
		return gen.ListPersonPhotos401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	page, next, err := h.Reads.ListPersonPhotos(ctx, userID, request.Nsid, stringParam(request.Params.Cursor), intParam(request.Params.Limit))
	if errors.Is(err, app.ErrNotFound) {
		return gen.ListPersonPhotos404JSONResponse(notFoundError()), nil
	}
	if errors.Is(err, app.ErrInvalidCursor) || errors.Is(err, app.ErrInvalidPage) {
		return gen.ListPersonPhotos400JSONResponse(invalidPageError()), nil
	}
	if err != nil {
		return nil, err
	}
	return gen.ListPersonPhotos200JSONResponse(photoPage(page.Items, next)), nil
}

func (h Handler) ListFavorites(ctx context.Context, request gen.ListFavoritesRequestObject) (gen.ListFavoritesResponseObject, error) {
	userID, ok := h.actor(ctx)
	if !ok {
		return gen.ListFavorites401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	page, next, err := h.Reads.ListFavorites(ctx, userID, stringParam(request.Params.Cursor), intParam(request.Params.Limit))
	if errors.Is(err, app.ErrInvalidCursor) || errors.Is(err, app.ErrInvalidPage) {
		return gen.ListFavorites400JSONResponse(invalidPageError()), nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]gen.CatalogFavorite, 0, len(page.Items))
	for _, favorite := range page.Items {
		items = append(items, gen.CatalogFavorite{Photo: photo(favorite.Photo), FavoriteAt: favorite.FaveDate})
	}
	return gen.ListFavorites200JSONResponse(gen.CatalogFavoritePage{Items: items, NextCursor: optionalString(next)}), nil
}

func (h Handler) ListAlbums(ctx context.Context, request gen.ListAlbumsRequestObject) (gen.ListAlbumsResponseObject, error) {
	userID, ok := h.actor(ctx)
	if !ok {
		return gen.ListAlbums401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	page, next, err := h.Reads.ListAlbums(ctx, userID, stringParam(request.Params.Cursor), intParam(request.Params.Limit))
	if errors.Is(err, app.ErrInvalidCursor) || errors.Is(err, app.ErrInvalidPage) {
		return gen.ListAlbums400JSONResponse(invalidPageError()), nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]gen.CatalogAlbum, 0, len(page.Items))
	for _, album := range page.Items {
		items = append(items, gen.CatalogAlbum{ID: album.ID, OwnerNsid: album.OwnerNSID, Title: album.Title, Description: album.Description, PrimaryPhotoID: album.PrimaryPhotoID, PhotosCount: album.VisiblePhotosCount, CreatedAt: album.CreatedAt, UpdatedAt: album.UpdatedAt})
	}
	return gen.ListAlbums200JSONResponse(gen.CatalogAlbumPage{Items: items, NextCursor: optionalString(next)}), nil
}

func (h Handler) ListAlbumPhotos(ctx context.Context, request gen.ListAlbumPhotosRequestObject) (gen.ListAlbumPhotosResponseObject, error) {
	userID, ok := h.actor(ctx)
	if !ok {
		return gen.ListAlbumPhotos401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	page, next, err := h.Reads.ListAlbumPhotos(ctx, userID, request.ID, stringParam(request.Params.Cursor), intParam(request.Params.Limit))
	if errors.Is(err, app.ErrNotFound) {
		return gen.ListAlbumPhotos404JSONResponse(notFoundError()), nil
	}
	if errors.Is(err, app.ErrInvalidCursor) || errors.Is(err, app.ErrInvalidPage) {
		return gen.ListAlbumPhotos400JSONResponse(invalidPageError()), nil
	}
	if err != nil {
		return nil, err
	}
	return gen.ListAlbumPhotos200JSONResponse(photoPage(page.Items, next)), nil
}

func (h Handler) GetPhoto(ctx context.Context, request gen.GetPhotoRequestObject) (gen.GetPhotoResponseObject, error) {
	userID, ok := h.actor(ctx)
	if !ok {
		return gen.GetPhoto401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	item, err := h.Reads.GetPhoto(ctx, userID, request.ID)
	if errors.Is(err, app.ErrNotFound) {
		return gen.GetPhoto404JSONResponse(notFoundError()), nil
	}
	if err != nil {
		return nil, err
	}
	return gen.GetPhoto200JSONResponse(photo(item)), nil
}

func photoPage(values []app.Photo, next string) gen.CatalogPhotoPage {
	items := make([]gen.CatalogPhoto, 0, len(values))
	for _, value := range values {
		items = append(items, photo(value))
	}
	return gen.CatalogPhotoPage{Items: items, NextCursor: optionalString(next)}
}

func photo(value app.Photo) gen.CatalogPhoto {
	sizes := make([]gen.CatalogSize, 0, len(value.Sizes))
	for _, size := range value.Sizes {
		sizes = append(sizes, gen.CatalogSize{Label: size.Label, Width: optionalInt(size.Width), Height: optionalInt(size.Height), Source: optionalString(size.Source), URL: optionalString(size.URL), Media: optionalString(size.Media)})
	}
	return gen.CatalogPhoto{
		ID: value.ID, OwnerNsid: value.OwnerNSID, Title: value.Title, Description: value.Description,
		Labels: value.Labels, TakenAt: value.TakenAt, UploadedAt: value.UploadedAt,
		Visibility: gen.CatalogPhotoVisibility(value.Visibility), SizesStatus: gen.CatalogPhotoSizesStatus(value.SizesStatus),
		Sizes: sizes, AiStatus: gen.CatalogPhotoAiStatus(value.AIStatus),
	}
}

func stringParam(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func intParam(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func optionalInt(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}

func notFoundError() gen.Error {
	return gen.Error{Code: "not_found", Message: "Catalog record is not available"}
}

func invalidPageError() gen.Error {
	return gen.Error{Code: "invalid_page", Message: "The page cursor or limit is invalid"}
}

// pageFailure lets the shared mapper keep list-handler errors consistent while
// retaining each operation's generated response type.
func pageFailure[T any](err error, badRequest func(gen.Error) T) (T, error) {
	var zero T
	if errors.Is(err, app.ErrInvalidCursor) || errors.Is(err, app.ErrInvalidPage) {
		return badRequest(invalidPageError()), nil
	}
	return zero, err
}
