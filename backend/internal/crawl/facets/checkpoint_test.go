package facets

import (
	"testing"

	"github.com/jooservices/xflickr/internal/crawl/domain"
)

func TestFacetCheckpointFiltersMatchDomain(t *testing.T) {
	checkpoint := domain.ScanCheckpoint(1_700_000_000)
	if got, want := PhotosCheckpointFilter(checkpoint), domain.PhotosCheckpointFilter(checkpoint); got[ParamMinUploadDate] != want[ParamMinUploadDate] {
		t.Fatalf("photos filter = %#v, want %#v", got, want)
	}
	if got, want := FavoritesCheckpointFilter(checkpoint), domain.FavoritesCheckpointFilter(checkpoint); got[ParamMinFaveDate] != want[ParamMinFaveDate] {
		t.Fatalf("favorites filter = %#v, want %#v", got, want)
	}
}
