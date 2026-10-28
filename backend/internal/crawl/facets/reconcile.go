package facets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/jooservices/xflickr/internal/crawl/domain"
)

// Reconcile reads a person's complete photo and favorite lists. The engine
// applies the missing-item diff only when the final favorites page commits.
type Reconcile struct{}

func (Reconcile) Name() string      { return "reconcile" }
func (Reconcile) Lane() domain.Lane { return domain.LanePerson }
func (Reconcile) Plan(context.Context, domain.Person) []domain.WorkSpec {
	return nil
}

func (Reconcile) Handle(ctx context.Context, page domain.Page) (domain.Result, error) {
	if page.Item.Subject == "" || page.Item.Cursor.CheckpointAt <= 0 || page.Number < 1 {
		return domain.Result{}, errors.New("crawl facets: invalid reconcile page")
	}
	_, _, err := validateReconcileListPage(page)
	if err != nil {
		return domain.Result{}, err
	}
	switch page.Item.Cursor.Method {
	case "flickr.people.getPhotos":
		result, err := parsePhotoPage(page, false)
		if err != nil {
			return domain.Result{}, err
		}
		if !result.HasMore {
			result.NextCursor = &domain.Cursor{
				Page: 1, Method: "flickr.favorites.getList", CheckpointAt: page.Item.Cursor.CheckpointAt,
				Params: map[string]string{"user_id": page.Item.Subject, "per_page": strconv.Itoa(photosPerPage), "extras": photoExtras},
			}
		}
		return result, nil
	case "flickr.favorites.getList":
		result, err := (Favorites{}).Handle(ctx, page)
		if err != nil {
			return domain.Result{}, err
		}
		// Reconciliation refreshes this person's existing catalog only. It does
		// not expand the user's crawl graph with newly observed favorite owners.
		result.Discoveries = nil
		if !result.HasMore {
			result.CompleteReconcile = true
		}
		return result, nil
	default:
		return domain.Result{}, fmt.Errorf("crawl facets: unsupported reconcile method %q", page.Item.Cursor.Method)
	}
}

func validateReconcileListPage(page domain.Page) (int, int, error) {
	var envelope struct {
		Photos *struct {
			Pages json.RawMessage `json:"pages"`
			Photo json.RawMessage `json:"photo"`
		} `json:"photos"`
	}
	if err := json.Unmarshal(page.Payload, &envelope); err != nil {
		return 0, 0, fmt.Errorf("crawl facets: decode reconcile list: %w", err)
	}
	if envelope.Photos == nil || len(envelope.Photos.Pages) == 0 || len(envelope.Photos.Photo) == 0 {
		return 0, 0, errors.New("crawl facets: reconcile response is missing pages or photo list")
	}
	var pages int
	if err := json.Unmarshal(envelope.Photos.Pages, &pages); err != nil || pages < 0 {
		return 0, 0, errors.New("crawl facets: reconcile response has invalid page count")
	}
	var photos []json.RawMessage
	if err := json.Unmarshal(envelope.Photos.Photo, &photos); err != nil {
		return 0, 0, fmt.Errorf("crawl facets: decode reconcile photos: %w", err)
	}
	if (pages == 0 && (page.Number != 1 || len(photos) != 0)) || (pages > 0 && page.Number > pages) {
		return 0, 0, errors.New("crawl facets: reconcile response page count is inconsistent")
	}
	if page.Number < pages && len(photos) == 0 {
		return 0, 0, errors.New("crawl facets: reconcile response contains an empty non-final page")
	}
	return pages, len(photos), nil
}

var _ domain.Facet = Reconcile{}
