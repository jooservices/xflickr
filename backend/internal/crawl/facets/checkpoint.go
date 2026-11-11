package facets

import "github.com/jooservices/xflickr/internal/crawl/domain"

// Re-export the crawl date-filter helpers so facet handlers and tests share
// one vocabulary with the domain rule without duplicating parameter names.
const (
	ParamMinUploadDate = domain.ParamMinUploadDate
	ParamMinFaveDate   = domain.ParamMinFaveDate
)

// PhotosCheckpointFilter returns the inclusive people.getPhotos date filter.
func PhotosCheckpointFilter(checkpoint domain.ScanCheckpoint) map[string]string {
	return domain.PhotosCheckpointFilter(checkpoint)
}

// FavoritesCheckpointFilter returns the inclusive favorites.getList date filter.
func FavoritesCheckpointFilter(checkpoint domain.ScanCheckpoint) map[string]string {
	return domain.FavoritesCheckpointFilter(checkpoint)
}
