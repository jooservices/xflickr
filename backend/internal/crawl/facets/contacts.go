package facets

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/crawl/domain"
	"github.com/jooservices/xflickr/pkg/flickr"
)

// Contacts enumerates the authenticated account's contact list. Only depth-0
// subjects (connected accounts) plan this work; deeper people never call
// contacts.getList because that method always returns the signed-in member's
// own contacts.
type Contacts struct{}

type contactsResponse struct {
	Contacts *contactsPage `json:"contacts"`
}

type contactsPage struct {
	Pages *int             `json:"pages"`
	Items *[]contactRecord `json:"contact"`
}

type contactRecord struct {
	NSID     string     `json:"nsid"`
	Username string     `json:"username"`
	Realname string     `json:"realname"`
	Ignored  flickr.Int `json:"ignored"`
	Friend   flickr.Int `json:"friend"`
	Family   flickr.Int `json:"family"`
}

func (Contacts) Name() string      { return "contacts" }
func (Contacts) Lane() domain.Lane { return domain.LanePerson }

func (Contacts) Plan(_ context.Context, p domain.Person) []domain.WorkSpec {
	if p.Depth != 0 || p.NSID == "" {
		return nil
	}
	return []domain.WorkSpec{{
		Subject: p.NSID,
		Method:  "flickr.contacts.getList",
		Params:  map[string]string{"user_id": p.NSID, "per_page": catalogPageSize},
	}}
}

func (Contacts) Handle(_ context.Context, p domain.Page) (domain.Result, error) {
	items, pages, err := decodeContactsPage(p)
	if err != nil {
		return domain.Result{}, err
	}
	out := domain.Result{
		People:      make([]catalog.Person, 0, len(items)),
		Contacts:    make([]catalog.Contact, 0, len(items)),
		Discoveries: make([]domain.Discovery, 0, len(items)),
	}
	seen := make(map[string]struct{}, len(items))
	seenAt := time.Time{}
	if p.Item.Cursor.CheckpointAt > 0 {
		seenAt = time.Unix(p.Item.Cursor.CheckpointAt, 0).UTC()
	}
	for _, item := range items {
		if item.NSID == "" {
			return domain.Result{}, fmt.Errorf("crawl facets: contacts record has no nsid")
		}
		if item.NSID == p.Item.Subject {
			continue
		}
		if _, ok := seen[item.NSID]; ok {
			continue
		}
		seen[item.NSID] = struct{}{}
		username := item.Username
		if username == "" {
			username = item.NSID
		}
		out.People = append(out.People, catalog.Person{NSID: item.NSID, Username: username, Realname: item.Realname})
		out.Contacts = append(out.Contacts, catalog.Contact{PersonNSID: p.Item.Subject, ContactNSID: item.NSID, SeenAt: seenAt})
		out.Discoveries = append(out.Discoveries, domain.Discovery{NSID: item.NSID, Via: "contact"})
	}
	out.HasMore = p.Number < pages
	return out, nil
}

func decodeContactsPage(p domain.Page) ([]contactRecord, int, error) {
	var response contactsResponse
	if err := json.Unmarshal(p.Payload, &response); err != nil {
		return nil, 0, fmt.Errorf("crawl facets: decode contacts: %w", err)
	}
	if response.Contacts == nil || response.Contacts.Pages == nil || response.Contacts.Items == nil {
		return nil, 0, fmt.Errorf("crawl facets: invalid contacts pagination metadata")
	}
	pages := *response.Contacts.Pages
	if pages < 1 || p.Number < 1 || p.Number > pages {
		return nil, 0, fmt.Errorf("crawl facets: invalid contacts pagination metadata")
	}
	items := *response.Contacts.Items
	if p.Number < pages && len(items) == 0 {
		return nil, 0, fmt.Errorf("crawl facets: empty nonfinal contacts page")
	}
	return items, pages, nil
}

var _ domain.Facet = Contacts{}
