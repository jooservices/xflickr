package http

import (
	"context"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

func (StrictHandler) ListMeCrawlInterests(context.Context, gen.ListMeCrawlInterestsRequestObject) (gen.ListMeCrawlInterestsResponseObject, error) {
	return nil, errCatalogServedByCatalog
}

func (StrictHandler) GetMeContact(context.Context, gen.GetMeContactRequestObject) (gen.GetMeContactResponseObject, error) {
	return nil, errCatalogServedByCatalog
}

func (StrictHandler) GetMeContactAbout(context.Context, gen.GetMeContactAboutRequestObject) (gen.GetMeContactAboutResponseObject, error) {
	return nil, errCatalogServedByCatalog
}

func (StrictHandler) ListMeContactAlbums(context.Context, gen.ListMeContactAlbumsRequestObject) (gen.ListMeContactAlbumsResponseObject, error) {
	return nil, errCatalogServedByCatalog
}

func (StrictHandler) ListMeContactFavorites(context.Context, gen.ListMeContactFavoritesRequestObject) (gen.ListMeContactFavoritesResponseObject, error) {
	return nil, errCatalogServedByCatalog
}

func (StrictHandler) ListMeContactGalleries(context.Context, gen.ListMeContactGalleriesRequestObject) (gen.ListMeContactGalleriesResponseObject, error) {
	return nil, errCatalogServedByCatalog
}

func (StrictHandler) ListMeContactGroups(context.Context, gen.ListMeContactGroupsRequestObject) (gen.ListMeContactGroupsResponseObject, error) {
	return nil, errCatalogServedByCatalog
}

func (StrictHandler) SetMeContactGroupPoolCrawl(context.Context, gen.SetMeContactGroupPoolCrawlRequestObject) (gen.SetMeContactGroupPoolCrawlResponseObject, error) {
	return nil, errCatalogServedByCatalog
}

func (StrictHandler) ListMeContactPhotos(context.Context, gen.ListMeContactPhotosRequestObject) (gen.ListMeContactPhotosResponseObject, error) {
	return nil, errCatalogServedByCatalog
}

func (StrictHandler) PrioritiseMeContactSizes(context.Context, gen.PrioritiseMeContactSizesRequestObject) (gen.PrioritiseMeContactSizesResponseObject, error) {
	return nil, errCatalogServedByCatalog
}
