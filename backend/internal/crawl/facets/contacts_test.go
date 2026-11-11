package facets

import (
	"testing"

	"github.com/jooservices/xflickr/internal/crawl/domain"
)

func TestContactsFacetPlanAndHandle(t *testing.T) {
	facet := Contacts{}
	if facet.Name() != "contacts" || facet.Lane() != domain.LanePerson {
		t.Fatalf("metadata = %q %v", facet.Name(), facet.Lane())
	}
	if specs := facet.Plan(t.Context(), domain.Person{NSID: "acct@N00", Depth: 0}); len(specs) != 1 ||
		specs[0].Method != "flickr.contacts.getList" || specs[0].Subject != "acct@N00" ||
		specs[0].Params["user_id"] != "acct@N00" || specs[0].Params["per_page"] != "500" {
		t.Fatalf("depth-0 plan = %#v", specs)
	}
	if specs := facet.Plan(t.Context(), domain.Person{NSID: "other@N01", Depth: 1}); len(specs) != 0 {
		t.Fatalf("deeper plan must be empty: %#v", specs)
	}
	if specs := facet.Plan(t.Context(), domain.Person{Depth: 0}); len(specs) != 0 {
		t.Fatalf("empty nsid plan = %#v", specs)
	}

	payload := `{"contacts":{"pages":2,"contact":[
		{"nsid":"c1@N00","username":"one","realname":"One","ignored":"0","friend":"1","family":"0"},
		{"nsid":"c2@N00","username":"two","friend":1,"family":0},
		{"nsid":"c3@N00","username":"three","ignored":0,"friend":"0","family":"1"},
		{"nsid":"acct@N00","username":"self"},
		{"nsid":"c1@N00","username":"dup"}
	]}}`
	result, err := facet.Handle(t.Context(), catalogPage("acct@N00", payload, 1))
	if err != nil {
		t.Fatal(err)
	}
	if !result.HasMore || len(result.People) != 3 || len(result.Contacts) != 3 || len(result.Discoveries) != 3 {
		t.Fatalf("result sizes = people:%d contacts:%d discoveries:%d more:%v", len(result.People), len(result.Contacts), len(result.Discoveries), result.HasMore)
	}
	if result.People[0].NSID != "c1@N00" || result.People[0].Username != "one" || result.People[0].Realname != "One" {
		t.Fatalf("first person = %#v", result.People[0])
	}
	if c := result.Contacts[1]; c.PersonNSID != "acct@N00" || c.ContactNSID != "c2@N00" {
		t.Fatalf("contact = %#v", c)
	}
	if result.Discoveries[2].NSID != "c3@N00" || result.Discoveries[2].Via != "contact" {
		t.Fatalf("discovery = %#v", result.Discoveries[2])
	}
	last, err := facet.Handle(t.Context(), catalogPage("acct@N00", payload, 2))
	if err != nil || last.HasMore {
		t.Fatalf("last page = %#v, %v", last, err)
	}

	empty, err := facet.Handle(t.Context(), catalogPage("acct@N00", `{"contacts":{"pages":1,"contact":[]}}`, 1))
	if err != nil || empty.HasMore || len(empty.People)+len(empty.Contacts)+len(empty.Discoveries) != 0 {
		t.Fatalf("empty = %#v, %v", empty, err)
	}

	for _, malformed := range []struct {
		name    string
		payload string
		number  int
	}{
		{name: "missing contacts", payload: `{}`},
		{name: "missing contacts list", payload: `{"contacts":{"pages":1}}`},
		{name: "missing pages", payload: `{"contacts":{"contact":[]}}`},
		{name: "zero pages", payload: `{"contacts":{"pages":0,"contact":[]}}`},
		{name: "empty nonfinal page", payload: `{"contacts":{"pages":2,"contact":[]}}`},
		{name: "contact missing nsid", payload: `{"contacts":{"pages":1,"contact":[{"username":"missing-id"}]}}`},
		{name: "page beyond total", payload: `{"contacts":{"pages":1,"contact":[]}}`, number: 2},
	} {
		t.Run(malformed.name, func(t *testing.T) {
			number := malformed.number
			if number == 0 {
				number = 1
			}
			if _, err := facet.Handle(t.Context(), catalogPage("acct@N00", malformed.payload, number)); err == nil {
				t.Fatal("invalid contacts page was accepted")
			}
		})
	}
	if _, err := facet.Handle(t.Context(), catalogPage("acct@N00", `{`, 1)); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestContactsRegistersDepthOneInterests(t *testing.T) {
	result, err := (Contacts{}).Handle(t.Context(), catalogPage("acct@N00", `{"contacts":{"pages":1,"contact":[{"nsid":"c@N00","username":"c"}]}}`, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Discoveries) != 1 || result.Discoveries[0].Via != "contact" || result.Discoveries[0].NSID != "c@N00" {
		t.Fatalf("discoveries = %#v", result.Discoveries)
	}
	// Engine discovery uses item.Depth+1; a depth-0 contacts page therefore
	// registers interests at depth 1 with via=contact (proven in engine tests).
	if (Contacts{}).Plan(t.Context(), domain.Person{NSID: "acct@N00", Depth: 0})[0].Subject != "acct@N00" {
		t.Fatal("connected account is the work subject")
	}
}

func TestContactsSkipsGoneEntities(t *testing.T) {
	// Gone accounts are addressed by user_id on contacts.getList so the engine
	// can suppress them before any Flickr call (see crawl/app entity state).
	specs := (Contacts{}).Plan(t.Context(), domain.Person{NSID: "gone@N00", Depth: 0})
	if len(specs) != 1 || specs[0].Params["user_id"] != "gone@N00" {
		t.Fatalf("plan must address the account for entity-state suppression: %#v", specs)
	}
}
