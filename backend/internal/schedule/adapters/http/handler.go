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

// Handler implements administrator plan endpoints. ActorID is populated from the authenticated request context.
type Handler struct {
	Plans   PlanService
	ActorID func(context.Context) (int64, bool)
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
