package http

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/home/app"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

type serviceStub struct {
	snapshot app.Snapshot
	err      error
	userID   int64
	called   bool
}

func (s *serviceStub) Get(_ context.Context, userID int64) (app.Snapshot, error) {
	s.called = true
	s.userID = userID
	return s.snapshot, s.err
}

func TestGetMeHomeRequiresActor(t *testing.T) {
	for _, actor := range []func(context.Context) (int64, bool){nil, func(context.Context) (int64, bool) { return 0, false }} {
		response, err := (Handler{ActorID: actor, Snapshot: &serviceStub{}}).GetMeHome(t.Context(), gen.GetMeHomeRequestObject{})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := response.(gen.GetMeHome401JSONResponse); !ok {
			t.Fatalf("response = %T, want unauthorized", response)
		}
	}
}

func TestGetMeHomeMapsSnapshot(t *testing.T) {
	thumbnail := "https://example.invalid/thumb.jpg"
	takenAt := time.Date(2026, time.November, 8, 11, 0, 0, 0, time.UTC)
	generatedAt := takenAt.Add(time.Hour)
	countsComputedAt := generatedAt.Add(-time.Minute)
	service := &serviceStub{snapshot: app.Snapshot{
		GeneratedAt:      generatedAt,
		CountsComputedAt: &countsComputedAt,
		Estimated:        true,
		Contacts:         app.ContactCounts{Total: 3, Direct: 1, Discovered: 2},
		Photos:           app.PhotoCounts{Total: 9, SizesReady: 7, Captioned: 5},
		NewPhotos:        []app.Photo{{ID: 44, Title: "Home photo", ThumbnailURL: &thumbnail, OwnerNSID: "owner", OwnerName: "Owner", TakenAt: &takenAt, Captioned: true}},
		StarredContacts:  []app.Contact{{NSID: "owner", Name: "Owner", IconURL: "icon", PhotosCount: 9, SizesReady: 7, SizesTotal: 9, CrawlState: "crawling"}},
		Activity:         []app.Activity{{ID: "event-1", Kind: "crawl", MessageKey: "new_photos", Count: 2, ContactName: "Owner", OccurredAt: takenAt, Severity: "info"}},
	}}
	handler := Handler{ActorID: func(context.Context) (int64, bool) { return 17, true }, Snapshot: service}
	response, err := handler.GetMeHome(t.Context(), gen.GetMeHomeRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := response.(gen.GetMeHome200JSONResponse)
	if !ok {
		t.Fatalf("response = %T, want 200", response)
	}
	if got.GeneratedAt != generatedAt || got.CountsComputedAt == nil || *got.CountsComputedAt != countsComputedAt || !got.Estimated || got.Contacts.Total != 3 || got.Photos.Captioned != 5 || len(got.NewPhotos) != 1 || got.NewPhotos[0].ThumbnailURL == nil || *got.NewPhotos[0].ThumbnailURL != thumbnail || got.NewPhotos[0].TakenAt == nil {
		t.Fatalf("snapshot response = %+v", got)
	}
	if len(got.StarredContacts) != 1 || got.StarredContacts[0].CrawlState != gen.HomeCrawlStateCrawling || len(got.Activity) != 1 || got.Activity[0].MessageKey != gen.HomeActivityMessageKeyNewPhotos {
		t.Fatalf("contacts/activity response = %+v / %+v", got.StarredContacts, got.Activity)
	}
	if !service.called || service.userID != 17 {
		t.Fatalf("service call user = %d, called = %t", service.userID, service.called)
	}
}

func TestGetMeHomeRequiresService(t *testing.T) {
	handler := Handler{ActorID: func(context.Context) (int64, bool) { return 1, true }}
	if _, err := handler.GetMeHome(t.Context(), gen.GetMeHomeRequestObject{}); err == nil {
		t.Fatal("GetMeHome() succeeded without a snapshot service")
	}
}

func TestGetMeHomeReturnsServiceErrors(t *testing.T) {
	want := errors.New("database unavailable")
	handler := Handler{ActorID: func(context.Context) (int64, bool) { return 1, true }, Snapshot: &serviceStub{err: want}}
	if _, err := handler.GetMeHome(t.Context(), gen.GetMeHomeRequestObject{}); !errors.Is(err, want) {
		t.Fatalf("GetMeHome() err = %v, want service error", err)
	}
}
