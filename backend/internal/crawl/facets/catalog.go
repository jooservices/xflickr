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
				ID          string  `json:"id"`
				Primary     string  `json:"primary"`
				Count       int64   `json:"photos"`
				Title       content `json:"title"`
				Description content `json:"description"`
				Created     string  `json:"date_create"`
				Updated     string  `json:"date_update"`
			} `json:"photoset"`
		} `json:"photosets"`
	}
	if err := json.Unmarshal(p.Payload, &e); err != nil {
		return domain.Result{}, fmt.Errorf("crawl facets: decode albums: %w", err)
	}
	out := domain.Result{Albums: make([]catalog.Album, 0, len(e.Photosets.Items)), People: []catalog.Person{{NSID: p.Item.Subject, Username: p.Item.Subject}}}
	for _, x := range e.Photosets.Items {
		primary, _ := strconv.ParseInt(x.Primary, 10, 64)
		out.Albums = append(out.Albums, catalog.Album{ID: x.ID, OwnerNSID: p.Item.Subject, Title: x.Title.value(), Description: x.Description.value(), PrimaryPhotoID: positive(primary), PhotosCount: x.Count, CreatedAt: timeValue(x.Created), UpdatedAt: timeValue(x.Updated)})
	}
	out.HasMore = p.Number < e.Photosets.Pages && len(e.Photosets.Items) > 0
	return out, nil
}

type Galleries struct{}

func (Galleries) Name() string      { return "galleries" }
func (Galleries) Lane() domain.Lane { return domain.LanePerson }
func (Galleries) Plan(_ context.Context, p domain.Person) []domain.WorkSpec {
	return []domain.WorkSpec{{Subject: p.NSID, Method: "flickr.galleries.getList", Params: map[string]string{"user_id": p.NSID, "per_page": catalogPageSize}}}
}

func (Galleries) Handle(_ context.Context, p domain.Page) (domain.Result, error) {
	var e struct {
		Galleries struct {
			Pages int `json:"pages"`
			Items []struct {
				ID          string  `json:"id"`
				Owner       string  `json:"owner"`
				Primary     string  `json:"primary_photo_id"`
				Count       int64   `json:"count_photos"`
				Title       content `json:"title"`
				Description content `json:"description"`
				Created     string  `json:"date_create"`
				Updated     string  `json:"date_update"`
			} `json:"gallery"`
		} `json:"galleries"`
	}
	if err := json.Unmarshal(p.Payload, &e); err != nil {
		return domain.Result{}, fmt.Errorf("crawl facets: decode galleries: %w", err)
	}
	out := domain.Result{Galleries: make([]catalog.Gallery, 0, len(e.Galleries.Items)), People: []catalog.Person{{NSID: p.Item.Subject, Username: p.Item.Subject}}}
	for _, x := range e.Galleries.Items {
		owner := x.Owner
		if owner == "" {
			owner = p.Item.Subject
		}
		if owner != p.Item.Subject {
			out.People = append(out.People, catalog.Person{NSID: owner, Username: owner})
		}
		primary, _ := strconv.ParseInt(x.Primary, 10, 64)
		out.Galleries = append(out.Galleries, catalog.Gallery{ID: x.ID, OwnerNSID: owner, Title: x.Title.value(), Description: x.Description.value(), PrimaryPhotoID: positive(primary), PhotosCount: x.Count, CreatedAt: timeValue(x.Created), UpdatedAt: timeValue(x.Updated)})
	}
	out.HasMore = p.Number < e.Galleries.Pages && len(e.Galleries.Items) > 0
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
		Groups struct {
			Pages int `json:"pages"`
			Items []struct {
				NSID        string `json:"nsid"`
				Name        string `json:"name"`
				Members     int64  `json:"members"`
				Pool        int64  `json:"pool_count"`
				Description string `json:"description"`
			} `json:"group"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(p.Payload, &e); err != nil {
		return domain.Result{}, fmt.Errorf("crawl facets: decode groups: %w", err)
	}
	out := domain.Result{Groups: make([]catalog.Group, 0, len(e.Groups.Items)), PersonGroups: make([]catalog.PersonGroup, 0, len(e.Groups.Items)), People: []catalog.Person{{NSID: p.Item.Subject, Username: p.Item.Subject}}}
	for _, x := range e.Groups.Items {
		out.Groups = append(out.Groups, catalog.Group{NSID: x.NSID, Name: x.Name, Description: x.Description, MembersCount: x.Members, PoolCount: x.Pool})
		out.PersonGroups = append(out.PersonGroups, catalog.PersonGroup{PersonNSID: p.Item.Subject, GroupNSID: x.NSID})
	}
	out.HasMore = p.Number < e.Groups.Pages && len(e.Groups.Items) > 0
	return out, nil
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
