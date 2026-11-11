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

type sizesUserMap map[int64]int64

func (m sizesUserMap) UserIDForKey(_ context.Context, keyID int64) (int64, error) {
	return m[keyID], nil
}

type accountSizesFake struct {
	statuses map[int64]catalog.SizesStatus
	written  []struct {
		PhotoID, AccountID, UserID int64
		Status                     catalog.SizesStatus
		Sizes                      []byte
	}
	resolved []struct {
		PhotoID, UserID int64
		Status          catalog.SizesStatus
	}
}

func (s *accountSizesFake) AccountSizes(_ context.Context, _, accountID int64, _ time.Time, _ string) (catalog.SizesStatus, bool, error) {
	status, ok := s.statuses[accountID]
	return status, ok, nil
}

func (s *accountSizesFake) SetAccountSizes(_ context.Context, photoID, accountID, userID int64, sizes []byte, status catalog.SizesStatus, _ time.Time, _ string, _ time.Time) error {
	if s.statuses == nil {
		s.statuses = make(map[int64]catalog.SizesStatus)
	}
	s.statuses[accountID] = status
	s.written = append(s.written, struct {
		PhotoID, AccountID, UserID int64
		Status                     catalog.SizesStatus
		Sizes                      []byte
	}{photoID, accountID, userID, status, append([]byte(nil), sizes...)})
	return nil
}

func (s *accountSizesFake) ResolveUserSizes(_ context.Context, photoID, userID int64, status catalog.SizesStatus) error {
	s.resolved = append(s.resolved, struct {
		PhotoID, UserID int64
		Status          catalog.SizesStatus
	}{photoID, userID, status})
	return nil
}

func restrictionKey(access EntityAccess) string {
	if access.AccountID > 0 {
		return fmt.Sprintf("account:%d", access.AccountID)
	}
	return fmt.Sprintf("%d:%d", access.UserID, access.KeyID)
}

func (s *restrictionStates) Suppressed(_ context.Context, a EntityAccess, _ time.Time) (bool, error) {
	return s.blocked[restrictionKey(a)], nil
}

func (s *restrictionStates) RecordFailure(_ context.Context, a EntityAccess, _, _ string, _, _ time.Time) error {
	s.recorded = append(s.recorded, a)
	s.blocked[restrictionKey(a)] = true
	return nil
}

func restrictedRig() (Sizes, *sizesStoreFake, *restrictedSizesClient, *sizesAuthFake, *restrictionStates) {
	checked := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	store := &sizesStoreFake{exists: true, photo: catalog.Photo{
		ID: 40, OwnerNSID: "owner@N00", LastUpdate: checked.Add(-time.Hour), Visibility: catalog.VisibilityPublic, SizesStatus: catalog.SizesProvisional,
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
		States: states, Store: store, Accounts: &accountSizesFake{}, Flickr: client, Auth: auth, Users: staticKeyUser{userID: 7},
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
	if store.status == catalog.SizesConfirmed {
		t.Fatal("account-authorized sizes were stored globally")
	}
	if len(handler.Accounts.(*accountSizesFake).written) != 1 || handler.Accounts.(*accountSizesFake).written[0].AccountID != 11 || handler.Accounts.(*accountSizesFake).written[0].Status != catalog.SizesConfirmed {
		t.Fatalf("account sizes = %+v", handler.Accounts)
	}
}

func TestFailedOwnerAccountDoesNotSuppressRequestingAccountOnNextRequest(t *testing.T) {
	handler, store, client, _, _ := restrictedRig()
	store.photo.Server = catalog.RestrictedPlaceholderServer
	handler.Auth.(*sizesAuthFake).candidates[1].KeyID = handler.Auth.(*sizesAuthFake).candidates[0].KeyID
	client.accountErr = map[int64]error{11: &flickr.CallError{Kind: flickr.ErrPermission, Code: 2}}
	delete(client.accountSizes, 11)
	client.accountSizes[22] = []flickr.PhotoSize{{Label: "Large", Width: 50, Height: 40}}
	job := SizesJob{PhotoID: 40, KeyID: 12, Class: string(quotadomain.Interactive)}
	if err := handler.Handle(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	store.photo.SizesStatus = store.status
	if store.checked.IsZero() {
		store.checked = handler.Now()
	}
	if err := handler.Handle(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	if got := client.calls[len(client.calls)-1]; !got.Signed || got.AccountID != 22 {
		t.Fatalf("next request call = %+v, want requesting user's other account", got)
	}
}

func TestRestrictedSizesFallsBackToNextEligibleAccount(t *testing.T) {
	handler, store, client, _, _ := restrictedRig()
	store.photo.Server = catalog.RestrictedPlaceholderServer
	client.accountErr = map[int64]error{11: &flickr.CallError{Kind: flickr.ErrPermission, Code: 2}}
	delete(client.accountSizes, 11)
	client.accountSizes[22] = []flickr.PhotoSize{{Label: "Large", Width: 50, Height: 40}}

	if err := handler.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12, Class: string(quotadomain.Interactive)}); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 2 || client.calls[0].AccountID != 11 || client.calls[1].AccountID != 22 || !client.calls[0].Signed || !client.calls[1].Signed {
		t.Fatalf("authenticated calls = %+v, want owner then next eligible account", client.calls)
	}
	accounts := handler.Accounts.(*accountSizesFake)
	if accounts.statuses[11] != catalog.SizesUnavailable || accounts.statuses[22] != catalog.SizesConfirmed {
		t.Fatalf("account outcomes = %v, want owner unavailable and next account confirmed", accounts.statuses)
	}
	if len(accounts.resolved) != 0 {
		t.Fatalf("user resolutions = %+v, want no unavailable outcome after a later account succeeds", accounts.resolved)
	}
}

func TestRestrictedSizesResolvesUnavailableAfterAllEligibleAccountsFail(t *testing.T) {
	handler, store, client, _, _ := restrictedRig()
	store.photo.Server = catalog.RestrictedPlaceholderServer
	denied := &flickr.CallError{Kind: flickr.ErrPermission, Code: 2}
	client.accountErr = map[int64]error{11: denied, 22: denied}
	delete(client.accountSizes, 11)

	if err := handler.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12, Class: string(quotadomain.Interactive)}); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 2 || client.calls[0].AccountID != 11 || client.calls[1].AccountID != 22 {
		t.Fatalf("authenticated calls = %+v, want all eligible accounts in order", client.calls)
	}
	accounts := handler.Accounts.(*accountSizesFake)
	if len(accounts.resolved) != 1 || accounts.resolved[0].UserID != 7 || accounts.resolved[0].Status != catalog.SizesUnavailable {
		t.Fatalf("user resolutions = %+v, want one unavailable result after exhausting accounts", accounts.resolved)
	}
}

func TestConfirmedGlobalSizesRetryAfterServerOrVisibilityBecomesRestricted(t *testing.T) {
	checked := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name       string
		server     string
		visibility catalog.Visibility
	}{
		{name: "placeholder server", server: catalog.RestrictedPlaceholderServer, visibility: catalog.VisibilityPublic},
		{name: "nonpublic visibility", server: "1", visibility: catalog.VisibilityPrivate},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler, store, client, _, _ := restrictedRig()
			store.photo.Server = test.server
			store.photo.Visibility = test.visibility
			store.photo.SizesStatus = catalog.SizesConfirmed
			store.photo.Sizes = []byte(`[{"label":"Large","source":"https://example.test/old"}]`)
			store.photo.SizesCheckedAt = &checked

			if err := handler.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12}); err != nil {
				t.Fatal(err)
			}
			if len(client.calls) != 1 || !client.calls[0].Signed || client.calls[0].AccountID != 11 {
				t.Fatalf("calls = %+v, want one authenticated request for the owner", client.calls)
			}
		})
	}
}

func TestFailureRecordsRestrictionForThatAccountOnly(t *testing.T) {
	handler, store, client, _, states := restrictedRig()
	client.accountErr = map[int64]error{11: &flickr.CallError{Kind: flickr.ErrNotFound, Code: 1}}
	delete(client.accountSizes, 11)
	if err := handler.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12}); err != nil {
		t.Fatal(err)
	}
	if store.status == catalog.SizesUnavailable || len(states.recorded) != 2 {
		t.Fatalf("status=%s recorded=%+v", store.status, states.recorded)
	}
	got := states.recorded[1]
	if got.UserID != 7 || got.KeyID != 12 || got.AccountID != 11 || got.Facet != "sizes" || got.ID != "40" {
		t.Fatalf("restriction = %+v", got)
	}
	if len(client.calls) != 2 {
		t.Fatalf("calls = %+v", client.calls)
	}
}

func TestAnonymousSizeRestrictionIsScopedToTheRequestingUserAndKey(t *testing.T) {
	handler, store, client := sizesRig()
	client.err = &flickr.CallError{Kind: flickr.ErrPermission, Code: 2}
	states := &restrictionStates{blocked: map[string]bool{}}
	handler.States = states
	handler.Users = sizesUserMap{12: 7, 13: 8}
	handler.Auth = &sizesAuthFake{}
	handler.Accounts = &accountSizesFake{}

	for _, keyID := range []int64{12, 12, 13} {
		if err := handler.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: keyID}); err != nil {
			t.Fatal(err)
		}
	}
	if client.calls != 2 || !states.blocked["7:12"] || !states.blocked["8:13"] {
		t.Fatalf("calls=%d restrictions=%v", client.calls, states.blocked)
	}
	if store.status == catalog.SizesUnavailable || len(handler.Accounts.(*accountSizesFake).resolved) != 3 {
		t.Fatalf("global status=%s user resolutions=%+v", store.status, handler.Accounts.(*accountSizesFake).resolved)
	}
}

func TestNoCallForAlreadyRestrictedAccount(t *testing.T) {
	handler, store, client, _, states := restrictedRig()
	store.photo.Server = catalog.RestrictedPlaceholderServer
	states.blocked[restrictionKey(EntityAccess{AccountID: 11})] = true
	if err := handler.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12}); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 0 || store.status == catalog.SizesUnavailable {
		t.Fatalf("calls=%d status=%s", len(client.calls), store.status)
	}
}

func TestOwnerAccountPreferredThenRequestingUserAccounts(t *testing.T) {
	handler, store, client, auth, states := restrictedRig()
	store.photo.Server = catalog.RestrictedPlaceholderServer
	states.blocked[restrictionKey(EntityAccess{AccountID: 11})] = true
	client.accountSizes[22] = []flickr.PhotoSize{{Label: "Large", Width: 50, Height: 40}}
	if err := handler.Handle(t.Context(), SizesJob{PhotoID: 40, KeyID: 12, Class: string(quotadomain.Interactive)}); err != nil {
		t.Fatal(err)
	}
	if auth.calls != 1 || len(client.calls) != 1 || !client.calls[0].Signed || client.calls[0].AccountID != 22 {
		t.Fatalf("preferred account calls=%+v auth=%+v", client.calls, auth)
	}
	if accountStatus := handler.Accounts.(*accountSizesFake).statuses[22]; accountStatus != catalog.SizesConfirmed {
		t.Fatalf("account status = %s", accountStatus)
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
