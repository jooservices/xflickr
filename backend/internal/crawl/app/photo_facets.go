package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
	quotadomain "github.com/jooservices/xflickr/internal/quota/domain"
	"github.com/jooservices/xflickr/pkg/flickr"
)

const (
	PhotoInfoFacet      = "info"
	PhotoExifFacet      = "exif"
	PhotoContextsFacet  = "contexts"
	PhotoCommentsFacet  = "comments"
	PhotoFavoritesFacet = "photo_favorites"
	maxPhotoFacetPages  = 200
)

var errInvalidPhotoFacetJob = errors.New("crawl: invalid photo facet job")

// PhotoFacetJob identifies one optional per-photo Flickr request.
type PhotoFacetJob struct {
	PhotoID int64             `json:"photo_id"`
	KeyID   int64             `json:"key_id"`
	Facet   string            `json:"facet"`
	Class   quotadomain.Class `json:"class,omitempty"`
}

// ParsePhotoFacetJob validates a durable optional photo-facet job payload.
func ParsePhotoFacetJob(args []byte) (PhotoFacetJob, error) {
	var job PhotoFacetJob
	if err := json.Unmarshal(args, &job); err != nil || job.PhotoID <= 0 || job.KeyID <= 0 || !validPhotoFacet(job.Facet) {
		return PhotoFacetJob{}, errInvalidPhotoFacetJob
	}
	if job.Class != "" && job.Class != quotadomain.Photo && job.Class != quotadomain.Interactive {
		return PhotoFacetJob{}, errInvalidPhotoFacetJob
	}
	return job, nil
}

func validPhotoFacet(facet string) bool {
	switch facet {
	case PhotoInfoFacet, PhotoExifFacet, PhotoContextsFacet, PhotoCommentsFacet, PhotoFavoritesFacet:
		return true
	default:
		return false
	}
}

// PhotoFacetStore loads a photo and stores a completed optional facet.
type PhotoFacetStore interface {
	LoadPhotoFacetSource(context.Context, int64) (catalog.PhotoFacetSource, bool, error)
	SetPhotoFacet(context.Context, int64, string, []byte, time.Time, time.Time) error
}

// PhotoFacetClient performs the Flickr methods used for optional photo data.
// List methods fetch one page; each page consumes its own quota permit.
type PhotoFacetClient interface {
	PhotosGetInfo(context.Context, int64, string, string) (flickr.PhotoInfo, error)
	PhotosGetExif(context.Context, int64, string, string) (flickr.PhotoExif, error)
	PhotosGetAllContexts(context.Context, int64, string) (flickr.PhotoContexts, error)
	PhotosCommentsGetList(context.Context, int64, string, int) (flickr.Page[flickr.PhotoComment], error)
	PhotosGetFavorites(context.Context, int64, string, int) (flickr.Page[flickr.PhotoFavorite], error)
}

// PhotoFacets fetches optional metadata only after the size facet has
// confirmed a photo for its current Flickr version.
type PhotoFacets struct {
	Store  PhotoFacetStore
	Flickr PhotoFacetClient
	Now    func() time.Time
}

// Handle fetches and persists one optional per-photo facet.
func (h PhotoFacets) Handle(ctx context.Context, job PhotoFacetJob) error {
	if ctx == nil || h.Store == nil || h.Flickr == nil || job.PhotoID <= 0 || job.KeyID <= 0 || !validPhotoFacet(job.Facet) {
		return errors.New("crawl: photo facets dependencies and identifiers are required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	source, exists, err := h.Store.LoadPhotoFacetSource(ctx, job.PhotoID)
	if err != nil {
		return fmt.Errorf("crawl: load photo %d facet source: %w", job.PhotoID, err)
	}
	if !photoFacetSourceReady(source, exists) {
		return nil
	}

	data, err := h.fetch(ctx, job, source)
	if err != nil {
		if !isTerminalPhotoFacetError(err) {
			return fmt.Errorf("crawl: fetch photo %d %s facet: %w", job.PhotoID, job.Facet, err)
		}
		data, err = emptyPhotoFacet(job.Facet)
		if err != nil {
			return err
		}
	}
	if err := h.Store.SetPhotoFacet(ctx, job.PhotoID, job.Facet, data, source.LastUpdate, h.now()); err != nil {
		return fmt.Errorf("crawl: store photo %d %s facet: %w", job.PhotoID, job.Facet, err)
	}
	return nil
}

func photoFacetSourceReady(source catalog.PhotoFacetSource, exists bool) bool {
	return exists && source.SizesStatus == catalog.SizesConfirmed && source.SizesCheckedAt != nil && !source.SizesCheckedAt.Before(source.LastUpdate)
}

func (h PhotoFacets) fetch(ctx context.Context, job PhotoFacetJob, source catalog.PhotoFacetSource) ([]byte, error) {
	photoID := strconv.FormatInt(job.PhotoID, 10)
	switch job.Facet {
	case PhotoInfoFacet:
		info, err := h.Flickr.PhotosGetInfo(ctx, job.KeyID, photoID, source.Secret)
		if err != nil {
			return nil, err
		}
		info.Secret, info.OriginalSecret = "", ""
		// #nosec G117 -- The secret fields are deliberately cleared above before serialization.
		return json.Marshal(info)
	case PhotoExifFacet:
		exif, err := h.Flickr.PhotosGetExif(ctx, job.KeyID, photoID, source.Secret)
		if err != nil {
			return nil, err
		}
		exif.Secret = ""
		// #nosec G117 -- The API response secret is deliberately cleared above before serialization.
		return json.Marshal(exif)
	case PhotoContextsFacet:
		contexts, err := h.Flickr.PhotosGetAllContexts(ctx, job.KeyID, photoID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(contexts)
	case PhotoCommentsFacet:
		items, err := h.comments(ctx, job.KeyID, photoID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(items)
	case PhotoFavoritesFacet:
		items, err := h.favorites(ctx, job.KeyID, photoID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(items)
	default:
		return nil, errInvalidPhotoFacetJob
	}
}

func (h PhotoFacets) comments(ctx context.Context, keyID int64, photoID string) ([]flickr.PhotoComment, error) {
	var all []flickr.PhotoComment
	for page := 1; page <= maxPhotoFacetPages; page++ {
		result, err := h.Flickr.PhotosCommentsGetList(ctx, keyID, photoID, page)
		if err != nil {
			return nil, err
		}
		all = append(all, result.Items...)
		if len(result.Items) == 0 || page >= result.Pages {
			return all, nil
		}
	}
	return nil, errors.New("crawl: photo comments exceeded the page safety limit")
}

func (h PhotoFacets) favorites(ctx context.Context, keyID int64, photoID string) ([]flickr.PhotoFavorite, error) {
	var all []flickr.PhotoFavorite
	for page := 1; page <= maxPhotoFacetPages; page++ {
		result, err := h.Flickr.PhotosGetFavorites(ctx, keyID, photoID, page)
		if err != nil {
			return nil, err
		}
		all = append(all, result.Items...)
		if len(result.Items) == 0 || page >= result.Pages {
			return all, nil
		}
	}
	return nil, errors.New("crawl: photo favorites exceeded the page safety limit")
}

func emptyPhotoFacet(facet string) ([]byte, error) {
	switch facet {
	case PhotoInfoFacet, PhotoExifFacet:
		return []byte(`{}`), nil
	case PhotoContextsFacet:
		return []byte(`{"set":[],"pool":[]}`), nil
	case PhotoCommentsFacet, PhotoFavoritesFacet:
		return []byte(`[]`), nil
	default:
		return nil, errInvalidPhotoFacetJob
	}
}

func isTerminalPhotoFacetError(err error) bool {
	return errors.Is(err, flickr.ErrNotFound) || errors.Is(err, flickr.ErrPermission)
}

func (h PhotoFacets) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}
