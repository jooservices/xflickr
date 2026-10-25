package facets

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/crawl/domain"
	"github.com/jooservices/xflickr/pkg/flickr"
)

func TestFacetPlansRequest500PhotosAndAllURLSizes(t *testing.T) {
	for _, facet := range []domain.Facet{Photos{}, Favorites{}} {
		specs := facet.Plan(t.Context(), domain.Person{NSID: "owner@N01"})
		if len(specs) != 1 || specs[0].Subject != "owner@N01" || specs[0].Params["per_page"] != "500" {
			t.Fatalf("%s plan = %#v", facet.Name(), specs)
		}
		if specs[0].Method == "" || specs[0].Params["user_id"] != "owner@N01" {
			t.Fatalf("%s request identity = %#v", facet.Name(), specs[0])
		}
		for _, size := range []string{"url_sq", "url_t", "url_s", "url_q", "url_m", "url_n", "url_z", "url_c", "url_l", "url_o"} {
			if !strings.Contains(specs[0].Params["extras"], size) {
				t.Errorf("%s extras missing %s", facet.Name(), size)
			}
		}
	}
	if (Photos{}).Plan(t.Context(), domain.Person{NSID: "p"})[0].Method != "flickr.people.getPhotos" {
		t.Fatal("photos plan uses the wrong Flickr method")
	}
	if (Favorites{}).Plan(t.Context(), domain.Person{NSID: "p"})[0].Method != "flickr.favorites.getList" {
		t.Fatal("favorites plan uses the wrong Flickr method")
	}
}

func TestPhotosFacetStoresPageWithProvisionalSizes(t *testing.T) {
	result, err := (Photos{}).Handle(t.Context(), domain.Page{Number: 1, Payload: []byte(pagePayload(2, 1, 1))})
	if err != nil {
		t.Fatal(err)
	}
	if !result.HasMore || len(result.Photos) != 1 || len(result.People) != 1 || len(result.Discoveries) != 0 {
		t.Fatalf("photos result = %#v", result)
	}
	photo := result.Photos[0]
	if photo.ID != 100 || photo.OwnerNSID != "owner-0@N01" || photo.SizesStatus != catalog.SizesProvisional {
		t.Fatalf("photo metadata = %#v", photo)
	}
	var sizes []struct {
		Source string `json:"source"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal(photo.Sizes, &sizes); err != nil {
		t.Fatal(err)
	}
	if len(sizes) != 10 || sizes[0].Source != "url_sq" || sizes[9].Source != "url_o" {
		t.Fatalf("provisional sizes = %s", photo.Sizes)
	}
}

func TestFavoritesFacetStoresPhotosAndUniqueOwnerDiscoveries(t *testing.T) {
	result, err := (Favorites{}).Handle(t.Context(), domain.Page{Number: 2, Payload: []byte(pagePayload(2, 2, 3))})
	if err != nil {
		t.Fatal(err)
	}
	if result.HasMore || len(result.Photos) != 3 || len(result.People) != 2 || len(result.Discoveries) != 2 {
		t.Fatalf("favorites result sizes = photos:%d people:%d discoveries:%d more:%v", len(result.Photos), len(result.People), len(result.Discoveries), result.HasMore)
	}
	if result.Discoveries[0].Via != "favorite" || result.Discoveries[1].Via != "favorite" {
		t.Fatalf("discovery provenance = %#v", result.Discoveries)
	}
	if result.Photos[1].Visibility != catalog.VisibilityFriends || result.Photos[2].Visibility != catalog.VisibilityFamily {
		t.Fatalf("visibility mapping = %q, %q", result.Photos[1].Visibility, result.Photos[2].Visibility)
	}
}

func TestFacetHandlesMalformedPayloadAndInvalidPhoto(t *testing.T) {
	for _, facet := range []domain.Facet{Photos{}, Favorites{}} {
		if _, err := facet.Handle(t.Context(), domain.Page{Payload: []byte("{")}); err == nil {
			t.Errorf("%s accepted malformed JSON", facet.Name())
		}
		payload := []byte(`{"photos":{"pages":1,"photo":[{"id":"bad","owner":"owner","lastupdate":"1"}]}}`)
		if _, err := facet.Handle(t.Context(), domain.Page{Number: 1, Payload: payload}); err == nil {
			t.Errorf("%s accepted an invalid photo", facet.Name())
		}
	}
	if _, err := toCatalogPhoto(flickr.Photo{ID: "102", Owner: "owner@N01"}, []byte(`{}`)); err == nil {
		t.Fatal("photo without a usable timestamp was accepted")
	}
}

func TestFavoritesFacetProcesses500PhotoPages(t *testing.T) {
	for page := 1; page <= 2; page++ {
		result, err := (Favorites{}).Handle(t.Context(), domain.Page{Number: page, Payload: []byte(pagePayload(2, page, 500))})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Photos) != 500 || len(result.Discoveries) != 2 || result.HasMore != (page == 1) {
			t.Fatalf("favorites page %d = photos:%d discoveries:%d more:%v", page, len(result.Photos), len(result.Discoveries), result.HasMore)
		}
	}
}

func TestPhotosFacetProcesses1000PhotosAcross500ItemPages(t *testing.T) {
	for page := 1; page <= 2; page++ {
		result, err := (Photos{}).Handle(t.Context(), domain.Page{Number: page, Payload: []byte(pagePayload(2, page, 500))})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Photos) != 500 || result.HasMore != (page == 1) {
			t.Fatalf("photos page %d = photos:%d more:%v", page, len(result.Photos), result.HasMore)
		}
	}
}

// pagePayload generates Flickr-shaped pages without coupling tests to the API
// fixtures' deliberately small one-photo samples.
func pagePayload(pages, page, count int) string {
	items := make([]string, count)
	for i := range items {
		owner := fmt.Sprintf("owner-%d@N01", i%2)
		public, friend, family := 1, 0, 0
		switch i % 3 {
		case 1:
			public, friend = 0, 1
		case 2:
			public, family = 0, 1
		}
		items[i] = fmt.Sprintf(`{"id":"%d","owner":%q,"ownername":"%s","secret":"secret","server":"1","title":"title","ispublic":%d,"isfriend":%d,"isfamily":%d,"dateupload":"1700000000","lastupdate":"1700000100","tags":"one two","license":"0","media":"photo","description":{"_content":"desc"},"url_sq":"sq","url_t":"t","url_s":"s","url_q":"q","url_m":"m","url_n":"n","url_z":"z","url_c":"c","url_l":"l","url_o":"o"}`, 100+i, owner, owner, public, friend, family)
	}
	return fmt.Sprintf(`{"photos":{"page":%d,"pages":%d,"perpage":500,"photo":[%s]}}`, page, pages, strings.Join(items, ","))
}
