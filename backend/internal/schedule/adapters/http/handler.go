package http

import (
	"context"
	"errors"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/schedule/app"
	"github.com/jooservices/xflickr/internal/schedule/domain"
)

type PlanService interface {
	List(context.Context) ([]domain.Plan, error)
	Replace(context.Context, int64, []domain.Plan) error
	Assign(context.Context, int64, int64, int64) error
}

type QueueService interface {
	ForUser(context.Context, int64) ([]app.LaneEstimate, error)
	ForAdmin(context.Context) (app.AdminQueue, error)
}

// Handler implements administrator plan endpoints. ActorID is populated from the authenticated request context.
type Handler struct {
	Plans   PlanService
	Queue   QueueService
	ActorID func(context.Context) (int64, bool)
}

func (h Handler) GetMeCrawlQueue(ctx context.Context, _ gen.GetMeCrawlQueueRequestObject) (gen.GetMeCrawlQueueResponseObject, error) {
	userID, ok := h.actor(ctx)
	if !ok {
		return gen.GetMeCrawlQueue401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	if h.Queue == nil {
		return nil, errors.New("schedule: queue service is required")
	}
	lanes, err := h.Queue.ForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]gen.QueueLane, 0, len(lanes))
	for _, lane := range lanes {
		out = append(out, gen.QueueLane{Lane: gen.QueueLaneLane(lane.Lane), BacklogCalls: lane.BacklogCalls, EffectiveCallsPerHour: lane.EffectiveCallsPerHour, EtaHours: lane.ETAHours})
	}
	return gen.GetMeCrawlQueue200JSONResponse{Lanes: out}, nil
}

func (h Handler) GetAdminQueue(ctx context.Context, _ gen.GetAdminQueueRequestObject) (gen.GetAdminQueueResponseObject, error) {
	if _, ok := h.actor(ctx); !ok {
		return gen.GetAdminQueue401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	if h.Queue == nil {
		return nil, errors.New("schedule: queue service is required")
	}
	snapshot, err := h.Queue.ForAdmin(ctx)
	if err != nil {
		return nil, err
	}
	backlog := make([]gen.PlanLaneBacklog, 0, len(snapshot.BacklogByPlan))
	for _, item := range snapshot.BacklogByPlan {
		backlog = append(backlog, gen.PlanLaneBacklog{Plan: item.Plan, Lane: gen.PlanLaneBacklogLane(item.Lane), BacklogCalls: item.BacklogCalls})
	}
	queues := make([]gen.ReadyQueueSize, 0, len(snapshot.ReadyQueueSizes))
	for _, item := range snapshot.ReadyQueueSizes {
		queues = append(queues, gen.ReadyQueueSize{Queue: item.Queue, ReadyJobs: item.ReadyJobs})
	}
	return gen.GetAdminQueue200JSONResponse{BacklogByPlan: backlog, OldestItemAgeSeconds: snapshot.OldestItemAgeSeconds, ReadyQueueSizes: queues}, nil
}

func (h Handler) GetAdminPlans(ctx context.Context, _ gen.GetAdminPlansRequestObject) (gen.GetAdminPlansResponseObject, error) {
	if _, ok := h.actor(ctx); !ok {
		return gen.GetAdminPlans401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	if h.Plans == nil {
		return nil, errors.New("schedule: plan service is required")
	}
	plans, err := h.Plans.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Plan, 0, len(plans))
	for _, p := range plans {
		out = append(out, apiPlan(p))
	}
	return gen.GetAdminPlans200JSONResponse(out), nil
}

func (h Handler) UpdateAdminPlans(ctx context.Context, request gen.UpdateAdminPlansRequestObject) (gen.UpdateAdminPlansResponseObject, error) {
	actor, ok := h.actor(ctx)
	if !ok {
		return gen.UpdateAdminPlans401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	if h.Plans == nil {
		return nil, errors.New("schedule: plan service is required")
	}
	if request.Body == nil {
		return gen.UpdateAdminPlans400JSONResponse{Code: "invalid_request", Message: "Plans are required"}, nil
	}
	plans := make([]domain.Plan, 0, len(request.Body.Plans))
	for _, p := range request.Body.Plans {
		plans = append(plans, domain.Plan{ID: p.ID, Name: p.Name, Weight: p.Weight, MaxDepthCap: p.MaxDepthCap, MaxContacts: p.MaxContacts, AIQuotaPerDay: p.AiQuotaPerDay, ZipTTLHours: p.ZipTTLHours, ResourceShare: float64(p.ResourceShare)})
	}
	if err := h.Plans.Replace(ctx, actor, plans); errors.Is(err, domain.ErrInvalidPlan) {
		return gen.UpdateAdminPlans400JSONResponse{Code: "invalid_request", Message: "Plan definitions are invalid"}, nil
	} else if err != nil {
		return nil, err
	}
	return gen.UpdateAdminPlans204Response{}, nil
}

func (h Handler) UpdateAdminUserPlan(ctx context.Context, request gen.UpdateAdminUserPlanRequestObject) (gen.UpdateAdminUserPlanResponseObject, error) {
	actor, ok := h.actor(ctx)
	if !ok {
		return gen.UpdateAdminUserPlan401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	if h.Plans == nil {
		return nil, errors.New("schedule: plan service is required")
	}
	if request.Body == nil {
		return gen.UpdateAdminUserPlan400JSONResponse{Code: "invalid_request", Message: "Plan id is required"}, nil
	}
	err := h.Plans.Assign(ctx, actor, request.ID, request.Body.PlanID)
	switch {
	case errors.Is(err, domain.ErrInvalidPlan):
		return gen.UpdateAdminUserPlan400JSONResponse{Code: "invalid_request", Message: "Plan id is invalid"}, nil
	case errors.Is(err, app.ErrPlanNotFound), errors.Is(err, app.ErrUserNotFound):
		return gen.UpdateAdminUserPlan404JSONResponse{Code: "not_found", Message: "User or plan not found"}, nil
	case err != nil:
		return nil, err
	default:
		return gen.UpdateAdminUserPlan204Response{}, nil
	}
}

func (h Handler) actor(ctx context.Context) (int64, bool) {
	if h.ActorID == nil {
		return 0, false
	}
	return h.ActorID(ctx)
}

func apiPlan(p domain.Plan) gen.Plan {
	return gen.Plan{ID: p.ID, Name: p.Name, Weight: p.Weight, MaxDepthCap: p.MaxDepthCap, MaxContacts: p.MaxContacts, AiQuotaPerDay: p.AIQuotaPerDay, ZipTTLHours: p.ZipTTLHours, ResourceShare: float32(p.ResourceShare)}
}
