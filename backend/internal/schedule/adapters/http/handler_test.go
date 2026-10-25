package http

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/schedule/app"
	"github.com/jooservices/xflickr/internal/schedule/domain"
)

type serviceStub struct {
	plans                          []domain.Plan
	listErr, replaceErr, assignErr error
	actor, user, plan              int64
}

type queueServiceStub struct {
	lanes []app.LaneEstimate
	admin app.AdminQueue
	err   error
	user  int64
}

func (s *queueServiceStub) ForUser(_ context.Context, userID int64) ([]app.LaneEstimate, error) {
	s.user = userID
	return s.lanes, s.err
}

func (s *queueServiceStub) ForAdmin(context.Context) (app.AdminQueue, error) { return s.admin, s.err }

func (s *serviceStub) List(context.Context) ([]domain.Plan, error) { return s.plans, s.listErr }
func (s *serviceStub) Replace(_ context.Context, actor int64, p []domain.Plan) error {
	s.actor = actor
	s.plans = p
	return s.replaceErr
}

func (s *serviceStub) Assign(_ context.Context, actor, user, plan int64) error {
	s.actor = actor
	s.user = user
	s.plan = plan
	return s.assignErr
}
func actorOK(context.Context) (int64, bool) { return 7, true }

func TestGetMeCrawlQueue(t *testing.T) {
	ctx := context.Background()
	if _, err := (Handler{}).GetMeCrawlQueue(ctx, gen.GetMeCrawlQueueRequestObject{}); err != nil {
		t.Fatal(err)
	}
	h := Handler{ActorID: actorOK}
	if _, err := h.GetMeCrawlQueue(ctx, gen.GetMeCrawlQueueRequestObject{}); err == nil {
		t.Fatal("missing queue service accepted")
	}
	eta := 2.5
	svc := &queueServiceStub{lanes: []app.LaneEstimate{{Lane: "person", BacklogCalls: 5, EffectiveCallsPerHour: 2, ETAHours: &eta}}}
	h.Queue = svc
	response, err := h.GetMeCrawlQueue(ctx, gen.GetMeCrawlQueueRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	got := response.(gen.GetMeCrawlQueue200JSONResponse)
	if svc.user != 7 || len(got.Lanes) != 1 || got.Lanes[0].Lane != "person" || got.Lanes[0].EtaHours == nil || *got.Lanes[0].EtaHours != eta {
		t.Fatalf("response=%+v user=%d", got, svc.user)
	}
	svc.err = errors.New("database")
	if _, err := h.GetMeCrawlQueue(ctx, gen.GetMeCrawlQueueRequestObject{}); err == nil {
		t.Fatal("queue error swallowed")
	}
}

func TestGetAdminQueue(t *testing.T) {
	ctx := context.Background()
	if _, err := (Handler{}).GetAdminQueue(ctx, gen.GetAdminQueueRequestObject{}); err != nil {
		t.Fatal(err)
	}
	h := Handler{ActorID: actorOK}
	if _, err := h.GetAdminQueue(ctx, gen.GetAdminQueueRequestObject{}); err == nil {
		t.Fatal("missing queue service accepted")
	}
	svc := &queueServiceStub{admin: app.AdminQueue{
		BacklogByPlan:        []app.PlanLaneBacklog{{Plan: "pro", Lane: "sizes", BacklogCalls: 3}},
		OldestItemAgeSeconds: 42,
		ReadyQueueSizes:      []app.ReadyQueueSize{{Queue: "sync", ReadyJobs: 9}},
	}}
	h.Queue = svc
	response, err := h.GetAdminQueue(ctx, gen.GetAdminQueueRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	got := response.(gen.GetAdminQueue200JSONResponse)
	if len(got.BacklogByPlan) != 1 || got.BacklogByPlan[0].Plan != "pro" || got.BacklogByPlan[0].Lane != "sizes" || got.OldestItemAgeSeconds != 42 || len(got.ReadyQueueSizes) != 1 || got.ReadyQueueSizes[0].ReadyJobs != 9 {
		t.Fatalf("response=%+v", got)
	}
	svc.err = errors.New("database")
	if _, err := h.GetAdminQueue(ctx, gen.GetAdminQueueRequestObject{}); err == nil {
		t.Fatal("queue error swallowed")
	}
}

func TestGetAdminPlans(t *testing.T) {
	if _, err := (Handler{}).GetAdminPlans(context.Background(), gen.GetAdminPlansRequestObject{}); err != nil {
		t.Fatal(err)
	}
	h := Handler{ActorID: func(context.Context) (int64, bool) { return 0, false }}
	if _, err := h.GetAdminPlans(context.Background(), gen.GetAdminPlansRequestObject{}); err != nil {
		t.Fatal(err)
	}
	h = Handler{ActorID: actorOK}
	if _, err := h.GetAdminPlans(context.Background(), gen.GetAdminPlansRequestObject{}); err == nil {
		t.Fatal("missing service accepted")
	}
	svc := &serviceStub{plans: []domain.Plan{{ID: 1, Name: "free", Weight: 1, ResourceShare: .2}}}
	h.Plans = svc
	response, err := h.GetAdminPlans(context.Background(), gen.GetAdminPlansRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	if got := response.(gen.GetAdminPlans200JSONResponse); len(got) != 1 || got[0].Name != "free" || math.Abs(float64(got[0].ResourceShare)-.2) > 1e-6 {
		t.Fatalf("response=%+v", got)
	}
	svc.listErr = errors.New("db")
	if _, err := h.GetAdminPlans(context.Background(), gen.GetAdminPlansRequestObject{}); err == nil {
		t.Fatal("list error swallowed")
	}
}

func TestUpdateAdminPlans(t *testing.T) {
	h := Handler{ActorID: func(context.Context) (int64, bool) { return 0, false }}
	if _, err := h.UpdateAdminPlans(context.Background(), gen.UpdateAdminPlansRequestObject{}); err != nil {
		t.Fatal(err)
	}
	h = Handler{ActorID: actorOK}
	if _, err := h.UpdateAdminPlans(context.Background(), gen.UpdateAdminPlansRequestObject{}); err == nil {
		t.Fatal("missing service accepted")
	}
	svc := &serviceStub{}
	h.Plans = svc
	if _, err := h.UpdateAdminPlans(context.Background(), gen.UpdateAdminPlansRequestObject{}); err != nil {
		t.Fatal(err)
	}
	request := gen.UpdateAdminPlansRequestObject{Body: &gen.UpdateAdminPlansJSONRequestBody{Plans: []gen.Plan{{ID: 2, Name: "pro", Weight: 4, MaxDepthCap: 3, MaxContacts: 20, AiQuotaPerDay: 8, ZipTTLHours: 10, ResourceShare: .8}}}}
	if _, err := h.UpdateAdminPlans(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if svc.actor != 7 || len(svc.plans) != 1 || svc.plans[0].ID != 2 || svc.plans[0].AIQuotaPerDay != 8 || math.Abs(svc.plans[0].ResourceShare-.8) > 1e-6 {
		t.Fatalf("service call=%+v", svc)
	}
	svc.replaceErr = domain.ErrInvalidPlan
	if _, err := h.UpdateAdminPlans(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	svc.replaceErr = errors.New("db")
	if _, err := h.UpdateAdminPlans(context.Background(), request); err == nil {
		t.Fatal("replace error swallowed")
	}
}

func TestUpdateAdminUserPlan(t *testing.T) {
	h := Handler{ActorID: func(context.Context) (int64, bool) { return 0, false }}
	if _, err := h.UpdateAdminUserPlan(context.Background(), gen.UpdateAdminUserPlanRequestObject{}); err != nil {
		t.Fatal(err)
	}
	h = Handler{ActorID: actorOK}
	if _, err := h.UpdateAdminUserPlan(context.Background(), gen.UpdateAdminUserPlanRequestObject{}); err == nil {
		t.Fatal("missing service accepted")
	}
	svc := &serviceStub{}
	h.Plans = svc
	if _, err := h.UpdateAdminUserPlan(context.Background(), gen.UpdateAdminUserPlanRequestObject{}); err != nil {
		t.Fatal(err)
	}
	req := gen.UpdateAdminUserPlanRequestObject{ID: 11, Body: &gen.UpdateUserPlanRequest{PlanID: 4}}
	if _, err := h.UpdateAdminUserPlan(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if svc.actor != 7 || svc.user != 11 || svc.plan != 4 {
		t.Fatalf("assignment args=%+v", svc)
	}
	dbErr := errors.New("db")
	for _, caseErr := range []error{domain.ErrInvalidPlan, app.ErrPlanNotFound, app.ErrUserNotFound, dbErr} {
		svc.assignErr = caseErr
		_, err := h.UpdateAdminUserPlan(context.Background(), req)
		if errors.Is(caseErr, dbErr) {
			if err == nil {
				t.Fatal("unexpected error swallowed")
			}
			continue
		}
		if err != nil {
			t.Fatalf("mapped error %v escaped: %v", caseErr, err)
		}
	}
}
