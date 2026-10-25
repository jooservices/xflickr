package http

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/quota/app"
	"github.com/jooservices/xflickr/internal/quota/domain"
)

type fakeStates struct {
	state    app.KeyState
	all      []app.KeyState
	err      error
	gotUser  int64
	gotKeyID int64
}

func (f *fakeStates) ForOwner(_ context.Context, userID, keyID int64) (app.KeyState, error) {
	f.gotUser, f.gotKeyID = userID, keyID
	return f.state, f.err
}

func (f *fakeStates) All(context.Context) ([]app.KeyState, error) { return f.all, f.err }

func actor(id int64, ok bool) func(context.Context) (int64, bool) {
	return func(context.Context) (int64, bool) { return id, ok }
}

var reset = time.Date(2026, 10, 25, 13, 0, 0, 0, time.UTC)

func TestGetKeyQuotaMapsState(t *testing.T) {
	states := &fakeStates{state: app.KeyState{
		Key: app.KeyInfo{ID: 5}, Used: 7, Limit: 100, Reserve: 10, ResetAt: reset, State: domain.StateCooling,
		Lanes: app.LaneUsage{Interactive: 1, Person: 2, Photo: 4},
	}}
	response, err := QuotaHandler{States: states, ActorID: actor(9, true)}.GetKeyQuota(t.Context(), gen.GetKeyQuotaRequestObject{ID: 5})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := response.(gen.GetKeyQuota200JSONResponse)
	if !ok {
		t.Fatalf("response = %T", response)
	}
	if states.gotUser != 9 || states.gotKeyID != 5 {
		t.Fatalf("service called with user=%d key=%d", states.gotUser, states.gotKeyID)
	}
	if got.KeyID != 5 || got.Used != 7 || got.Limit != 100 || got.Reserve != 10 || got.State != gen.KeyQuotaStateCooling ||
		got.ResetAt == nil || !got.ResetAt.Equal(reset) || got.Lanes != (gen.LaneUsage{Interactive: 1, Person: 2, Photo: 4}) {
		t.Fatalf("body = %+v", got)
	}
}

func TestGetKeyQuotaOmitsResetWithoutWindow(t *testing.T) {
	response, err := QuotaHandler{States: &fakeStates{state: app.KeyState{State: domain.StateOK}}, ActorID: actor(1, true)}.GetKeyQuota(t.Context(), gen.GetKeyQuotaRequestObject{ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got := response.(gen.GetKeyQuota200JSONResponse); got.ResetAt != nil {
		t.Fatalf("reset_at = %v", got.ResetAt)
	}
}

func TestGetKeyQuotaErrors(t *testing.T) {
	request := gen.GetKeyQuotaRequestObject{ID: 1}
	if response, err := (QuotaHandler{States: &fakeStates{}}).GetKeyQuota(t.Context(), request); err != nil {
		t.Fatal(err)
	} else if _, ok := response.(gen.GetKeyQuota401JSONResponse); !ok {
		t.Fatalf("unauthenticated = %T", response)
	}
	if response, err := (QuotaHandler{States: &fakeStates{}, ActorID: actor(0, false)}).GetKeyQuota(t.Context(), request); err != nil {
		t.Fatal(err)
	} else if _, ok := response.(gen.GetKeyQuota401JSONResponse); !ok {
		t.Fatalf("no actor = %T", response)
	}
	notFound := QuotaHandler{States: &fakeStates{err: app.ErrKeyNotFound}, ActorID: actor(1, true)}
	if response, err := notFound.GetKeyQuota(t.Context(), request); err != nil {
		t.Fatal(err)
	} else if _, ok := response.(gen.GetKeyQuota404JSONResponse); !ok {
		t.Fatalf("not found = %T", response)
	}
	boom := errors.New("boom")
	if _, err := (QuotaHandler{States: &fakeStates{err: boom}, ActorID: actor(1, true)}).GetKeyQuota(t.Context(), request); !errors.Is(err, boom) {
		t.Fatalf("service error = %v", err)
	}
	if _, err := (QuotaHandler{ActorID: actor(1, true)}).GetKeyQuota(t.Context(), request); err == nil {
		t.Fatal("missing service accepted")
	}
}

func TestGetAdminQuotaMapsKeys(t *testing.T) {
	states := &fakeStates{all: []app.KeyState{
		{Key: app.KeyInfo{ID: 1, UserID: 4, Label: "main"}, Used: 3, Limit: 10, ResetAt: reset, State: domain.StateExhausted, Trend: domain.TrendFalling},
		{Key: app.KeyInfo{ID: 2, UserID: 5, Label: "idle"}, Limit: 10, State: domain.StateOK, Trend: domain.TrendSteady},
	}}
	response, err := QuotaHandler{States: states, Node: "node-a", ActorID: actor(1, true)}.GetAdminQuota(t.Context(), gen.GetAdminQuotaRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	got := response.(gen.GetAdminQuota200JSONResponse)
	if len(got.Keys) != 2 {
		t.Fatalf("keys = %+v", got.Keys)
	}
	first := got.Keys[0]
	if first.KeyID != 1 || first.Label != "main" || first.Node != "node-a" || first.Used != 3 || first.Limit != 10 ||
		first.State != gen.AdminKeyQuotaStateExhausted || first.Trend != gen.Falling || first.UserID == nil || *first.UserID != 4 ||
		first.ResetAt == nil || !first.ResetAt.Equal(reset) {
		t.Fatalf("first = %+v", first)
	}
	if got.Keys[1].ResetAt != nil {
		t.Fatalf("idle key has reset_at %v", got.Keys[1].ResetAt)
	}
}

func TestGetAdminQuotaEmptyAndErrors(t *testing.T) {
	response, err := QuotaHandler{States: &fakeStates{}, ActorID: actor(1, true)}.GetAdminQuota(t.Context(), gen.GetAdminQuotaRequestObject{})
	if err != nil || response.(gen.GetAdminQuota200JSONResponse).Keys == nil {
		t.Fatalf("empty = %+v %v", response, err)
	}
	if response, err := (QuotaHandler{States: &fakeStates{}}).GetAdminQuota(t.Context(), gen.GetAdminQuotaRequestObject{}); err != nil {
		t.Fatal(err)
	} else if _, ok := response.(gen.GetAdminQuota401JSONResponse); !ok {
		t.Fatalf("unauthenticated = %T", response)
	}
	boom := errors.New("boom")
	if _, err := (QuotaHandler{States: &fakeStates{err: boom}, ActorID: actor(1, true)}).GetAdminQuota(t.Context(), gen.GetAdminQuotaRequestObject{}); !errors.Is(err, boom) {
		t.Fatalf("service error = %v", err)
	}
	if _, err := (QuotaHandler{ActorID: actor(1, true)}).GetAdminQuota(t.Context(), gen.GetAdminQuotaRequestObject{}); err == nil {
		t.Fatal("missing service accepted")
	}
}
