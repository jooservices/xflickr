package postgres

import (
	"testing"

	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres/gen"
)

func TestOrderPhotoParamsSortsByIDAndKeepsTheLastDuplicate(t *testing.T) {
	input := []gen.UpsertPhotoParams{
		{ID: 30, Title: "c"}, {ID: 10, Title: "first"}, {ID: 20, Title: "b"}, {ID: 10, Title: "last"},
	}
	got := orderPhotoParams(input)
	if len(got) != 3 || got[0].ID != 10 || got[1].ID != 20 || got[2].ID != 30 {
		t.Fatalf("ordered ids = %+v", got)
	}
	if got[0].Title != "last" {
		t.Fatalf("duplicate id kept %q, want the last payload", got[0].Title)
	}
	if input[0].ID != 30 {
		t.Fatal("input slice was reordered")
	}
}
