package app

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/crawl/domain"
	"github.com/jooservices/xflickr/pkg/flickr"
)

func TestAddressedEntity(t *testing.T) {
	for _, tc := range []struct{ method, param, kind string }{
		{"flickr.people.getPhotos", "user_id", "person"},
		{"flickr.favorites.getList", "user_id", "person"},
		{"flickr.photosets.getList", "user_id", "person"},
		{"flickr.galleries.getList", "user_id", "person"},
		{"flickr.people.getGroups", "user_id", "person"},
		{"flickr.contacts.getList", "user_id", "person"},
		{"flickr.photosets.getPhotos", "photoset_id", "album"},
		{"flickr.galleries.getPhotos", "gallery_id", "gallery"},
		{"flickr.groups.pools.getPhotos", "group_id", "group"},
		{"flickr.photos.getExif", "photo_id", "photo"},
	} {
		t.Run(tc.method, func(t *testing.T) {
			ref, ok := AddressedEntity(Request{Method: tc.method, Params: map[string]string{tc.param: "subject"}})
			if !ok || ref.Kind != tc.kind || ref.ID != "subject" {
				t.Fatalf("got %+v, %v", ref, ok)
			}
			if got := FailureState(ref, flickr.ErrNotFound); got != map[bool]string{true: "restricted", false: "gone"}[tc.kind == "photo"] {
				t.Fatal(got)
			}
			if got := FailureState(ref, flickr.ErrPermission); got != "restricted" {
				t.Fatal(got)
			}
		})
	}
	if _, ok := AddressedEntity(Request{}); ok {
		t.Fatal("unknown request has a subject")
	}
	if FailureState(EntityRef{}, nil) != "" {
		t.Fatal("success marked")
	}
}

type stateFake struct {
	blocked       bool
	err           error
	calls         int
	recorded      []EntityAccess
	state, reason string
	recheck       time.Time
}

func (s *stateFake) Suppressed(_ context.Context, a EntityAccess, _ time.Time) (bool, error) {
	s.calls++
	return s.blocked && a.KeyID != 0, s.err
}

func (s *stateFake) RecordFailure(_ context.Context, a EntityAccess, state, reason string, _ time.Time, recheck time.Time) error {
	s.recorded = append(s.recorded, a)
	s.state = state
	s.reason = reason
	s.recheck = recheck
	return s.err
}

func TestEngineRecordsAndSuppressesEveryPersonFacet(t *testing.T) {
	for _, method := range []string{"flickr.people.getPhotos", "flickr.favorites.getList", "flickr.photosets.getList", "flickr.galleries.getList", "flickr.people.getGroups", "flickr.contacts.getList"} {
		for _, cause := range []error{flickr.ErrNotFound, flickr.ErrPermission, flickr.ErrResourceDenied} {
			t.Run(method+cause.Error(), func(t *testing.T) {
				r := newRig(t)
				states := &stateFake{}
				r.engine.States = states
				r.store.item.Cursor.Method = method
				r.flickr.err = cause
				if err := r.engine.Handle(t.Context(), 7); err != nil {
					t.Fatal(err)
				}
				if len(states.recorded) != 1 || states.recorded[0].Kind != "person" || states.recorded[0].Facet != method {
					t.Fatalf("%+v", states)
				}
				if states.recorded[0].UserID != 3 || states.recorded[0].KeyID != 9 {
					t.Fatal("caller lost")
				}
				states.blocked = true
				r.flickr.err = nil
				if err := r.engine.Handle(t.Context(), 7); err != nil {
					t.Fatal(err)
				}
				if len(r.flickr.requests) != 1 {
					t.Fatal("repeated upstream call")
				}
			})
		}
	}
}

func TestEveryPhotoFacetRecordsRestrictionWithoutCompletingGlobalData(t *testing.T) {
	for _, facet := range []string{PhotoInfoFacet, PhotoExifFacet, PhotoContextsFacet, PhotoCommentsFacet, PhotoFavoritesFacet} {
		for _, cause := range []error{flickr.ErrNotFound, flickr.ErrPermission} {
			t.Run(facet+cause.Error(), func(t *testing.T) {
				states := &stateFake{}
				store := &photoFacetStoreFake{source: catalog.PhotoFacetSource{LastUpdate: epoch, SizesConfirmed: true}, exists: true}
				client := &photoFacetClientFake{infoErr: cause, exifErr: cause, contextsErr: cause, commentsErr: cause, favoritesErr: cause}
				h := PhotoFacets{Store: store, Flickr: client, States: states}
				job := PhotoFacetJob{PhotoID: 42, KeyID: 7, Facet: facet}
				if err := h.Handle(t.Context(), job); err != nil {
					t.Fatal(err)
				}
				if len(states.recorded) != 1 || states.state != "restricted" || !states.recorded[0].Version.Equal(epoch) || store.lastFacet != "" {
					t.Fatalf("%+v", states)
				}
				states.blocked = true
				if err := h.Handle(t.Context(), job); err != nil {
					t.Fatal(err)
				}
				if len(states.recorded) != 1 {
					t.Fatal("restricted facet fetched again")
				}
			})
		}
	}
	for _, cause := range []error{flickr.ErrNotFound, flickr.ErrPermission} {
		h, store, client := sizesRig()
		states := &stateFake{}
		h.States = states
		h.Users = staticKeyUser{userID: 7}
		h.Auth = &sizesAuthFake{}
		h.Accounts = &accountSizesFake{}
		client.err = cause
		if err := h.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12}); err != nil {
			t.Fatal(err)
		}
		if states.state != "restricted" || store.status == catalog.SizesUnavailable {
			t.Fatal("sizes restriction completed global sizes instead of recording scoped access")
		}
		states.blocked = true
		if err := h.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12}); err != nil {
			t.Fatal(err)
		}
		if client.calls != 1 {
			t.Fatal("repeated sizes")
		}
	}
}

func TestEntityStateErrorsAreRetried(t *testing.T) {
	failure := errors.New("database failed")
	r := newRig(t)
	r.engine.States = &stateFake{err: failure}
	if !errors.Is(r.engine.Handle(t.Context(), 7), failure) {
		t.Fatal("lost read error")
	}
	h, _, sizesClient := sizesRig()
	h.States = &stateFake{err: failure}
	sizesClient.err = flickr.ErrPermission
	if !errors.Is(h.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12}), failure) {
		t.Fatal("lost sizes error")
	}
	if err := recordEntityFailure(t.Context(), nil, EntityAccess{}, flickr.ErrNotFound, epoch, 0); err != nil {
		t.Fatal(err)
	}
	states := &stateFake{}
	if err := recordEntityFailure(t.Context(), states, EntityAccess{}, errors.New("transient"), epoch, 0); err != nil || len(states.recorded) != 0 {
		t.Fatal("recorded transient")
	}
}

func TestRestrictedCheckAdvancesOtherProbe(t *testing.T) {
	r := newRig(t)
	if err := r.engine.Registry.Register(ChangeCheck{}); err != nil {
		t.Fatal(err)
	}
	r.store.item.Kind = "check"
	states := &stateFake{blocked: true}
	r.engine.States = states
	if err := r.engine.Handle(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	if len(r.flickr.requests) != 0 {
		t.Fatal("restricted check called Flickr")
	}
	if r.tx.advanced == nil || r.tx.advanced.Method != "flickr.favorites.getList" {
		t.Fatalf("did not advance: %+v", r.tx)
	}
}

func TestContactsSkipsGoneEntities(t *testing.T) {
	r := newRig(t)
	registry := NewRegistry()
	if err := registry.Register(&fakeFacet{name: "contacts", lane: domain.LanePerson}); err != nil {
		t.Fatal(err)
	}
	r.engine.Registry = registry
	r.store.item.Kind = "contacts"
	r.store.item.Cursor.Method = "flickr.contacts.getList"
	r.store.item.Cursor.Params = map[string]string{"user_id": "gone@N00", "per_page": "500"}
	r.engine.States = &gonePersonStates{id: "gone@N00"}
	if err := r.engine.Handle(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	if len(r.flickr.requests) != 0 {
		t.Fatalf("gone contacts account still called Flickr: %+v", r.flickr.requests)
	}
	if !slices.Contains(r.store.calls, "finish") {
		t.Fatalf("gone contacts work was not finished: %v", r.store.calls)
	}
}

type gonePersonStates struct{ id string }

func (s gonePersonStates) Suppressed(_ context.Context, a EntityAccess, _ time.Time) (bool, error) {
	return a.Kind == "person" && a.ID == s.id, nil
}

func (s gonePersonStates) RecordFailure(context.Context, EntityAccess, string, string, time.Time, time.Time) error {
	return nil
}

func TestEngineMissingEntityUsesAddressedID(t *testing.T) {
	for _, tc := range []struct{ kind, param string }{{"album", "photoset_id"}, {"gallery", "gallery_id"}, {"group", "group_id"}} {
		t.Run(tc.kind, func(t *testing.T) {
			r := newRig(t)
			states := &stateFake{}
			r.engine.States = states
			r.store.item.Cursor.Params = map[string]string{tc.param: "entity-id", "user_id": "owner"}
			r.flickr.err = flickr.ErrNotFound
			if err := r.engine.Handle(t.Context(), 7); err != nil {
				t.Fatal(err)
			}
			if len(states.recorded) != 1 || states.recorded[0].Kind != tc.kind || states.recorded[0].ID != "entity-id" || states.state != "gone" {
				t.Fatalf("%+v", states)
			}
		})
	}
}
