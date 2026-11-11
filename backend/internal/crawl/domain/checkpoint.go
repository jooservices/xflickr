package domain

import "strconv"

// ScanCheckpoint is a Unix-second inclusive lower bound for the next
// incremental photos or favorites list read for one observer and person.
type ScanCheckpoint int64

// Valid reports whether c can be sent to Flickr as a date filter.
func (c ScanCheckpoint) Valid() bool { return c > 0 }

// Unix returns the checkpoint as Unix seconds for Flickr parameters.
func (c ScanCheckpoint) Unix() int64 { return int64(c) }

// Facet kinds that persist a scan-start checkpoint on successful completion.
const (
	FacetPhotos    = "photos"
	FacetFavorites = "favorites"
)

// Flickr date filter parameter names. Official docs:
// https://www.flickr.com/services/api/flickr.people.getPhotos.html
// (min_upload_date — inclusive upload lower bound; XFlickr does not use
// min_taken_date for crawl checkpoints)
// https://www.flickr.com/services/api/flickr.favorites.getList.html
// (min_fave_date — inclusive favorite-date lower bound)
const (
	ParamMinUploadDate = "min_upload_date"
	ParamMinFaveDate   = "min_fave_date"
)

// NeedsScanStartCheckpoint reports whether kind stores a photos/favorites
// scan-start marker on its work cursor.
func NeedsScanStartCheckpoint(kind string) bool {
	return kind == FacetPhotos || kind == FacetFavorites
}

// NextCheckpoint is the single rule for what to persist after a photos or
// favorites scan. scanStart is captured before the first page request.
// pagesCommitted is true only when every page of that scan committed. A
// partial or failed scan keeps old; a successful scan stores scanStart so the
// next incremental read uses an inclusive lower bound that also covers late
// arrivals during the scan.
func NextCheckpoint(scanStart ScanCheckpoint, pagesCommitted bool, old ScanCheckpoint) ScanCheckpoint {
	if !pagesCommitted || !scanStart.Valid() {
		return old
	}
	return scanStart
}

// PhotosCheckpointFilter returns the inclusive people.getPhotos date filter.
func PhotosCheckpointFilter(checkpoint ScanCheckpoint) map[string]string {
	return map[string]string{ParamMinUploadDate: strconv.FormatInt(checkpoint.Unix(), 10)}
}

// FavoritesCheckpointFilter returns the inclusive favorites.getList date filter.
func FavoritesCheckpointFilter(checkpoint ScanCheckpoint) map[string]string {
	return map[string]string{ParamMinFaveDate: strconv.FormatInt(checkpoint.Unix(), 10)}
}
