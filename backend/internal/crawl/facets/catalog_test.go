package facets

import (
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
	payload := `{"groups":{"pages":1,"group":[{"nsid":"grp1@N00","name":"One","members":5,"pool_count":6,"description":"d"},{"nsid":"grp2@N00","name":"Two"}]}}`
	result, err := (Groups{}).Handle(t.Context(), catalogPage("p@N01", payload, 1))
	if err != nil {
		t.Fatal(err)
	}
	if result.HasMore || len(result.People) != 0 || len(result.Groups) != 2 || len(result.PersonGroups) != 2 {
		t.Fatalf("groups result = %#v", result)
	}
	if g := result.Groups[0]; g.NSID != "grp1@N00" || g.Name != "One" || g.MembersCount != 5 || g.PoolCount != 6 || g.Description != "d" {
		t.Fatalf("group = %#v", g)
	}
	if pg := result.PersonGroups[1]; pg.PersonNSID != "p@N01" || pg.GroupNSID != "grp2@N00" {
		t.Fatalf("membership = %#v", pg)
	}
}

func TestCatalogFacetsHandleEmptyAndMalformedReplies(t *testing.T) {
	cases := []struct {
		facet domain.Facet
		empty string
	}{
		{Albums{}, `{"photosets":{"pages":1,"photoset":[]}}`},
		{Galleries{}, `{"galleries":{"pages":1,"gallery":[]}}`},
		{Groups{}, `{"groups":{"pages":1,"group":[]}}`},
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
