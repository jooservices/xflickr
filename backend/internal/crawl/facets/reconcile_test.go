package facets

import (
	"fmt"
	"testing"

	"github.com/jooservices/xflickr/internal/crawl/domain"
)

func TestReconcileReadsBothCompleteListsBeforeApplyingDiff(t *testing.T) {
	checkpoint := int64(1793000000)
	photoCursor := domain.Cursor{Page: 1, Method: "flickr.people.getPhotos", CheckpointAt: checkpoint, Params: map[string]string{"user_id": "owner"}}
	photoItem := domain.Item{Kind: "reconcile", Subject: "owner", Cursor: photoCursor}

	firstPhotos, err := (Reconcile{}).Handle(t.Context(), domain.Page{Item: photoItem, Number: 1, Payload: []byte(reconcilePayload(2, 1, `{"id":"100","owner":"owner","dateupload":"1700000000","lastupdate":"1700000100","ispublic":1}`))})
	if err != nil || !firstPhotos.HasMore || firstPhotos.NextCursor != nil || firstPhotos.CompleteReconcile {
		t.Fatalf("first photo page = %#v, err=%v", firstPhotos, err)
	}

	photoItem.Cursor.Page = 2
	lastPhotos, err := (Reconcile{}).Handle(t.Context(), domain.Page{Item: photoItem, Number: 2, Payload: []byte(reconcilePayload(2, 2, `{"id":"101","owner":"owner","dateupload":"1700000000","lastupdate":"1700000100","ispublic":1}`))})
	if err != nil || lastPhotos.HasMore || lastPhotos.CompleteReconcile || lastPhotos.NextCursor == nil {
		t.Fatalf("last photo page = %#v, err=%v", lastPhotos, err)
	}
	if lastPhotos.NextCursor.Method != "flickr.favorites.getList" || lastPhotos.NextCursor.Page != 1 || lastPhotos.NextCursor.CheckpointAt != checkpoint || lastPhotos.NextCursor.Params["user_id"] != "owner" || lastPhotos.NextCursor.Params["per_page"] != "500" {
		t.Fatalf("favorites cursor = %#v", lastPhotos.NextCursor)
	}

	favoriteItem := photoItem
	favoriteItem.Cursor = *lastPhotos.NextCursor
	firstFavorites, err := (Reconcile{}).Handle(t.Context(), domain.Page{Item: favoriteItem, Number: 1, Payload: []byte(reconcilePayload(2, 1, `{"id":"200","owner":"other","dateupload":"1700000000","lastupdate":"1700000100","date_faved":"1700000200","ispublic":1}`))})
	if err != nil || !firstFavorites.HasMore || firstFavorites.CompleteReconcile || len(firstFavorites.Discoveries) != 0 {
		t.Fatalf("first favorites page = %#v, err=%v", firstFavorites, err)
	}

	favoriteItem.Cursor.Page = 2
	lastFavorites, err := (Reconcile{}).Handle(t.Context(), domain.Page{Item: favoriteItem, Number: 2, Payload: []byte(reconcilePayload(2, 2, `{"id":"201","owner":"other","dateupload":"1700000000","lastupdate":"1700000100","date_faved":"1700000300","ispublic":1}`))})
	if err != nil || lastFavorites.HasMore || !lastFavorites.CompleteReconcile || lastFavorites.NextCursor != nil || len(lastFavorites.Discoveries) != 0 || len(lastFavorites.Favorites) != 1 {
		t.Fatalf("last favorites page = %#v, err=%v", lastFavorites, err)
	}
}

func TestReconcileAcceptsACompleteEmptyListAndRejectsUnsafePages(t *testing.T) {
	item := domain.Item{Kind: "reconcile", Subject: "owner", Cursor: domain.Cursor{Page: 1, Method: "flickr.people.getPhotos", CheckpointAt: 1793000000}}
	result, err := (Reconcile{}).Handle(t.Context(), domain.Page{Item: item, Number: 1, Payload: []byte(`{"photos":{"pages":0,"photo":[]}}`)})
	if err != nil || result.HasMore || result.NextCursor == nil || result.NextCursor.Method != "flickr.favorites.getList" {
		t.Fatalf("empty complete list = %#v, err=%v", result, err)
	}

	for _, tc := range []struct {
		name    string
		number  int
		payload string
	}{
		{name: "missing envelope", number: 1, payload: `{}`},
		{name: "empty intermediate page", number: 1, payload: `{"photos":{"pages":2,"photo":[]}}`},
		{name: "page exceeds total", number: 2, payload: `{"photos":{"pages":1,"photo":[]}}`},
		{name: "invalid page count", number: 1, payload: `{"photos":{"pages":"many","photo":[]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := (Reconcile{}).Handle(t.Context(), domain.Page{Item: item, Number: tc.number, Payload: []byte(tc.payload)})
			if err == nil {
				t.Fatal("unsafe page accepted")
			}
		})
	}
}

func reconcilePayload(pages, page int, photo string) string {
	return fmt.Sprintf(`{"photos":{"pages":%d,"page":%d,"photo":[%s]}}`, pages, page, photo)
}
