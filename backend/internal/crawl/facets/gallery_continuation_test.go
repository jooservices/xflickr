package facets

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jooservices/xflickr/internal/crawl/domain"
)

func galleryContinuationPages(t *testing.T) []json.RawMessage {
	return galleryFixturePages(t, "galleries.getList.continuation.json")
}

func galleryFixturePages(t *testing.T, name string) []json.RawMessage {
	t.Helper()
	path := filepath.Join("../../../pkg/flickr/testdata", name)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read gallery continuation fixture: %v", err)
	}
	var pages []json.RawMessage
	if err := json.Unmarshal(contents, &pages); err != nil {
		t.Fatalf("decode gallery continuation fixture: %v", err)
	}
	return pages
}

func TestGalleriesFollowsContinuationUntilTerminal(t *testing.T) {
	pages := galleryContinuationPages(t)
	if len(pages) != 3 {
		t.Fatalf("fixture pages = %d, want 3", len(pages))
	}
	cursor := domain.Cursor{Page: 1, Method: "flickr.galleries.getList", Continuation: "0"}
	wantCursors := []string{"opaque:page/2?token=a", "opaque:page/3?token=b", "-1"}
	wantGalleryIDs := []string{"gallery-1", "gallery-2", "gallery-3"}
	for index, payload := range pages {
		result, err := (Galleries{}).Handle(t.Context(), domain.Page{
			Item: domain.Item{Subject: "owner@N01", Cursor: cursor}, Number: index + 1, Payload: payload,
		})
		if err != nil {
			t.Fatalf("page %d: %v", index+1, err)
		}
		if result.NextCursor != wantCursors[index] || result.HasMore != (index < len(pages)-1) {
			t.Fatalf("page %d result cursor=%q more=%v", index+1, result.NextCursor, result.HasMore)
		}
		if len(result.Galleries) != 1 || result.Galleries[0].ID != wantGalleryIDs[index] {
			t.Fatalf("page %d galleries = %#v", index+1, result.Galleries)
		}
		if result.HasMore {
			cursor = cursor.Next().WithContinuation(result.NextCursor)
		}
	}
}

func TestGalleriesLegacyResponseUsesPageCounter(t *testing.T) {
	pages := galleryFixturePages(t, "galleries.getList.legacy.json")
	if len(pages) != 2 {
		t.Fatalf("legacy fixture pages = %d, want 2", len(pages))
	}
	for index, payload := range pages {
		result, err := (Galleries{}).Handle(t.Context(), domain.Page{
			Item: domain.Item{Subject: "owner@N01", Cursor: domain.Cursor{Page: index + 1}}, Number: index + 1, Payload: payload,
		})
		if err != nil || result.NextCursor != "" || result.HasMore != (index == 0) {
			t.Fatalf("legacy page %d = %#v, %v", index+1, result, err)
		}
		if len(result.Galleries) != 1 || result.Galleries[0].ID != []string{"legacy-gallery-1", "legacy-gallery-2"}[index] {
			t.Fatalf("legacy page %d galleries = %#v", index+1, result.Galleries)
		}
	}
}

func TestGalleriesStopsOnRepeatedCursor(t *testing.T) {
	page := catalogPage("owner@N01", `{"galleries":{"continuation":"opaque-current","gallery":[{"id":"g1"}]}}`, 2)
	page.Item.Cursor.Continuation = "opaque-current"
	result, err := (Galleries{}).Handle(t.Context(), page)
	if err != nil || result.HasMore || result.NextCursor != "opaque-current" || result.CursorWarning == "" {
		t.Fatalf("repeated cursor result = %#v, %v", result, err)
	}
}

func TestGalleriesStopsOnEmptyPageWithCursor(t *testing.T) {
	page := catalogPage("owner@N01", `{"galleries":{"continuation":"opaque-next","gallery":[]}}`, 2)
	result, err := (Galleries{}).Handle(t.Context(), page)
	if err != nil || result.HasMore || result.NextCursor != "opaque-next" || result.CursorWarning == "" {
		t.Fatalf("empty page result = %#v, %v", result, err)
	}
}
