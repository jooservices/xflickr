package facets

import (
	"context"
	"encoding/json"
	"fmt"

	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/crawl/domain"
	"github.com/jooservices/xflickr/pkg/flickr"
)

// Contacts enumerates the authenticated account's contact list. Only depth-0
// subjects (connected accounts) plan this work; deeper people never call
// contacts.getList because that method always returns the signed-in member's
// own contacts.
type Contacts struct{}

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
	var e struct {
		Contacts struct {
			Pages int `json:"pages"`
			Items []struct {
				NSID     string     `json:"nsid"`
				Username string     `json:"username"`
				Realname string     `json:"realname"`
				Ignored  flickr.Int `json:"ignored"`
				Friend   flickr.Int `json:"friend"`
				Family   flickr.Int `json:"family"`
			} `json:"contact"`
		} `json:"contacts"`
	}
	if err := json.Unmarshal(p.Payload, &e); err != nil {
		return domain.Result{}, fmt.Errorf("crawl facets: decode contacts: %w", err)
	}
	out := domain.Result{
		People:      make([]catalog.Person, 0, len(e.Contacts.Items)),
		Contacts:    make([]catalog.Contact, 0, len(e.Contacts.Items)),
		Discoveries: make([]domain.Discovery, 0, len(e.Contacts.Items)),
	}
	seen := make(map[string]struct{}, len(e.Contacts.Items))
	for _, item := range e.Contacts.Items {
		if item.NSID == "" || item.NSID == p.Item.Subject {
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
		out.Contacts = append(out.Contacts, catalog.Contact{PersonNSID: p.Item.Subject, ContactNSID: item.NSID})
		out.Discoveries = append(out.Discoveries, domain.Discovery{NSID: item.NSID, Via: "contact"})
	}
	out.HasMore = p.Number < e.Contacts.Pages && len(e.Contacts.Items) > 0
	return out, nil
}

var _ domain.Facet = Contacts{}
