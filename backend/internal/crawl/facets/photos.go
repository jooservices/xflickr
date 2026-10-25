// Package facets implements the Flickr person crawl facets.
package facets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/crawl/domain"
	"github.com/jooservices/xflickr/pkg/flickr"
)

const (
	photosPerPage = 500
	photoExtras   = "url_sq,url_t,url_s,url_q,url_m,url_n,url_z,url_c,url_l,url_o,date_upload,last_update,owner_name,media,tags,description,license,o_dims,views"
)

// Photos reads a person's photos and stores the records with provisional sizes
// obtained from Flickr's URL extras.
type Photos struct{}

func (Photos) Name() string      { return "photos" }
func (Photos) Lane() domain.Lane { return domain.LanePerson }

func (Photos) Plan(_ context.Context, person domain.Person) []domain.WorkSpec {
	return []domain.WorkSpec{{
		Subject: person.NSID,
		Method:  "flickr.people.getPhotos",
		Params: map[string]string{
			"user_id":  person.NSID,
			"per_page": strconv.Itoa(photosPerPage),
			"extras":   photoExtras,
		},
	}}
}

func (Photos) Handle(_ context.Context, page domain.Page) (domain.Result, error) {
	return parsePhotoPage(page, false)
}

type photoEnvelope struct {
	Photos struct {
		Pages int               `json:"pages"`
		Photo []json.RawMessage `json:"photo"`
	} `json:"photos"`
}

type provisionalSize struct {
	Label  string `json:"label"`
	Source string `json:"source"`
	URL    string `json:"url"`
}

var urlExtras = []struct{ field, label string }{
	{"url_sq", "Square"},
	{"url_t", "Thumbnail"},
	{"url_s", "Small 240"},
	{"url_q", "Small 150"},
	{"url_m", "Medium 500"},
	{"url_n", "Medium 320"},
	{"url_z", "Medium 640"},
	{"url_c", "Medium 800"},
	{"url_l", "Large 1024"},
	{"url_o", "Original"},
}

func parsePhotoPage(page domain.Page, discoverOwners bool) (domain.Result, error) {
	var envelope photoEnvelope
	if err := json.Unmarshal(page.Payload, &envelope); err != nil {
		return domain.Result{}, fmt.Errorf("crawl facets: decode photo page: %w", err)
	}
	result := domain.Result{Photos: make([]catalog.Photo, 0, len(envelope.Photos.Photo))}
	people := make(map[string]catalog.Person)
	seenOwners := make(map[string]bool)
	for i, raw := range envelope.Photos.Photo {
		var photo flickr.Photo
		if err := json.Unmarshal(raw, &photo); err != nil {
			return domain.Result{}, fmt.Errorf("crawl facets: decode photo %d: %w", i, err)
		}
		converted, err := toCatalogPhoto(photo, raw)
		if err != nil {
			return domain.Result{}, fmt.Errorf("crawl facets: convert photo %d: %w", i, err)
		}
		result.Photos = append(result.Photos, converted)
		if converted.OwnerNSID != "" {
			people[converted.OwnerNSID] = catalog.Person{NSID: converted.OwnerNSID, Username: photo.OwnerName}
			if discoverOwners && !seenOwners[converted.OwnerNSID] {
				result.Discoveries = append(result.Discoveries, domain.Discovery{NSID: converted.OwnerNSID, Via: "favorite"})
				seenOwners[converted.OwnerNSID] = true
			}
		}
	}
	result.People = make([]catalog.Person, 0, len(people))
	for _, person := range people {
		result.People = append(result.People, person)
	}
	result.HasMore = page.Number < envelope.Photos.Pages && len(envelope.Photos.Photo) > 0
	return result, nil
}

func toCatalogPhoto(photo flickr.Photo, raw []byte) (catalog.Photo, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(string(photo.ID)), 10, 64)
	if err != nil || id <= 0 || strings.TrimSpace(photo.Owner) == "" {
		return catalog.Photo{}, errors.New("photo id and owner are required")
	}
	uploaded := unixTime(int64(photo.DateUpload))
	updated := unixTime(int64(photo.LastUpdate))
	if updated == nil {
		updated = uploaded
	}
	if updated == nil {
		return catalog.Photo{}, errors.New("photo update time is required")
	}
	visibility := catalog.VisibilityPrivate
	switch {
	case photo.IsPublic.Int() != 0:
		visibility = catalog.VisibilityPublic
	case photo.IsFriend.Int() != 0:
		visibility = catalog.VisibilityFriends
	case photo.IsFamily.Int() != 0:
		visibility = catalog.VisibilityFamily
	}
	tags := strings.Fields(photo.Tags)
	sizes, err := json.Marshal(extractSizes(raw))
	if err != nil {
		return catalog.Photo{}, fmt.Errorf("encode provisional sizes: %w", err)
	}
	return catalog.Photo{
		ID: id, OwnerNSID: string(photo.Owner), Secret: photo.Secret, Server: string(photo.Server),
		Title: photo.Title, Description: string(photo.Description), Tags: tags,
		UploadedAt: uploaded, LastUpdate: *updated, Visibility: visibility,
		License: string(photo.License), Media: photo.Media, Sizes: sizes,
		SizesStatus: catalog.SizesProvisional, Raw: append([]byte(nil), raw...),
	}, nil
}

func extractSizes(raw []byte) []provisionalSize {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return []provisionalSize{}
	}
	sizes := make([]provisionalSize, 0, len(urlExtras))
	for _, extra := range urlExtras {
		var url string
		if json.Unmarshal(fields[extra.field], &url) == nil && url != "" {
			sizes = append(sizes, provisionalSize{Label: extra.label, Source: extra.field, URL: url})
		}
	}
	return sizes
}

func unixTime(value int64) *time.Time {
	if value <= 0 {
		return nil
	}
	t := time.Unix(value, 0).UTC()
	return &t
}

var _ domain.Facet = Photos{}
