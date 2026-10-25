package facets

import (
	"context"
	"strconv"

	"github.com/jooservices/xflickr/internal/crawl/domain"
)

// Favorites reads a person's favorites, storing each photo and discovering its
// owner for the crawl engine's per-user depth gate.
type Favorites struct{}

func (Favorites) Name() string      { return "favorites" }
func (Favorites) Lane() domain.Lane { return domain.LanePerson }

func (Favorites) Plan(_ context.Context, person domain.Person) []domain.WorkSpec {
	return []domain.WorkSpec{{
		Subject: person.NSID,
		Method:  "flickr.favorites.getList",
		Params: map[string]string{
			"user_id":  person.NSID,
			"per_page": strconv.Itoa(photosPerPage),
			"extras":   photoExtras,
		},
	}}
}

func (Favorites) Handle(_ context.Context, page domain.Page) (domain.Result, error) {
	return parsePhotoPage(page, true)
}

var _ domain.Facet = Favorites{}
