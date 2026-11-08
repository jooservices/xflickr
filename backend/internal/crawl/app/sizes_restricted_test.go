package app

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
	crawldomain "github.com/jooservices/xflickr/internal/crawl/domain"
	quotadomain "github.com/jooservices/xflickr/internal/quota/domain"
	"github.com/jooservices/xflickr/pkg/flickr"
)

type restrictedSizesClient struct {
	anonymousErr error
	accountSizes map[int64][]flickr.PhotoSize
	accountErr   map[int64]error
	calls        []sizesCall
}

type sizesCall struct {
	KeyID, AccountID int64
	PhotoID          string
	Class            quotadomain.Class
	Signed           bool
}

func (f *restrictedSizesClient) GetSizes(_ context.Context, keyID int64, photoID string) ([]flickr.PhotoSize, error) {
	f.calls = append(f.calls, sizesCall{KeyID: keyID, PhotoID: photoID, Class: quotadomain.Photo})
	if f.anonymousErr != nil {
		return nil, f.anonymousErr
	}
	return []flickr.PhotoSize{{Label: "Original", Width: 1, Height: 1}}, nil
}

func (f *restrictedSizesClient) GetSizesClass(ctx context.Context, keyID int64, photoID string, class quotadomain.Class) ([]flickr.PhotoSize, error) {
	sizes, err := f.GetSizes(ctx, keyID, photoID)
	if len(f.calls) > 0 {
		f.calls[len(f.calls)-1].Class = class
	}
	return sizes, err
}

func (f *restrictedSizesClient) GetSizesForAccount(_ context.Context, keyID, accountID int64, photoID string, class quotadomain.Class) ([]flickr.PhotoSize, error) {
	f.calls = append(f.calls, sizesCall{KeyID: keyID, AccountID: accountID, PhotoID: photoID, Class: class, Signed: true})
	if err := f.accountErr[accountID]; err != nil {
		return nil, err
	}
	if sizes := f.accountSizes[accountID]; len(sizes) > 0 {
		return sizes, nil
	}
	return nil, errors.New("crawl: missing account sizes fixture")
}

type sizesAuthFake struct {
	candidates []crawldomain.SizesAuthCandidate
	err        error
	calls      int
	ownerNSID  string
	userID     int64
}

func (f *sizesAuthFake) SizesAuthCandidates(_ context.Context, ownerNSID string, requestingUserID int64) ([]crawldomain.SizesAuthCandidate, error) {
	f.calls++
	f.ownerNSID, f.userID = ownerNSID, requestingUserID
	return append([]crawldomain.SizesAuthCandidate(nil), f.candidates...), f.err
}

type restrictionStates struct {
	blocked  map[string]bool
	recorded []EntityAccess
}

func restrictionKey(userID, keyID int64) string {
	return fmt.Sprintf("%d:%d", userID, keyID)
}

func (s *restrictionStates) Suppressed(_ context.Context, a EntityAccess, _ time.Time) (bool, error) {
	return s.blocked[restrictionKey(a.UserID, a.KeyID)], nil
}

func (s *restrictionStates) RecordFailure(_ context.Context, a EntityAccess, _, _ string, _, _ time.Time) error {
	s.recorded = append(s.recorded, a)
	s.blocked[restrictionKey(a.UserID, a.KeyID)] = true
	return nil
}

func restrictedRig() (Sizes, *sizesStoreFake, *restrictedSizesClient, *sizesAuthFake, *restrictionStates) {
	checked := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	store := &sizesStoreFake{exists: true, photo: catalog.Photo{
		ID: 40, OwnerNSID: "owner@N00", LastUpdate: checked.Add(-time.Hour), SizesStatus: catalog.SizesProvisional,
	}}
	client := &restrictedSizesClient{
		anonymousErr: &flickr.CallError{Kind: flickr.ErrNotFound, Code: 1},
		accountSizes: map[int64][]flickr.PhotoSize{
			11: {{Label: "Original", Width: 100, Height: 80, Source: "https://example.test/original"}},
		},
	}
	auth := &sizesAuthFake{candidates: []crawldomain.SizesAuthCandidate{
		{AccountID: 11, UserID: 7, KeyID: 12, NSID: "owner@N00", Owner: true},
		{AccountID: 22, UserID: 7, KeyID: 13, NSID: "viewer@N00", Owner: false},
	}}
	states := &restrictionStates{blocked: map[string]bool{}}
	handler := Sizes{
		States: states, Store: store, Flickr: client, Auth: auth, Users: staticKeyUser{userID: 7},
		Now: func() time.Time { return checked },
	}
	return handler, store, client, auth, states
}

type staticKeyUser struct{ userID int64 }

func (s staticKeyUser) UserIDForKey(context.Context, int64) (int64, error) {
	return s.userID, nil
}

func TestRestrictedPhotoRetriesOnceWithOwnerToken(t *testing.T) {
	handler, store, client, auth, _ := restrictedRig()
	if err := handler.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12}); err != nil {
		t.Fatal(err)
	}
	if auth.calls != 1 || auth.ownerNSID != "owner@N00" || auth.userID != 7 {
		t.Fatalf("auth lookup = %+v", auth)
	}
	if len(client.calls) != 2 || client.calls[0].Signed || !client.calls[1].Signed || client.calls[1].AccountID != 11 {
		t.Fatalf("calls = %+v", client.calls)
	}
	if store.status != catalog.SizesConfirmed {
		t.Fatalf("status = %s", store.status)
	}
}

func TestFailureRecordsRestrictionForThatAccountOnly(t *testing.T) {
	handler, store, client, _, states := restrictedRig()
	client.accountErr = map[int64]error{11: &flickr.CallError{Kind: flickr.ErrNotFound, Code: 1}}
	delete(client.accountSizes, 11)
	if err := handler.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12}); err != nil {
		t.Fatal(err)
	}
	if store.status != catalog.SizesUnavailable || len(states.recorded) != 1 {
		t.Fatalf("status=%s recorded=%+v", store.status, states.recorded)
	}
	got := states.recorded[0]
	if got.UserID != 7 || got.KeyID != 12 || got.Facet != "sizes" || got.ID != "40" {
		t.Fatalf("restriction = %+v", got)
	}
	if len(client.calls) != 2 {
		t.Fatalf("calls = %+v", client.calls)
	}
}

func TestNoCallForAlreadyRestrictedAccount(t *testing.T) {
	handler, store, client, _, states := restrictedRig()
	store.photo.Server = catalog.RestrictedPlaceholderServer
	states.blocked[restrictionKey(7, 12)] = true
	if err := handler.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12}); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 0 || store.status != catalog.SizesUnavailable {
		t.Fatalf("calls=%d status=%s", len(client.calls), store.status)
	}
}

func TestOwnerAccountPreferredThenRequestingUserAccounts(t *testing.T) {
	handler, store, client, auth, states := restrictedRig()
	store.photo.Server = catalog.RestrictedPlaceholderServer
	states.blocked[restrictionKey(7, 12)] = true
	client.accountSizes[22] = []flickr.PhotoSize{{Label: "Large", Width: 50, Height: 40}}
	if err := handler.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12, Class: string(quotadomain.Interactive)}); err != nil {
		t.Fatal(err)
	}
	if auth.calls != 1 || len(client.calls) != 1 || !client.calls[0].Signed || client.calls[0].AccountID != 22 {
		t.Fatalf("preferred account calls=%+v auth=%+v", client.calls, auth)
	}
	if store.status != catalog.SizesConfirmed {
		t.Fatalf("status = %s", store.status)
	}
}

func TestNoRetryLoop(t *testing.T) {
	handler, store, client, _, states := restrictedRig()
	store.photo.Server = catalog.RestrictedPlaceholderServer
	client.accountErr = map[int64]error{11: &flickr.CallError{Kind: flickr.ErrPermission, Code: 2}}
	delete(client.accountSizes, 11)
	job := SizesJob{PhotoID: 40, KeyID: 12}
	if err := handler.Handle(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	store.photo.SizesStatus = store.status
	checked := store.checked
	store.photo.SizesCheckedAt = &checked
	for range 3 {
		if err := handler.Handle(t.Context(), job); err != nil {
			t.Fatal(err)
		}
	}
	signed := 0
	for _, call := range client.calls {
		if call.Signed {
			signed++
		}
	}
	if signed != 1 || len(states.recorded) != 1 {
		t.Fatalf("signed calls=%d recorded=%d all=%+v", signed, len(states.recorded), client.calls)
	}
}
