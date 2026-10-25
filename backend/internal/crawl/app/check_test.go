package app

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/crawl/domain"
	"github.com/jooservices/xflickr/internal/crawl/facets"
)

func TestCheckRequestsUseOnePhotoAndFavoritePerPage(t *testing.T) {
	requests := CheckRequests("person-1", 100, 200)
	if len(requests) != 2 {
		t.Fatalf("got %d probes, want 2", len(requests))
	}
	if requests[0].Method != "flickr.people.getPhotos" || requests[0].Params["min_upload_date"] != "100" || requests[0].Params["per_page"] != "1" {
		t.Fatalf("photo probe = %#v", requests[0])
	}
	if requests[1].Method != "flickr.favorites.getList" || requests[1].Params["min_fave_date"] != "200" || requests[1].Params["per_page"] != "1" {
		t.Fatalf("favorite probe = %#v", requests[1])
	}
}

func TestIncrementalWorkOnlyPlansChangedLists(t *testing.T) {
	got, err := IncrementalWork("person-1", 100, 200, json.RawMessage(`{"photos":{"total":"1"}}`), json.RawMessage(`{"photos":{"total":0}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != "photos" || got[0].Params["min_upload_date"] != "100" || got[0].Params["per_page"] != "500" || got[0].Params["extras"] != incrementalPhotoExtras || !got[0].Reopen {
		t.Fatalf("incremental work = %#v", got)
	}
	got, err = IncrementalWork("person-1", 100, 200, json.RawMessage(`{"photos":{"total":0}}`), json.RawMessage(`{"photos":{"total":"3"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != "favorites" || got[0].Params["min_fave_date"] != "200" || got[0].Params["extras"] != incrementalPhotoExtras || !got[0].Reopen {
		t.Fatalf("favorite incremental work = %#v", got)
	}
	got, err = IncrementalWork("person-1", 100, 200, json.RawMessage(`{"photos":{"total":"0"}}`), json.RawMessage(`{"photos":{"total":0}}`))
	if err != nil || len(got) != 0 {
		t.Fatalf("unchanged lists = %#v, %v", got, err)
	}
}

func TestIncrementalWorkRejectsMalformedTotals(t *testing.T) {
	for _, payload := range []string{`not-json`, `{"photos":{"total":"many"}}`, `{"photos":{"total":-1}}`} {
		if _, err := IncrementalWork("p", 0, 0, json.RawMessage(payload), json.RawMessage(`{"photos":{"total":0}}`)); err == nil {
			t.Errorf("payload %q accepted", payload)
		}
	}
}

func TestChangeCheckUsesOneItemForBothProbesAndPlansOnlyChangedWork(t *testing.T) {
	checker := ChangeCheck{}
	if checker.Name() != "check" || checker.Lane() != domain.LanePerson || len(checker.Plan(context.Background(), domain.Person{NSID: "p"})) != 0 {
		t.Fatal("change check facet metadata is invalid")
	}
	item := domain.Item{Subject: "person", Cursor: domain.Cursor{Method: "flickr.people.getPhotos", CheckpointAt: 77, Params: map[string]string{"min_upload_date": "10", "min_fave_date": "20"}}}
	result, err := checker.Handle(context.Background(), domain.Page{Item: item, Payload: json.RawMessage(`{"photos":{"total":"1"}}`)})
	if err != nil || result.NextCursor == nil || result.NextCursor.Method != "flickr.favorites.getList" || len(result.Work) != 1 {
		t.Fatalf("photo check result = %#v, %v", result, err)
	}
	work := result.Work[0]
	if work.Kind != "photos" || work.Params["min_upload_date"] != "10" || work.Params["per_page"] != "500" || work.Params["extras"] != incrementalPhotoExtras || !work.Reopen || work.CheckpointAt != 77 {
		t.Fatalf("incremental photo work = %#v", work)
	}
	item.Cursor = *result.NextCursor
	result, err = checker.Handle(context.Background(), domain.Page{Item: item, Payload: json.RawMessage(`{"photos":{"total":0}}`)})
	if err != nil || result.NextCursor != nil || len(result.Work) != 3 {
		t.Fatalf("unchanged favorite result = %#v, %v", result, err)
	}
	item.Cursor.Method = "flickr.favorites.getList"
	result, err = checker.Handle(context.Background(), domain.Page{Item: item, Payload: json.RawMessage(`{"photos":{"total":1}}`)})
	if err != nil || len(result.Work) != 4 || result.Work[0].Kind != "albums" || result.Work[3].Kind != "favorites" || result.Work[3].Params["min_fave_date"] != "20" || result.Work[3].Params["extras"] != incrementalPhotoExtras || !result.Work[3].Reopen {
		t.Fatalf("changed favorite result = %#v, %v", result, err)
	}
	item.Cursor.Method = "unsupported"
	if _, err := checker.Handle(context.Background(), domain.Page{Item: item, Payload: json.RawMessage(`{"photos":{"total":0}}`)}); err == nil {
		t.Fatal("unsupported check method accepted")
	}
}

func TestContactsPhotosRequestCanFlagAccountContacts(t *testing.T) {
	request := ContactsPhotosRequest("account-nsid")
	if request.Kind != "contacts_photos" || request.Method != "flickr.photos.getContactsPhotos" || request.Subject != "account-nsid" || !reflect.DeepEqual(request.Params, map[string]string{"user_id": "account-nsid", "count": "50"}) {
		t.Fatalf("contacts photos request = %#v", request)
	}
}

func TestContactsPhotosFlagsSeveralPeopleForChecks(t *testing.T) {
	facet := ContactsPhotos{}
	if facet.Name() != "contacts_photos" || facet.Lane() != domain.LanePerson || len(facet.Plan(context.Background(), domain.Person{})) != 0 {
		t.Fatal("contacts photos facet metadata is invalid")
	}
	result, err := facet.Handle(context.Background(), domain.Page{
		Item:    domain.Item{Cursor: domain.Cursor{CheckpointAt: 44}},
		Payload: json.RawMessage(`{"photos":{"photo":[{"owner":"a"},{"owner":"b"},{"owner":"a"}]}}`),
	})
	if err != nil || len(result.Work) != 2 {
		t.Fatalf("contacts result = %#v, %v", result, err)
	}
	for i, owner := range []string{"a", "b"} {
		work := result.Work[i]
		if work.Kind != "check" || work.Subject != owner || work.Method != "flickr.people.getPhotos" || work.CheckpointAt != 44 || work.Params["per_page"] != "1" || !work.Reopen {
			t.Fatalf("flagged check %d = %#v", i, work)
		}
	}
}

func TestFlaggedPeopleFromContactsPhotosDeduplicatesOwners(t *testing.T) {
	got, err := FlaggedPeopleFromContactsPhotos(json.RawMessage(`{"photos":{"photo":[{"owner":"a"},{"owner":"b"},{"owner":"a"},{"owner":""}]}}`))
	if err != nil || !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("flagged people = %#v, %v", got, err)
	}
	if _, err := FlaggedPeopleFromContactsPhotos(json.RawMessage(`{`)); err == nil {
		t.Fatal("invalid contacts photos payload accepted")
	}
}

func TestCheckIntervalDue(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	if !CheckIntervalDue(time.Time{}, now, time.Hour) {
		t.Fatal("never checked person is not due")
	}
	if CheckIntervalDue(now.Add(-time.Minute), now, time.Hour) {
		t.Fatal("recent check is due")
	}
	if !CheckIntervalDue(now.Add(-time.Hour), now, time.Hour) {
		t.Fatal("check at interval boundary is not due")
	}
	if CheckIntervalDue(now, now, 0) {
		t.Fatal("zero interval should use the 24 hour default")
	}
}

func TestCatalogKindsArePlannedExactlyOncePerCycle(t *testing.T) {
	registry := NewRegistry()
	for _, facet := range []domain.Facet{facets.Photos{}, facets.Favorites{}, facets.Albums{}, facets.Galleries{}, facets.Groups{}} {
		if err := registry.Register(facet); err != nil {
			t.Fatal(err)
		}
	}
	initial := map[string]string{}
	for _, spec := range registry.Plan(context.Background(), domain.Person{NSID: "p@N01"}) {
		initial[spec.Kind] = spec.Method
	}
	refresh := map[string]int{}
	for _, probe := range []string{"flickr.people.getPhotos", "flickr.favorites.getList"} {
		item := domain.Item{Subject: "p@N01", Cursor: domain.Cursor{Method: probe}}
		result, err := ChangeCheck{}.Handle(context.Background(), domain.Page{Item: item, Payload: json.RawMessage(`{"photos":{"total":0}}`)})
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range result.Work {
			refresh[spec.Kind]++
			if want, ok := initial[spec.Kind]; ok && want != spec.Method {
				t.Fatalf("%s refresh uses %s, initial plan uses %s", spec.Kind, spec.Method, want)
			}
		}
	}
	for _, kind := range []string{"albums", "galleries", "groups"} {
		if initial[kind] == "" || refresh[kind] != 1 {
			t.Fatalf("%s: initial=%q refreshed=%d, want planned initially and refreshed once per cycle", kind, initial[kind], refresh[kind])
		}
	}
}
