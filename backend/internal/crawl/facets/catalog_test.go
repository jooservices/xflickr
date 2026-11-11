package facets

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jooservices/xflickr/internal/crawl/domain"
)

func catalogPage(subject, payload string, number int) domain.Page {
	return domain.Page{Number: number, Item: domain.Item{Subject: subject}, Payload: []byte(payload)}
}

func TestCatalogFacetPlansUseDocumentedMethods(t *testing.T) {
	want := map[string]string{"albums": "flickr.photosets.getList", "galleries": "flickr.galleries.getList", "groups": "flickr.people.getGroups"}
	for _, facet := range []domain.Facet{Albums{}, Galleries{}, Groups{}} {
		specs := facet.Plan(t.Context(), domain.Person{NSID: "p@N01"})
		if len(specs) != 1 || specs[0].Method != want[facet.Name()] || specs[0].Params["user_id"] != "p@N01" || specs[0].Params["per_page"] != "500" || specs[0].Subject != "p@N01" {
			t.Fatalf("%s plan = %#v", facet.Name(), specs)
		}
		if facet.Lane() != domain.LanePerson {
			t.Fatalf("%s lane = %v", facet.Name(), facet.Lane())
		}
		if facet.Name() == "galleries" && specs[0].Params["continuation"] != "0" {
			t.Fatalf("gallery first continuation = %q, want 0", specs[0].Params["continuation"])
		}
	}
}

func TestAlbumsMapsResponseAndPaginates(t *testing.T) {
	payload := `{"photosets":{"pages":2,"photoset":[{"id":"a1","primary":"55","photos":3,"title":{"_content":"T"},"description":{"_content":"D"},"date_create":"1700000000","date_update":"1700000100"},{"id":"a2","primary":"","photos":0,"title":{"_content":"U"}}]}}`
	result, err := (Albums{}).Handle(t.Context(), catalogPage("p@N01", payload, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.People) != 0 {
		t.Fatalf("albums must not emit placeholder people: %#v", result.People)
	}
	if !result.HasMore || len(result.Albums) != 2 {
		t.Fatalf("albums result = %#v", result)
	}
	a := result.Albums[0]
	if a.ID != "a1" || a.OwnerNSID != "p@N01" || a.Title != "T" || a.Description != "D" || a.PrimaryPhotoID == nil || *a.PrimaryPhotoID != 55 || a.PhotosCount != 3 || a.CreatedAt.Unix() != 1700000000 || a.UpdatedAt.Unix() != 1700000100 {
		t.Fatalf("album = %#v", a)
	}
	if result.Albums[1].PrimaryPhotoID != nil || !result.Albums[1].CreatedAt.IsZero() {
		t.Fatalf("sparse album = %#v", result.Albums[1])
	}
	last, err := (Albums{}).Handle(t.Context(), catalogPage("p@N01", payload, 2))
	if err != nil || last.HasMore {
		t.Fatalf("last page = %#v, %v", last, err)
	}
}

func TestGalleriesMapsResponseOwnersAndPaginates(t *testing.T) {
	payload := `{"galleries":{"pages":2,"gallery":[{"id":"g1","owner":"other@N02","primary_photo_id":"9","count_photos":4,"title":{"content":"G"}},{"id":"g2","title":{"_content":"Own"}}]}}`
	result, err := (Galleries{}).Handle(t.Context(), catalogPage("p@N01", payload, 1))
	if err != nil {
		t.Fatal(err)
	}
	if !result.HasMore || len(result.Galleries) != 2 {
		t.Fatalf("galleries result = %#v", result)
	}
	if g := result.Galleries[0]; g.OwnerNSID != "other@N02" || g.Title != "G" || g.PrimaryPhotoID == nil || *g.PrimaryPhotoID != 9 || g.PhotosCount != 4 {
		t.Fatalf("gallery = %#v", g)
	}
	if result.Galleries[1].OwnerNSID != "p@N01" {
		t.Fatalf("owner default = %#v", result.Galleries[1])
	}
	if len(result.People) != 1 || result.People[0].NSID != "other@N02" {
		t.Fatalf("only the foreign owner needs a row: %#v", result.People)
	}
}

func TestGroupsMapsResponseAndMemberships(t *testing.T) {
	payload := `{"groups":{"pages":2,"group":[{"nsid":" grp1@N00 ","name":" One ","members":5,"pool_count":6,"description":"d"},{"nsid":"grp2@N00","name":"Two"}]}}`
	result, err := (Groups{}).Handle(t.Context(), catalogPage("p@N01", payload, 1))
	if err != nil {
		t.Fatal(err)
	}
	if !result.HasMore || len(result.People) != 0 || len(result.Groups) != 2 || len(result.PersonGroups) != 2 {
		t.Fatalf("groups result = %#v", result)
	}
	if g := result.Groups[0]; g.NSID != " grp1@N00 " || g.Name != " One " || g.MembersCount != 5 || g.PoolCount != 6 || g.Description != "d" {
		t.Fatalf("group = %#v", g)
	}
	if pg := result.PersonGroups[0]; pg.PersonNSID != "p@N01" || pg.GroupNSID != " grp1@N00 " {
		t.Fatalf("membership = %#v", pg)
	}
	last, err := (Groups{}).Handle(t.Context(), catalogPage("p@N01", payload, 2))
	if err != nil || last.HasMore {
		t.Fatalf("last group page = %#v, %v", last, err)
	}
}

func TestGroupsRejectsMissingEnvelope(t *testing.T) {
	cases := map[string]string{
		"malformed JSON":          `{`,
		"missing groups":          `{}`,
		"null groups":             `{"groups":null}`,
		"wrong groups type":       `{"groups":[]}`,
		"wrong group list type":   `{"groups":{"group":"not a list"}}`,
		"null group list":         `{"groups":{"group":null}}`,
		"wrong member field type": `{"groups":{"group":[{"nsid":42,"name":"Group"}]}}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			result, err := (Groups{}).Handle(t.Context(), catalogPage("p@N01", payload, 1))
			if err == nil {
				t.Fatal("malformed groups response was accepted")
			}
			if !strings.HasPrefix(err.Error(), "crawl facets: decode groups:") {
				t.Fatalf("error = %q, want groups decode context", err)
			}
			if !reflect.DeepEqual(result, domain.Result{}) {
				t.Fatalf("error result = %#v, want zero result", result)
			}
		})
	}
}

func TestGroupsRejectsInvalidMember(t *testing.T) {
	cases := map[string]string{
		"missing nsid":              `{"groups":{"group":[{"name":"Group"}]}}`,
		"blank nsid":                `{"groups":{"group":[{"nsid":" \t ","name":"Group"}]}}`,
		"missing name":              `{"groups":{"group":[{"nsid":"g@N00"}]}}`,
		"blank name":                `{"groups":{"group":[{"nsid":"g@N00","name":" \n "}]}}`,
		"valid then invalid member": `{"groups":{"group":[{"nsid":"valid@N00","name":"Valid"},{"nsid":"invalid@N00","name":"  "}]}}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			result, err := (Groups{}).Handle(t.Context(), catalogPage("p@N01", payload, 1))
			if err == nil {
				t.Fatal("invalid group member was accepted")
			}
			if !strings.HasPrefix(err.Error(), "crawl facets: decode groups:") {
				t.Fatalf("error = %q, want groups decode context", err)
			}
			if !reflect.DeepEqual(result, domain.Result{}) {
				t.Fatalf("error result = %#v, want zero result", result)
			}
		})
	}
}

func TestGroupsAcceptsEmptyEnvelope(t *testing.T) {
	for name, payload := range map[string]string{
		"empty group list":    `{"groups":{"group":[]}}`,
		"empty groups object": `{"groups":{}}`,
	} {
		t.Run(name, func(t *testing.T) {
			result, err := (Groups{}).Handle(t.Context(), catalogPage("p@N01", payload, 1))
			if err != nil {
				t.Fatalf("empty response: %v", err)
			}
			if result.HasMore || len(result.Groups) != 0 || len(result.PersonGroups) != 0 {
				t.Fatalf("empty response produced groups or memberships: %#v", result)
			}
		})
	}
}

func TestGroupsRejectsEmptyPageWhenMorePagesRemain(t *testing.T) {
	for name, payload := range map[string]string{
		"empty group list":   `{"groups":{"pages":2,"group":[]}}`,
		"omitted group list": `{"groups":{"pages":2}}`,
	} {
		t.Run(name, func(t *testing.T) {
			result, err := (Groups{}).Handle(t.Context(), catalogPage("p@N01", payload, 1))
			if err == nil {
				t.Fatal("empty intermediate page was accepted")
			}
			if !strings.HasPrefix(err.Error(), "crawl facets: decode groups:") {
				t.Fatalf("error = %q, want groups decode context", err)
			}
			if !reflect.DeepEqual(result, domain.Result{}) {
				t.Fatalf("error result = %#v, want zero result", result)
			}
		})
	}
}

func TestGroupPoolFacetStoresPhotosOnlyForExplicitWork(t *testing.T) {
	facet := GroupPool{}
	if facet.Lane() != domain.LanePerson || len(facet.Plan(t.Context(), domain.Person{NSID: "group@N00", UserID: 7})) != 0 {
		t.Fatal("group pool crawl must require an explicit per-group work item")
	}
	page := domain.Page{
		Number:  1,
		Item:    domain.Item{Kind: "group_pool", Subject: "group@N00"},
		Payload: []byte(`{"photos":{"pages":2,"photo":[{"id":"123","owner":"owner@N00","ownername":"Owner","date_upload":"1700000000","lastupdate":"1700000100","ispublic":1}]}}`),
	}
	result, err := facet.Handle(t.Context(), page)
	if err != nil || !result.HasMore || len(result.Photos) != 1 || len(result.People) != 1 || len(result.GroupPoolPhotos) != 1 {
		t.Fatalf("group pool page = %+v err=%v", result, err)
	}
	if result.Photos[0].ID != 123 || result.Photos[0].OwnerNSID != "owner@N00" || result.GroupPoolPhotos[0].GroupNSID != "group@N00" || result.GroupPoolPhotos[0].PhotoID != 123 {
		t.Fatalf("group pool records = photos:%+v pool:%+v", result.Photos, result.GroupPoolPhotos)
	}
}

func TestCatalogFacetsHandleEmptyAndMalformedReplies(t *testing.T) {
	cases := []struct {
		facet domain.Facet
		empty string
	}{
		{Albums{}, `{"photosets":{"pages":1,"photoset":[]}}`},
		{Galleries{}, `{"galleries":{"pages":1,"gallery":[]}}`},
		{GroupPool{}, `{"photos":{"pages":1,"photo":[]}}`},
	}
	for _, tc := range cases {
		result, err := tc.facet.Handle(t.Context(), catalogPage("p@N01", tc.empty, 1))
		if err != nil || result.HasMore || len(result.Albums)+len(result.Galleries)+len(result.Groups)+len(result.PersonGroups)+len(result.People) != 0 {
			t.Fatalf("%s empty = %#v, %v", tc.facet.Name(), result, err)
		}
		if _, err := tc.facet.Handle(t.Context(), catalogPage("p@N01", `{`, 1)); err == nil {
			t.Fatalf("%s accepted malformed JSON", tc.facet.Name())
		}
		if _, err := tc.facet.Handle(t.Context(), catalogPage("p@N01", `{}`, 1)); err != nil {
			t.Fatalf("%s rejected an empty object: %v", tc.facet.Name(), err)
		}
	}
}

func TestCatalogFacetsAcceptFlickrStringCounts(t *testing.T) {
	cases := []struct {
		name    string
		facet   domain.Facet
		payload string
	}{
		{"albums", Albums{}, `{"photosets":{"pages":1,"photoset":[{"id":"a","photos":"3"}]}}`},
		{"galleries", Galleries{}, `{"galleries":{"pages":1,"gallery":[{"id":"g","count_photos":"4"}]}}`},
		{"groups", Groups{}, `{"groups":{"group":[{"nsid":"g@N00","name":"Group","members":"150","pool_count":"2500"}]}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tc.facet.Handle(t.Context(), catalogPage("p@N01", tc.payload, 1))
			if err != nil {
				t.Fatal(err)
			}
			if result.HasMore {
				t.Fatal("single response must not request another page")
			}
			switch tc.name {
			case "albums":
				if len(result.Albums) != 1 || result.Albums[0].PhotosCount != 3 {
					t.Fatalf("albums = %#v", result.Albums)
				}
			case "galleries":
				if len(result.Galleries) != 1 || result.Galleries[0].PhotosCount != 4 {
					t.Fatalf("galleries = %#v", result.Galleries)
				}
			case "groups":
				if len(result.Groups) != 1 || result.Groups[0].MembersCount != 150 || result.Groups[0].PoolCount != 2500 || len(result.PersonGroups) != 1 {
					t.Fatalf("groups = %#v, memberships = %#v", result.Groups, result.PersonGroups)
				}
			}
		})
	}
}
