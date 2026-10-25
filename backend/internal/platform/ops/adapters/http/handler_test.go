package http

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/ops"
)

type fakeService struct {
	status ops.Status
	err    error
}

func (f fakeService) Status(context.Context) (ops.Status, error) { return f.status, f.err }

func actor(context.Context) (int64, bool)   { return 1, true }
func noActor(context.Context) (int64, bool) { return 0, false }

func TestGetAdminOpsStatus(t *testing.T) {
	generated := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	handler := Handler{ActorID: actor, Service: fakeService{status: ops.Status{
		GeneratedAt:      generated,
		Counts:           ops.Counts{People: 1, Photos: 2, PhotoSizes: 3, Favorites: 4, Albums: 5, Galleries: 6, Groups: 7},
		WorkItems:        map[string]int64{"pending": 2},
		ReadyJobs:        0,
		PhotoSizesJobs:   ops.LaneJobs{Pending: 8, Ready: 2, Running: 1},
		Keys:             []ops.KeyStatus{{KeyID: 9, Label: "k", Status: "valid", CallsLastHour: 40, Limit: 100}},
		OldestPendingAge: 2 * time.Minute,
		TokenStatus:      ops.TokenOK,
		IdleReason:       ops.ReasonNoKey,
		PendingItems:     2,
	}}}
	response, err := handler.GetAdminOpsStatus(t.Context(), gen.GetAdminOpsStatusRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	ok, isOK := response.(gen.GetAdminOpsStatus200JSONResponse)
	if !isOK {
		t.Fatalf("response=%T", response)
	}
	if ok.Counts.PhotoSizes != 3 || ok.PhotoSizesPending != 8 || ok.PhotoSizesReady != 2 || ok.PhotoSizesRunning != 1 || ok.OldestPendingAgeSeconds != 120 || ok.TokenStatus != gen.OpsStatusTokenStatusOk || ok.PendingItems != 2 {
		t.Fatalf("body=%+v", ok)
	}
	if len(ok.Keys) != 1 || ok.Keys[0].Remaining != 60 || ok.Keys[0].Used != 40 || ok.IdleReason == nil || *ok.IdleReason != gen.NoKey {
		t.Fatalf("keys=%+v idle=%v", ok.Keys, ok.IdleReason)
	}
}

func TestGetAdminOpsStatusOmitsIdleReason(t *testing.T) {
	response, err := Handler{ActorID: actor, Service: fakeService{}}.GetAdminOpsStatus(t.Context(), gen.GetAdminOpsStatusRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	if body := response.(gen.GetAdminOpsStatus200JSONResponse); body.IdleReason != nil {
		t.Fatalf("idle reason=%v", *body.IdleReason)
	}
}

func TestGetAdminOpsStatusErrors(t *testing.T) {
	for _, handler := range []Handler{{Service: fakeService{}}, {ActorID: noActor, Service: fakeService{}}} {
		response, err := handler.GetAdminOpsStatus(t.Context(), gen.GetAdminOpsStatusRequestObject{})
		if _, ok := response.(gen.GetAdminOpsStatus401JSONResponse); !ok || err != nil {
			t.Fatalf("response=%T err=%v", response, err)
		}
	}
	if _, err := (Handler{ActorID: actor}).GetAdminOpsStatus(t.Context(), gen.GetAdminOpsStatusRequestObject{}); err == nil {
		t.Fatal("expected missing service error")
	}
	want := errors.New("db down")
	if _, err := (Handler{ActorID: actor, Service: fakeService{err: want}}).GetAdminOpsStatus(t.Context(), gen.GetAdminOpsStatusRequestObject{}); !errors.Is(err, want) {
		t.Fatalf("err=%v", err)
	}
}
