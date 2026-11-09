package http

import (
	"context"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/catalog/app"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

type contactReadsStub struct {
	app.ContactReadRepository
	err         error
	missing     bool
	interests   app.ContactInterestPage
	detail      app.ContactDetail
	photos      app.ContactPhotoPage
	favorites   app.ContactFavoritePage
	albums      app.ContactAlbumPage
	galleries   app.ContactGalleryPage
	groups      app.ContactGroupPage
	about       app.ContactAbout
	pool        app.ContactPoolCrawl
	prioritised int64
}

func (s *contactReadsStub) ListContactInterests(context.Context, int64, string, string, string, string, int) (app.ContactInterestPage, error) {
	return s.interests, s.err
}

func (s *contactReadsStub) GetContact(context.Context, int64, string) (app.ContactDetail, bool, error) {
	return s.detail, !s.missing, s.err
}

func (s *contactReadsStub) ListContactPhotos(context.Context, int64, string, string, int) (app.ContactPhotoPage, error) {
	return s.photos, s.err
}

func (s *contactReadsStub) ListContactFavorites(context.Context, int64, string, string, int) (app.ContactFavoritePage, error) {
	return s.favorites, s.err
}

func (s *contactReadsStub) ListContactAlbums(context.Context, int64, string) (app.ContactAlbumPage, error) {
	return s.albums, s.err
}

func (s *contactReadsStub) ListContactGalleries(context.Context, int64, string) (app.ContactGalleryPage, error) {
	return s.galleries, s.err
}

func (s *contactReadsStub) ListContactGroups(context.Context, int64, string) (app.ContactGroupPage, error) {
	return s.groups, s.err
}

func (s *contactReadsStub) GetContactAbout(context.Context, int64, string) (app.ContactAbout, bool, error) {
	return s.about, !s.missing, s.err
}

func (s *contactReadsStub) SetContactGroupPoolCrawl(context.Context, int64, string, string, bool) (app.ContactPoolCrawl, bool, error) {
	return s.pool, !s.missing, s.err
}

func (s *contactReadsStub) PrioritiseContactSizes(context.Context, int64, string) (int64, error) {
	return s.prioritised, s.err
}

func TestContactHandlersReturnContactSections(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	url := "https://images.example.invalid/a.jpg"
	cursor := "next"
	stub := &contactReadsStub{
		interests: app.ContactInterestPage{Items: []app.ContactInterest{{NSID: "one@N00", Name: "One", Username: "one", AvatarURL: &url, Status: "crawling", RecentPhotos: []app.ContactRecentPhoto{{ID: "9", Alt: "Night", ThumbnailURL: &url}}}}, Counts: app.ContactInterestCounts{All: 1}, NextCursor: &cursor},
		detail:    app.ContactDetail{NSID: "one@N00", Username: "one", RealName: "One", AvatarURL: &url, FoundVia: []app.ContactFoundVia{{NSID: "root@N00", Label: "Root"}}, CrawlStatus: "running", PhotoRecordsTotal: pointer[int64](10), Facets: []app.ContactFacet{{Name: "photos", Completed: 5, Total: pointer[int64](10), Status: "partial"}}, Counts: app.ContactCounts{Photos: 10}},
		photos:    app.ContactPhotoPage{Items: []app.ContactPhoto{{ID: "9", Title: "Night", ThumbnailURL: &url, Width: pointer(640), Height: pointer(480), SizesAvailable: true}}, Total: 1, NextCursor: &cursor},
		favorites: app.ContactFavoritePage{Items: []app.ContactFavorite{{ContactPhoto: app.ContactPhoto{ID: "9", Title: "Night"}, OwnerNSID: "owner@N00", OwnerName: stringPointer("Owner")}}, Total: 1},
		albums:    app.ContactAlbumPage{Items: []app.ContactAlbum{{ID: "album", Title: "Trip", OwnerName: "One", StoredCount: 3, TotalCount: 4, CoverPhotoURLs: []string{url}}}, Total: 1},
		galleries: app.ContactGalleryPage{Items: []app.ContactGallery{{ID: "gallery", Title: "Best", PhotoCount: 2}}, Total: 1},
		groups:    app.ContactGroupPage{Items: []app.ContactGroup{{NSID: "group", Name: "Street", MembersCount: 20}}, Total: 1},
		about:     app.ContactAbout{Facets: []app.ContactFacet{{Name: "photos", Completed: 5, Status: "partial"}}, TombstoneCount: 2, LastChangeCheckAt: &now, TombstoneUpdatedAt: &now, UpdatedAt: &now},
		pool:      app.ContactPoolCrawl{Enabled: true, ChangedAt: now}, prioritised: 4,
	}
	handler := Handler{ContactReads: app.ContactReads{Repository: stub}, ActorID: func(context.Context) (int64, bool) { return 7, true }}
	limit := 30
	requests := []func() (any, error){
		func() (any, error) {
			return handler.ListMeCrawlInterests(t.Context(), gen.ListMeCrawlInterestsRequestObject{Params: gen.ListMeCrawlInterestsParams{Limit: &limit}})
		},
		func() (any, error) {
			return handler.GetMeContact(t.Context(), gen.GetMeContactRequestObject{Nsid: "one@N00"})
		},
		func() (any, error) {
			return handler.ListMeContactPhotos(t.Context(), gen.ListMeContactPhotosRequestObject{Nsid: "one@N00"})
		},
		func() (any, error) {
			return handler.ListMeContactFavorites(t.Context(), gen.ListMeContactFavoritesRequestObject{Nsid: "one@N00"})
		},
		func() (any, error) {
			return handler.ListMeContactAlbums(t.Context(), gen.ListMeContactAlbumsRequestObject{Nsid: "one@N00"})
		},
		func() (any, error) {
			return handler.ListMeContactGalleries(t.Context(), gen.ListMeContactGalleriesRequestObject{Nsid: "one@N00"})
		},
		func() (any, error) {
			return handler.ListMeContactGroups(t.Context(), gen.ListMeContactGroupsRequestObject{Nsid: "one@N00"})
		},
		func() (any, error) {
			return handler.GetMeContactAbout(t.Context(), gen.GetMeContactAboutRequestObject{Nsid: "one@N00"})
		},
		func() (any, error) {
			return handler.SetMeContactGroupPoolCrawl(t.Context(), gen.SetMeContactGroupPoolCrawlRequestObject{Nsid: "one@N00", GroupNsid: "group", Body: &gen.ContactGroupPoolCrawlRequest{Enabled: true}})
		},
		func() (any, error) {
			return handler.PrioritiseMeContactSizes(t.Context(), gen.PrioritiseMeContactSizesRequestObject{Nsid: "one@N00"})
		},
	}
	for _, request := range requests {
		response, err := request()
		if err != nil {
			t.Fatalf("handler error=%v", err)
		}
		switch value := response.(type) {
		case gen.ListMeCrawlInterests200JSONResponse:
			if len(value.Items) != 1 || value.Items[0].RecentPhotos[0].ThumbnailURL == nil || *value.Items[0].RecentPhotos[0].ThumbnailURL != url || value.NextCursor == nil {
				t.Fatalf("interests response=%+v", value)
			}
		case gen.GetMeContact200JSONResponse:
			if value.Nsid != "one@N00" || len(value.FoundVia) != 1 || value.AvatarURL == nil {
				t.Fatalf("detail response=%+v", value)
			}
		case gen.ListMeContactPhotos200JSONResponse:
			if len(value.Items) != 1 || value.Items[0].Width == nil || value.NextCursor == nil {
				t.Fatalf("photos response=%+v", value)
			}
		case gen.ListMeContactFavorites200JSONResponse:
			if len(value.Items) != 1 || value.Items[0].OwnerName == nil {
				t.Fatalf("favorites response=%+v", value)
			}
		case gen.ListMeContactAlbums200JSONResponse:
			if len(value.Items) != 1 || value.Items[0].CoverPhotoUrls[0] != url {
				t.Fatalf("albums response=%+v", value)
			}
		case gen.ListMeContactGalleries200JSONResponse:
			if len(value.Items) != 1 {
				t.Fatalf("galleries response=%+v", value)
			}
		case gen.ListMeContactGroups200JSONResponse:
			if len(value.Items) != 1 {
				t.Fatalf("groups response=%+v", value)
			}
		case gen.GetMeContactAbout200JSONResponse:
			if value.Tombstones.Count != 2 || value.LastChangeCheckAt == nil {
				t.Fatalf("about response=%+v", value)
			}
		case gen.SetMeContactGroupPoolCrawl200JSONResponse:
			if !value.Enabled || !value.ChangedAt.Equal(now) {
				t.Fatalf("pool response=%+v", value)
			}
		case gen.PrioritiseMeContactSizes200JSONResponse:
			if value.Prioritised != 4 {
				t.Fatalf("sizes response=%+v", value)
			}
		default:
			t.Fatalf("unexpected response type %T", response)
		}
	}
	if got := contactDetail(stub.detail); got.CoverPhotoURL != nil || got.SizesCompleted != 0 {
		t.Fatalf("optional detail fields=%+v", got)
	}
	if contactLimit(nil) != app.DefaultPageSize || contactLimit(&limit) != limit || positive(pointer(0)) != nil || validURL(stringPointer("javascript:alert(1)")) != nil {
		t.Fatal("contact response helpers accepted invalid values")
	}
}

func TestContactHandlersMapAuthenticationAndRequestErrors(t *testing.T) {
	requests := []func(Handler) (any, error){
		func(h Handler) (any, error) {
			return h.ListMeCrawlInterests(t.Context(), gen.ListMeCrawlInterestsRequestObject{})
		},
		func(h Handler) (any, error) { return h.GetMeContact(t.Context(), gen.GetMeContactRequestObject{}) },
		func(h Handler) (any, error) {
			return h.ListMeContactPhotos(t.Context(), gen.ListMeContactPhotosRequestObject{})
		},
		func(h Handler) (any, error) {
			return h.ListMeContactFavorites(t.Context(), gen.ListMeContactFavoritesRequestObject{})
		},
		func(h Handler) (any, error) {
			return h.ListMeContactAlbums(t.Context(), gen.ListMeContactAlbumsRequestObject{})
		},
		func(h Handler) (any, error) {
			return h.ListMeContactGalleries(t.Context(), gen.ListMeContactGalleriesRequestObject{})
		},
		func(h Handler) (any, error) {
			return h.ListMeContactGroups(t.Context(), gen.ListMeContactGroupsRequestObject{})
		},
		func(h Handler) (any, error) {
			return h.GetMeContactAbout(t.Context(), gen.GetMeContactAboutRequestObject{})
		},
		func(h Handler) (any, error) {
			return h.SetMeContactGroupPoolCrawl(t.Context(), gen.SetMeContactGroupPoolCrawlRequestObject{})
		},
		func(h Handler) (any, error) {
			return h.PrioritiseMeContactSizes(t.Context(), gen.PrioritiseMeContactSizesRequestObject{})
		},
	}
	for _, request := range requests {
		response, err := request(Handler{})
		if err != nil {
			t.Fatalf("unauthenticated handler error=%v", err)
		}
		if response == nil {
			t.Fatal("unauthenticated handler returned nil response")
		}
	}

	stub := &contactReadsStub{err: app.ErrNotFound}
	handler := Handler{ContactReads: app.ContactReads{Repository: stub}, ActorID: func(context.Context) (int64, bool) { return 7, true }}
	if response, err := handler.GetMeContact(t.Context(), gen.GetMeContactRequestObject{Nsid: "one@N00"}); err != nil {
		t.Fatal(err)
	} else if _, ok := response.(gen.GetMeContact404JSONResponse); !ok {
		t.Fatalf("detail not-found response=%T", response)
	}
	if response, err := handler.ListMeContactPhotos(t.Context(), gen.ListMeContactPhotosRequestObject{Nsid: "one@N00"}); err != nil {
		t.Fatal(err)
	} else if _, ok := response.(gen.ListMeContactPhotos404JSONResponse); !ok {
		t.Fatalf("photos not-found response=%T", response)
	}
	if response, err := handler.ListMeContactFavorites(t.Context(), gen.ListMeContactFavoritesRequestObject{Nsid: "one@N00"}); err != nil {
		t.Fatal(err)
	} else if _, ok := response.(gen.ListMeContactFavorites404JSONResponse); !ok {
		t.Fatalf("favorites not-found response=%T", response)
	}
	if response, err := handler.GetMeContactAbout(t.Context(), gen.GetMeContactAboutRequestObject{Nsid: "one@N00"}); err != nil {
		t.Fatal(err)
	} else if _, ok := response.(gen.GetMeContactAbout404JSONResponse); !ok {
		t.Fatalf("about not-found response=%T", response)
	}
	if response, err := handler.SetMeContactGroupPoolCrawl(t.Context(), gen.SetMeContactGroupPoolCrawlRequestObject{Nsid: "one@N00", GroupNsid: "group", Body: &gen.ContactGroupPoolCrawlRequest{Enabled: true}}); err != nil {
		t.Fatal(err)
	} else if _, ok := response.(gen.SetMeContactGroupPoolCrawl404JSONResponse); !ok {
		t.Fatalf("pool not-found response=%T", response)
	}
	if response, err := handler.SetMeContactGroupPoolCrawl(t.Context(), gen.SetMeContactGroupPoolCrawlRequestObject{Nsid: "one@N00", GroupNsid: "group"}); err != nil {
		t.Fatal(err)
	} else if _, ok := response.(gen.SetMeContactGroupPoolCrawl404JSONResponse); !ok {
		t.Fatalf("pool empty-body response=%T", response)
	}
	if response, err := handler.PrioritiseMeContactSizes(t.Context(), gen.PrioritiseMeContactSizesRequestObject{Nsid: "one@N00"}); err != nil {
		t.Fatal(err)
	} else if _, ok := response.(gen.PrioritiseMeContactSizes404JSONResponse); !ok {
		t.Fatalf("sizes not-found response=%T", response)
	}

	stub.err = app.ErrInvalidCursor
	if response, err := handler.ListMeContactPhotos(t.Context(), gen.ListMeContactPhotosRequestObject{Nsid: "one@N00"}); err != nil {
		t.Fatal(err)
	} else if _, ok := response.(gen.ListMeContactPhotos400JSONResponse); !ok {
		t.Fatalf("photos invalid cursor response=%T", response)
	}
	if response, err := handler.ListMeContactFavorites(t.Context(), gen.ListMeContactFavoritesRequestObject{Nsid: "one@N00"}); err != nil {
		t.Fatal(err)
	} else if _, ok := response.(gen.ListMeContactFavorites400JSONResponse); !ok {
		t.Fatalf("favorites invalid cursor response=%T", response)
	}
	stub.err = app.ErrInvalidPage
	if response, err := handler.ListMeCrawlInterests(t.Context(), gen.ListMeCrawlInterestsRequestObject{}); err != nil {
		t.Fatal(err)
	} else if _, ok := response.(gen.ListMeCrawlInterests400JSONResponse); !ok {
		t.Fatalf("interests invalid page response=%T", response)
	}
}

func pointer[T any](value T) *T          { return &value }
func stringPointer(value string) *string { return &value }
