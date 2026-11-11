package app

import (
	"context"
	"errors"
	"testing"
)

type contactReadFake struct {
	ContactReadRepository
	called  string
	list    ContactInterestPage
	detail  ContactDetail
	about   ContactAbout
	pool    ContactPoolCrawl
	err     error
	missing bool
}

func (f *contactReadFake) ListContactInterests(_ context.Context, _ int64, filter, q, sort, cursor string, _ int) (ContactInterestPage, error) {
	f.called = "list:" + filter + ":" + q + ":" + sort + ":" + cursor
	return f.list, f.err
}

func (f *contactReadFake) GetContact(_ context.Context, _ int64, nsid string) (ContactDetail, bool, error) {
	f.called = "detail:" + nsid
	return f.detail, !f.missing, f.err
}

func (f *contactReadFake) ListContactPhotos(_ context.Context, _ int64, nsid, cursor string, _ int) (ContactPhotoPage, error) {
	f.called = "photos:" + nsid + ":" + cursor
	return ContactPhotoPage{}, f.err
}

func (f *contactReadFake) ListContactFavorites(_ context.Context, _ int64, nsid, cursor string, _ int) (ContactFavoritePage, error) {
	f.called = "favorites:" + nsid + ":" + cursor
	return ContactFavoritePage{}, f.err
}

func (f *contactReadFake) ListContactAlbums(_ context.Context, _ int64, nsid, _ string, _ int) (ContactAlbumPage, error) {
	f.called = "albums:" + nsid
	return ContactAlbumPage{}, f.err
}

func (f *contactReadFake) ListContactGalleries(_ context.Context, _ int64, nsid, _ string, _ int) (ContactGalleryPage, error) {
	f.called = "galleries:" + nsid
	return ContactGalleryPage{}, f.err
}

func (f *contactReadFake) ListContactGroups(_ context.Context, _ int64, nsid, _ string, _ int) (ContactGroupPage, error) {
	f.called = "groups:" + nsid
	return ContactGroupPage{}, f.err
}

func (f *contactReadFake) GetContactAbout(_ context.Context, _ int64, nsid string) (ContactAbout, bool, error) {
	f.called = "about:" + nsid
	return f.about, !f.missing, f.err
}

func (f *contactReadFake) SetContactGroupPoolCrawl(_ context.Context, _ int64, nsid, group string, enabled bool) (ContactPoolCrawl, bool, error) {
	f.called = "pool:" + nsid + ":" + group
	return ContactPoolCrawl{Enabled: enabled}, !f.missing, f.err
}

func (f *contactReadFake) PrioritiseContactSizes(_ context.Context, _ int64, nsid string) (int64, error) {
	f.called = "sizes:" + nsid
	return 3, f.err
}

func TestContactReadsValidateAndForwardListOptions(t *testing.T) {
	t.Parallel()
	fake := &contactReadFake{list: ContactInterestPage{Counts: ContactInterestCounts{All: 4}}}
	service := ContactReads{Repository: fake}
	page, err := service.ListInterests(t.Context(), 9, "starred", " name ", "name", "cursor", 30)
	if err != nil || page.Counts.All != 4 || fake.called != "list:starred:name:name:cursor" {
		t.Fatalf("list=%+v called=%q err=%v", page, fake.called, err)
	}
	for _, test := range []struct {
		name         string
		filter, sort string
		limit        int
		want         error
	}{{"bad filter", "unknown", "updated", 30, ErrInvalidPage}, {"bad sort", "all", "unknown", 30, ErrInvalidPage}, {"bad limit", "all", "name", 101, ErrInvalidPage}, {"long query", "all", "name", 30, ErrInvalidPage}} {
		t.Run(test.name, func(t *testing.T) {
			q := ""
			if test.name == "long query" {
				q = string(make([]byte, 101))
			}
			_, err := service.ListInterests(t.Context(), 9, test.filter, q, test.sort, "", test.limit)
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want %v", err, test.want)
			}
		})
	}
	if _, err := (ContactReads{}).ListInterests(t.Context(), 9, "all", "", "updated", "", 30); !errors.Is(err, ErrContactReadUnavailable) {
		t.Fatalf("missing repository error=%v", err)
	}
}

func TestContactReadsRequireScopedContactAndForwardSections(t *testing.T) {
	t.Parallel()
	fake := &contactReadFake{}
	service := ContactReads{Repository: fake}
	ctx := t.Context()
	if _, err := service.GetContact(ctx, 2, "one@N00"); err != nil || fake.called != "detail:one@N00" {
		t.Fatalf("detail called=%q err=%v", fake.called, err)
	}
	if _, err := service.GetContact(ctx, 2, " "); !errors.Is(err, ErrNotFound) {
		t.Fatalf("blank contact error=%v", err)
	}
	if _, err := service.GetContact(ctx, 2, string(make([]byte, 256))); !errors.Is(err, ErrNotFound) {
		t.Fatalf("long contact error=%v", err)
	}
	checks := []struct {
		name string
		run  func() error
		want string
	}{
		{"photos", func() error { _, err := service.ListContactPhotos(ctx, 2, "one@N00", "next", 30); return err }, "photos:one@N00:next"},
		{"favorites", func() error { _, err := service.ListContactFavorites(ctx, 2, "one@N00", "next", 30); return err }, "favorites:one@N00:next"},
		{"albums", func() error { _, err := service.ListContactAlbums(ctx, 2, "one@N00", "next", 30); return err }, "albums:one@N00"},
		{"galleries", func() error { _, err := service.ListContactGalleries(ctx, 2, "one@N00", "next", 30); return err }, "galleries:one@N00"},
		{"groups", func() error { _, err := service.ListContactGroups(ctx, 2, "one@N00", "next", 30); return err }, "groups:one@N00"},
		{"about", func() error { _, err := service.GetContactAbout(ctx, 2, "one@N00"); return err }, "about:one@N00"},
		{"pool", func() error { _, err := service.SetContactGroupPoolCrawl(ctx, 2, "one@N00", "group", true); return err }, "pool:one@N00:group"},
		{"sizes", func() error { _, err := service.PrioritiseContactSizes(ctx, 2, "one@N00"); return err }, "sizes:one@N00"},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.run(); err != nil || fake.called != check.want {
				t.Fatalf("called=%q err=%v want %q", fake.called, err, check.want)
			}
		})
	}
	if _, err := service.SetContactGroupPoolCrawl(ctx, 2, "one@N00", " ", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty group error=%v", err)
	}
}

func TestContactReadsPropagateRepositoryErrorsAndMissingContact(t *testing.T) {
	t.Parallel()
	boom := errors.New("database unavailable")
	fake := &contactReadFake{err: boom}
	service := ContactReads{Repository: fake}
	if _, err := service.GetContact(t.Context(), 1, "one@N00"); !errors.Is(err, boom) {
		t.Fatalf("detail error=%v", err)
	}
	fake.err = nil
	fake.ContactReadRepository = nil
	if _, err := (ContactReads{}).GetContact(t.Context(), 1, "one@N00"); !errors.Is(err, ErrContactReadUnavailable) {
		t.Fatalf("missing repository error=%v", err)
	}
	if _, err := service.GetContact(nil, 1, "one@N00"); !errors.Is(err, ErrContactReadUnavailable) {
		t.Fatalf("nil context error=%v", err)
	}
}

func TestContactReadsMapMissingResourcesAndPropagateEachRepositoryError(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fake := &contactReadFake{missing: true}
	service := ContactReads{Repository: fake}
	if _, err := service.GetContact(ctx, 1, "one@N00"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing detail error=%v", err)
	}
	if _, err := service.GetContactAbout(ctx, 1, "one@N00"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing about error=%v", err)
	}
	if _, err := service.SetContactGroupPoolCrawl(ctx, 1, "one@N00", "group", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing group preference error=%v", err)
	}

	boom := errors.New("database unavailable")
	fake.missing, fake.err = false, boom
	calls := []struct {
		name string
		run  func() error
	}{
		{"interests", func() error { _, err := service.ListInterests(ctx, 1, "all", "", "updated", "", 30); return err }},
		{"detail", func() error { _, err := service.GetContact(ctx, 1, "one@N00"); return err }},
		{"photos", func() error { _, err := service.ListContactPhotos(ctx, 1, "one@N00", "", 30); return err }},
		{"favorites", func() error { _, err := service.ListContactFavorites(ctx, 1, "one@N00", "", 30); return err }},
		{"albums", func() error { _, err := service.ListContactAlbums(ctx, 1, "one@N00", "", 30); return err }},
		{"galleries", func() error { _, err := service.ListContactGalleries(ctx, 1, "one@N00", "", 30); return err }},
		{"groups", func() error { _, err := service.ListContactGroups(ctx, 1, "one@N00", "", 30); return err }},
		{"about", func() error { _, err := service.GetContactAbout(ctx, 1, "one@N00"); return err }},
		{"pool", func() error { _, err := service.SetContactGroupPoolCrawl(ctx, 1, "one@N00", "group", true); return err }},
		{"sizes", func() error { _, err := service.PrioritiseContactSizes(ctx, 1, "one@N00"); return err }},
	}
	for _, call := range calls {
		t.Run(call.name, func(t *testing.T) {
			if err := call.run(); !errors.Is(err, boom) {
				t.Fatalf("error=%v want %v", err, boom)
			}
		})
	}
	if _, err := service.ListContactPhotos(ctx, 1, "one@N00", "", 101); !errors.Is(err, ErrInvalidPage) {
		t.Fatalf("invalid photo limit error=%v", err)
	}
	if _, err := service.ListContactFavorites(ctx, 1, "one@N00", "", 101); !errors.Is(err, ErrInvalidPage) {
		t.Fatalf("invalid favorite limit error=%v", err)
	}
	for _, check := range []struct {
		name string
		run  func() error
	}{
		{"albums", func() error { _, err := service.ListContactAlbums(ctx, 1, "one@N00", "", 101); return err }},
		{"galleries", func() error { _, err := service.ListContactGalleries(ctx, 1, "one@N00", "", 101); return err }},
		{"groups", func() error { _, err := service.ListContactGroups(ctx, 1, "one@N00", "", 101); return err }},
	} {
		t.Run("invalid "+check.name+" limit", func(t *testing.T) {
			if err := check.run(); !errors.Is(err, ErrInvalidPage) {
				t.Fatalf("invalid %s limit error=%v", check.name, err)
			}
		})
	}
	if _, err := service.SetContactGroupPoolCrawl(ctx, 1, "one@N00", "group", true); !errors.Is(err, boom) {
		t.Fatalf("preference error=%v", err)
	}
}
