// Package http adapts catalog read operations to the generated HTTP contract.
package http

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/jooservices/xflickr/internal/catalog/app"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

// Handler implements authenticated catalog read endpoints. ActorID is
// populated by the identity middleware from the signed-in session.
type Handler struct {
	Reads        app.ReadService
	ContactReads app.ContactReads
	ActorID      func(context.Context) (int64, bool)
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
	page, next, err := h.Reads.ListPeople(ctx, userID, stringParam(request.Params.Cursor), pageLimit(request.Params.Limit))
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
	page, next, err := h.Reads.ListPersonPhotos(ctx, userID, request.Nsid, stringParam(request.Params.Cursor), pageLimit(request.Params.Limit))
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
	page, next, err := h.Reads.ListFavorites(ctx, userID, stringParam(request.Params.Cursor), pageLimit(request.Params.Limit))
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
	page, next, err := h.Reads.ListAlbums(ctx, userID, stringParam(request.Params.Cursor), pageLimit(request.Params.Limit))
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
	page, next, err := h.Reads.ListAlbumPhotos(ctx, userID, request.ID, stringParam(request.Params.Cursor), pageLimit(request.Params.Limit))
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

func pageLimit(value *int) int {
	if value == nil {
		return app.DefaultPageSize
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

func (h Handler) ListMeCrawlInterests(ctx context.Context, request gen.ListMeCrawlInterestsRequestObject) (gen.ListMeCrawlInterestsResponseObject, error) {
	userID, ok := h.actor(ctx)
	if !ok {
		return gen.ListMeCrawlInterests401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	filter, sortBy := "all", "updated"
	if request.Params.Filter != nil {
		filter = string(*request.Params.Filter)
	}
	if request.Params.Sort != nil {
		sortBy = string(*request.Params.Sort)
	}
	page, err := h.ContactReads.ListInterests(ctx, userID, filter, stringParam(request.Params.Q), sortBy, stringParam(request.Params.Cursor), contactLimit(request.Params.Limit))
	if errors.Is(err, app.ErrInvalidPage) || errors.Is(err, app.ErrInvalidCursor) {
		return gen.ListMeCrawlInterests400JSONResponse(invalidPageError()), nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]gen.CrawlInterest, 0, len(page.Items))
	for _, value := range page.Items {
		recent := make([]gen.CrawlInterestRecentPhoto, 0, len(value.RecentPhotos))
		for _, photo := range value.RecentPhotos {
			recent = append(recent, gen.CrawlInterestRecentPhoto{ID: photo.ID, Alt: photo.Alt, ThumbnailURL: photo.ThumbnailURL})
		}
		items = append(items, gen.CrawlInterest{Nsid: value.NSID, Name: value.Name, Username: value.Username, AvatarURL: validURL(value.AvatarURL), Depth: value.Depth, Starred: value.Starred, Status: gen.CrawlInterestStatus(value.Status), PhotosCrawled: value.PhotosCrawled, PhotosTotal: value.PhotosTotal, FavoritesCount: value.FavoritesCount, SizesPercent: value.SizesPercent, LastCheckAt: value.LastCheckAt, RecentPhotos: recent})
	}
	return gen.ListMeCrawlInterests200JSONResponse(gen.CrawlInterestPage{Items: items, Counts: gen.CrawlInterestCounts{All: page.Counts.All, Starred: page.Counts.Starred, Crawling: page.Counts.Crawling, NeedsAttention: page.Counts.NeedsAttention}, NextCursor: page.NextCursor, UpdatedAt: page.UpdatedAt}), nil
}

func (h Handler) GetMeContact(ctx context.Context, request gen.GetMeContactRequestObject) (gen.GetMeContactResponseObject, error) {
	userID, ok := h.actor(ctx)
	if !ok {
		return gen.GetMeContact401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	value, err := h.ContactReads.GetContact(ctx, userID, request.Nsid)
	if errors.Is(err, app.ErrNotFound) {
		return gen.GetMeContact404JSONResponse(notFoundError()), nil
	}
	if err != nil {
		return nil, err
	}
	return gen.GetMeContact200JSONResponse(contactDetail(value)), nil
}

func (h Handler) ListMeContactPhotos(ctx context.Context, request gen.ListMeContactPhotosRequestObject) (gen.ListMeContactPhotosResponseObject, error) {
	userID, ok := h.actor(ctx)
	if !ok {
		return gen.ListMeContactPhotos401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	page, err := h.ContactReads.ListContactPhotos(ctx, userID, request.Nsid, stringParam(request.Params.Cursor), contactLimit(request.Params.Limit))
	if errors.Is(err, app.ErrNotFound) {
		return gen.ListMeContactPhotos404JSONResponse(notFoundError()), nil
	}
	if errors.Is(err, app.ErrInvalidPage) || errors.Is(err, app.ErrInvalidCursor) {
		return gen.ListMeContactPhotos400JSONResponse(invalidPageError()), nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]gen.ContactPhoto, 0, len(page.Items))
	for _, value := range page.Items {
		items = append(items, gen.ContactPhoto{ID: value.ID, Title: value.Title, ThumbnailURL: validURL(value.ThumbnailURL), Width: positive(value.Width), Height: positive(value.Height), TakenAt: value.TakenAt, SizesAvailable: value.SizesAvailable})
	}
	return gen.ListMeContactPhotos200JSONResponse(gen.ContactPhotoPage{Items: items, Total: page.Total, NextCursor: page.NextCursor, UpdatedAt: page.UpdatedAt}), nil
}

func (h Handler) ListMeContactFavorites(ctx context.Context, request gen.ListMeContactFavoritesRequestObject) (gen.ListMeContactFavoritesResponseObject, error) {
	userID, ok := h.actor(ctx)
	if !ok {
		return gen.ListMeContactFavorites401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	page, err := h.ContactReads.ListContactFavorites(ctx, userID, request.Nsid, stringParam(request.Params.Cursor), contactLimit(request.Params.Limit))
	if errors.Is(err, app.ErrNotFound) {
		return gen.ListMeContactFavorites404JSONResponse(notFoundError()), nil
	}
	if errors.Is(err, app.ErrInvalidPage) || errors.Is(err, app.ErrInvalidCursor) {
		return gen.ListMeContactFavorites400JSONResponse(invalidPageError()), nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]gen.ContactFavorite, 0, len(page.Items))
	for _, value := range page.Items {
		items = append(items, gen.ContactFavorite{ID: value.ID, Title: value.Title, ThumbnailURL: validURL(value.ThumbnailURL), Width: positive(value.Width), Height: positive(value.Height), TakenAt: value.TakenAt, SizesAvailable: value.SizesAvailable, OwnerNsid: value.OwnerNSID, OwnerName: value.OwnerName, OwnerAvatarURL: validURL(value.OwnerAvatarURL)})
	}
	return gen.ListMeContactFavorites200JSONResponse(gen.ContactFavoritePage{Items: items, Total: page.Total, NextCursor: page.NextCursor, UpdatedAt: page.UpdatedAt}), nil
}

func (h Handler) ListMeContactAlbums(ctx context.Context, request gen.ListMeContactAlbumsRequestObject) (gen.ListMeContactAlbumsResponseObject, error) {
	userID, ok := h.actor(ctx)
	if !ok {
		return gen.ListMeContactAlbums401JSONResponse(gen.Error{Code: "unauthenticated"}), nil
	}
	page, err := h.ContactReads.ListContactAlbums(ctx, userID, request.Nsid, stringParam(request.Params.Cursor), contactLimit(request.Params.Limit))
	if errors.Is(err, app.ErrNotFound) {
		return gen.ListMeContactAlbums404JSONResponse(notFoundError()), nil
	}
	if errors.Is(err, app.ErrInvalidPage) || errors.Is(err, app.ErrInvalidCursor) {
		return gen.ListMeContactAlbums400JSONResponse(invalidPageError()), nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]gen.ContactAlbum, 0, len(page.Items))
	for _, v := range page.Items {
		items = append(items, gen.ContactAlbum{ID: v.ID, Title: v.Title, OwnerName: v.OwnerName, StoredCount: v.StoredCount, TotalCount: v.TotalCount, CoverPhotoUrls: v.CoverPhotoURLs})
	}
	return gen.ListMeContactAlbums200JSONResponse(gen.ContactAlbumPage{Items: items, Total: page.Total, NextCursor: page.NextCursor, UpdatedAt: page.UpdatedAt}), nil
}

func (h Handler) ListMeContactGalleries(ctx context.Context, request gen.ListMeContactGalleriesRequestObject) (gen.ListMeContactGalleriesResponseObject, error) {
	userID, ok := h.actor(ctx)
	if !ok {
		return gen.ListMeContactGalleries401JSONResponse(gen.Error{Code: "unauthenticated"}), nil
	}
	page, err := h.ContactReads.ListContactGalleries(ctx, userID, request.Nsid, stringParam(request.Params.Cursor), contactLimit(request.Params.Limit))
	if errors.Is(err, app.ErrNotFound) {
		return gen.ListMeContactGalleries404JSONResponse(notFoundError()), nil
	}
	if errors.Is(err, app.ErrInvalidPage) || errors.Is(err, app.ErrInvalidCursor) {
		return gen.ListMeContactGalleries400JSONResponse(invalidPageError()), nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]gen.ContactGallery, 0, len(page.Items))
	for _, v := range page.Items {
		items = append(items, gen.ContactGallery{ID: v.ID, Title: v.Title, PhotoCount: v.PhotoCount})
	}
	return gen.ListMeContactGalleries200JSONResponse(gen.ContactGalleryPage{Items: items, Total: page.Total, NextCursor: page.NextCursor, UpdatedAt: page.UpdatedAt}), nil
}

func (h Handler) ListMeContactGroups(ctx context.Context, request gen.ListMeContactGroupsRequestObject) (gen.ListMeContactGroupsResponseObject, error) {
	userID, ok := h.actor(ctx)
	if !ok {
		return gen.ListMeContactGroups401JSONResponse(gen.Error{Code: "unauthenticated"}), nil
	}
	page, err := h.ContactReads.ListContactGroups(ctx, userID, request.Nsid, stringParam(request.Params.Cursor), contactLimit(request.Params.Limit))
	if errors.Is(err, app.ErrNotFound) {
		return gen.ListMeContactGroups404JSONResponse(notFoundError()), nil
	}
	if errors.Is(err, app.ErrInvalidPage) || errors.Is(err, app.ErrInvalidCursor) {
		return gen.ListMeContactGroups400JSONResponse(invalidPageError()), nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]gen.ContactGroup, 0, len(page.Items))
	for _, v := range page.Items {
		items = append(items, gen.ContactGroup{Nsid: v.NSID, Name: v.Name, MembersCount: v.MembersCount, PoolCrawlEnabled: v.PoolCrawlEnabled})
	}
	return gen.ListMeContactGroups200JSONResponse(gen.ContactGroupPage{Items: items, Total: page.Total, NextCursor: page.NextCursor, UpdatedAt: page.UpdatedAt}), nil
}

func (h Handler) GetMeContactAbout(ctx context.Context, request gen.GetMeContactAboutRequestObject) (gen.GetMeContactAboutResponseObject, error) {
	userID, ok := h.actor(ctx)
	if !ok {
		return gen.GetMeContactAbout401JSONResponse(gen.Error{Code: "unauthenticated"}), nil
	}
	value, err := h.ContactReads.GetContactAbout(ctx, userID, request.Nsid)
	if errors.Is(err, app.ErrNotFound) {
		return gen.GetMeContactAbout404JSONResponse(notFoundError()), nil
	}
	if err != nil {
		return nil, err
	}
	facets := contactFacets(value.Facets)
	return gen.GetMeContactAbout200JSONResponse(gen.ContactAbout{LastChangeCheckAt: value.LastChangeCheckAt, Facets: facets, Tombstones: struct {
		Count     int64      `json:"count"`
		UpdatedAt *time.Time `json:"updated_at"`
	}{Count: value.TombstoneCount, UpdatedAt: value.TombstoneUpdatedAt}, UpdatedAt: value.UpdatedAt}), nil
}

func (h Handler) SetMeContactGroupPoolCrawl(ctx context.Context, request gen.SetMeContactGroupPoolCrawlRequestObject) (gen.SetMeContactGroupPoolCrawlResponseObject, error) {
	userID, ok := h.actor(ctx)
	if !ok {
		return gen.SetMeContactGroupPoolCrawl401JSONResponse(gen.Error{Code: "unauthenticated"}), nil
	}
	if request.Body == nil {
		return gen.SetMeContactGroupPoolCrawl404JSONResponse(gen.Error{Code: "invalid_request", Message: "Enabled value is required"}), nil
	}
	value, err := h.ContactReads.SetContactGroupPoolCrawl(ctx, userID, request.Nsid, request.GroupNsid, request.Body.Enabled)
	if errors.Is(err, app.ErrNotFound) {
		return gen.SetMeContactGroupPoolCrawl404JSONResponse(notFoundError()), nil
	}
	if err != nil {
		return nil, err
	}
	return gen.SetMeContactGroupPoolCrawl200JSONResponse(gen.ContactGroupPoolCrawlResponse{Enabled: value.Enabled, ChangedAt: value.ChangedAt}), nil
}

func (h Handler) PrioritiseMeContactSizes(ctx context.Context, request gen.PrioritiseMeContactSizesRequestObject) (gen.PrioritiseMeContactSizesResponseObject, error) {
	userID, ok := h.actor(ctx)
	if !ok {
		return gen.PrioritiseMeContactSizes401JSONResponse(gen.Error{Code: "unauthenticated"}), nil
	}
	count, err := h.ContactReads.PrioritiseContactSizes(ctx, userID, request.Nsid)
	if errors.Is(err, app.ErrNotFound) {
		return gen.PrioritiseMeContactSizes404JSONResponse(notFoundError()), nil
	}
	if err != nil {
		return nil, err
	}
	return gen.PrioritiseMeContactSizes200JSONResponse(gen.SizesPriorityResponse{Prioritised: count}), nil
}

func contactDetail(value app.ContactDetail) gen.ContactDetail {
	found := make([]struct {
		Label string `json:"label"`
		Nsid  string `json:"nsid"`
	}, 0, len(value.FoundVia))
	for _, item := range value.FoundVia {
		found = append(found, struct {
			Label string `json:"label"`
			Nsid  string `json:"nsid"`
		}{Label: item.Label, Nsid: item.NSID})
	}
	return gen.ContactDetail{Nsid: value.NSID, Username: value.Username, RealName: value.RealName, AvatarURL: validURL(value.AvatarURL), CoverPhotoURL: validURL(value.CoverPhotoURL), Location: value.Location, Depth: value.Depth, FoundVia: found, Starred: value.Starred, Paused: value.Paused, CrawlStatus: gen.ContactDetailCrawlStatus(value.CrawlStatus), PhotoRecordsCrawled: value.PhotoRecordsCrawled, PhotoRecordsTotal: value.PhotoRecordsTotal, SizesCompleted: value.SizesCompleted, Facets: contactFacets(value.Facets), Counts: gen.ContactCounts{Photos: value.Counts.Photos, Favorites: value.Counts.Favorites, Albums: value.Counts.Albums, Galleries: value.Counts.Galleries, Groups: value.Counts.Groups}, UpdatedAt: value.UpdatedAt}
}

func contactFacets(values []app.ContactFacet) []gen.ContactFacet {
	result := make([]gen.ContactFacet, 0, len(values))
	for _, v := range values {
		result = append(result, gen.ContactFacet{Name: gen.ContactFacetName(v.Name), Completed: v.Completed, Total: v.Total, Status: gen.ContactFacetStatus(v.Status), UpdatedAt: v.UpdatedAt})
	}
	return result
}

func contactLimit(value *int) int {
	if value == nil {
		return app.DefaultPageSize
	}
	return *value
}

func positive(value *int) *int {
	if value == nil || *value < 1 {
		return nil
	}
	return value
}

func validURL(value *string) *string {
	if value == nil {
		return nil
	}
	parsed, err := url.Parse(strings.TrimSpace(*value))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return nil
	}
	clean := parsed.String()
	return &clean
}
