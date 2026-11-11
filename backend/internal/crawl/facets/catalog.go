package facets

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/crawl/domain"
	"github.com/jooservices/xflickr/pkg/flickr"
)

const catalogPageSize = "500"

type Albums struct{}

func (Albums) Name() string      { return "albums" }
func (Albums) Lane() domain.Lane { return domain.LanePerson }
func (Albums) Plan(_ context.Context, p domain.Person) []domain.WorkSpec {
	return []domain.WorkSpec{{Subject: p.NSID, Method: "flickr.photosets.getList", Params: map[string]string{"user_id": p.NSID, "per_page": catalogPageSize}}}
}

func (Albums) Handle(_ context.Context, p domain.Page) (domain.Result, error) {
	var e struct {
		Photosets struct {
			Pages int `json:"pages"`
			Items []struct {
				ID          string     `json:"id"`
				Primary     string     `json:"primary"`
				Count       flickr.Int `json:"photos"`
				Title       content    `json:"title"`
				Description content    `json:"description"`
				Created     string     `json:"date_create"`
				Updated     string     `json:"date_update"`
			} `json:"photoset"`
		} `json:"photosets"`
	}
	if err := json.Unmarshal(p.Payload, &e); err != nil {
		return domain.Result{}, fmt.Errorf("crawl facets: decode albums: %w", err)
	}
	out := domain.Result{Albums: make([]catalog.Album, 0, len(e.Photosets.Items))}
	for _, x := range e.Photosets.Items {
		primary, _ := strconv.ParseInt(x.Primary, 10, 64)
		out.Albums = append(out.Albums, catalog.Album{ID: x.ID, OwnerNSID: p.Item.Subject, Title: x.Title.value(), Description: x.Description.value(), PrimaryPhotoID: positive(primary), PhotosCount: int64(x.Count), CreatedAt: timeValue(x.Created), UpdatedAt: timeValue(x.Updated)})
	}
	out.HasMore = p.Number < e.Photosets.Pages && len(e.Photosets.Items) > 0
	return out, nil
}

type Galleries struct{}

func (Galleries) Name() string      { return "galleries" }
func (Galleries) Lane() domain.Lane { return domain.LanePerson }
func (Galleries) Plan(_ context.Context, p domain.Person) []domain.WorkSpec {
	return []domain.WorkSpec{{Subject: p.NSID, Method: "flickr.galleries.getList", Params: map[string]string{"user_id": p.NSID, "per_page": catalogPageSize, "continuation": "0"}}}
}

func (Galleries) Handle(_ context.Context, p domain.Page) (domain.Result, error) {
	var e struct {
		Galleries struct {
			Pages        int    `json:"pages"`
			Continuation string `json:"continuation"`
			Items        []struct {
				ID          string     `json:"id"`
				Owner       string     `json:"owner"`
				Primary     string     `json:"primary_photo_id"`
				Count       flickr.Int `json:"count_photos"`
				Title       content    `json:"title"`
				Description content    `json:"description"`
				Created     string     `json:"date_create"`
				Updated     string     `json:"date_update"`
			} `json:"gallery"`
		} `json:"galleries"`
	}
	if err := json.Unmarshal(p.Payload, &e); err != nil {
		return domain.Result{}, fmt.Errorf("crawl facets: decode galleries: %w", err)
	}
	out := domain.Result{Galleries: make([]catalog.Gallery, 0, len(e.Galleries.Items))}
	for _, x := range e.Galleries.Items {
		owner := x.Owner
		if owner == "" {
			owner = p.Item.Subject
		}
		if owner != p.Item.Subject {
			out.People = append(out.People, catalog.Person{NSID: owner, Username: owner})
		}
		primary, _ := strconv.ParseInt(x.Primary, 10, 64)
		out.Galleries = append(out.Galleries, catalog.Gallery{ID: x.ID, OwnerNSID: owner, Title: x.Title.value(), Description: x.Description.value(), PrimaryPhotoID: positive(primary), PhotosCount: int64(x.Count), CreatedAt: timeValue(x.Created), UpdatedAt: timeValue(x.Updated)})
	}
	if e.Galleries.Continuation == "" {
		out.HasMore = p.Number < e.Galleries.Pages && len(e.Galleries.Items) > 0
		return out, nil
	}
	out.NextCursor = e.Galleries.Continuation
	if e.Galleries.Continuation == "-1" {
		return out, nil
	}
	if e.Galleries.Continuation == p.Item.Cursor.Continuation {
		out.CursorWarning = "repeated continuation"
		return out, nil
	}
	if len(e.Galleries.Items) == 0 {
		out.CursorWarning = "empty page with nonterminal continuation"
		return out, nil
	}
	out.HasMore = true
	return out, nil
}

type Groups struct{}

func (Groups) Name() string      { return "groups" }
func (Groups) Lane() domain.Lane { return domain.LanePerson }
func (Groups) Plan(_ context.Context, p domain.Person) []domain.WorkSpec {
	return []domain.WorkSpec{{Subject: p.NSID, Method: "flickr.people.getGroups", Params: map[string]string{"user_id": p.NSID, "per_page": catalogPageSize}}}
}

func (Groups) Handle(_ context.Context, p domain.Page) (domain.Result, error) {
	var e struct {
		Groups *struct {
			Pages int             `json:"pages"`
			Items json.RawMessage `json:"group"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(p.Payload, &e); err != nil {
		return domain.Result{}, fmt.Errorf("crawl facets: decode groups: %w", err)
	}
	if e.Groups == nil {
		return domain.Result{}, fmt.Errorf("crawl facets: decode groups: groups envelope is missing")
	}
	var items []struct {
		NSID        string     `json:"nsid"`
		Name        string     `json:"name"`
		Members     flickr.Int `json:"members"`
		Pool        flickr.Int `json:"pool_count"`
		Description string     `json:"description"`
	}
	if len(e.Groups.Items) > 0 {
		if strings.TrimSpace(string(e.Groups.Items)) == "null" {
			return domain.Result{}, fmt.Errorf("crawl facets: decode groups: group list is null")
		}
		if err := json.Unmarshal(e.Groups.Items, &items); err != nil {
			return domain.Result{}, fmt.Errorf("crawl facets: decode groups: %w", err)
		}
	}
	if len(items) == 0 && p.Number < e.Groups.Pages {
		return domain.Result{}, fmt.Errorf("crawl facets: decode groups: empty page before final page")
	}
	for i, x := range items {
		if strings.TrimSpace(x.NSID) == "" {
			return domain.Result{}, fmt.Errorf("crawl facets: decode groups: group %d has no nsid", i+1)
		}
		if strings.TrimSpace(x.Name) == "" {
			return domain.Result{}, fmt.Errorf("crawl facets: decode groups: group %d has no name", i+1)
		}
	}
	out := domain.Result{Groups: make([]catalog.Group, 0, len(items)), PersonGroups: make([]catalog.PersonGroup, 0, len(items))}
	for _, x := range items {
		out.Groups = append(out.Groups, catalog.Group{NSID: x.NSID, Name: x.Name, Description: x.Description, MembersCount: int64(x.Members), PoolCount: int64(x.Pool)})
		out.PersonGroups = append(out.PersonGroups, catalog.PersonGroup{PersonNSID: p.Item.Subject, GroupNSID: x.NSID})
	}
	out.HasMore = p.Number < e.Groups.Pages && len(items) > 0
	return out, nil
}

// GroupPool reads a group pool only for a work item created by explicit opt-in.
type GroupPool struct{}

func (GroupPool) Name() string                                          { return "group_pool" }
func (GroupPool) Lane() domain.Lane                                     { return domain.LanePerson }
func (GroupPool) Plan(context.Context, domain.Person) []domain.WorkSpec { return nil }

func (GroupPool) Handle(ctx context.Context, page domain.Page) (domain.Result, error) {
	result, err := parsePhotoPage(page, false)
	if err != nil {
		return domain.Result{}, err
	}
	result.GroupPoolPhotos = make([]catalog.GroupPoolPhoto, 0, len(result.Photos))
	for _, photo := range result.Photos {
		result.GroupPoolPhotos = append(result.GroupPoolPhotos, catalog.GroupPoolPhoto{GroupNSID: page.Item.Subject, PhotoID: photo.ID})
	}
	return result, nil
}

type content struct {
	Content string `json:"_content"`
	Text    string `json:"content"`
}

func (c content) value() string {
	if c.Content != "" {
		return c.Content
	}
	return c.Text
}
func parseUnix(s string) int64 { n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64); return n }
func positive(n int64) *int64 {
	if n <= 0 {
		return nil
	}
	return &n
}

func timeValue(s string) time.Time {
	if n := parseUnix(s); n > 0 {
		return time.Unix(n, 0).UTC()
	}
	return time.Time{}
}
