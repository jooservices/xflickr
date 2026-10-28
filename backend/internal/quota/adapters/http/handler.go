// Package http serves the quota state endpoints.
package http

import (
	"context"
	"errors"
	"time"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/quota/app"
)

// StateService reads quota state for the endpoints.
type StateService interface {
	ForOwner(ctx context.Context, userID, keyID int64) (app.KeyState, error)
	All(context.Context) ([]app.KeyState, error)
}

type CallStatsService interface {
	Stats(context.Context, string) (app.CallStats, error)
}

// QuotaHandler implements GET /keys/{id}/quota and GET /admin/quota. ActorID is
// populated from the authenticated request context; /admin is already limited
// to administrators by the identity middleware.
type QuotaHandler struct {
	States    StateService
	CallStats CallStatsService
	Node      string
	ActorID   func(context.Context) (int64, bool)
}

var _ StateService = app.States{}

// GetKeyQuota returns the quota state of a key owned by the signed-in user.
func (h QuotaHandler) GetKeyQuota(ctx context.Context, request gen.GetKeyQuotaRequestObject) (gen.GetKeyQuotaResponseObject, error) {
	userID, ok := h.actor(ctx)
	if !ok {
		return gen.GetKeyQuota401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	if h.States == nil {
		return nil, errors.New("quota: state service is required")
	}
	state, err := h.States.ForOwner(ctx, userID, request.ID)
	if errors.Is(err, app.ErrKeyNotFound) {
		return gen.GetKeyQuota404JSONResponse(gen.Error{Code: "not_found", Message: "Key not found"}), nil
	}
	if err != nil {
		return nil, err
	}
	return gen.GetKeyQuota200JSONResponse(toKeyQuota(state)), nil
}

// GetAdminQuota returns the quota state of every active key.
func (h QuotaHandler) GetAdminQuota(ctx context.Context, _ gen.GetAdminQuotaRequestObject) (gen.GetAdminQuotaResponseObject, error) {
	if _, ok := h.actor(ctx); !ok {
		return gen.GetAdminQuota401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	if h.States == nil {
		return nil, errors.New("quota: state service is required")
	}
	states, err := h.States.All(ctx)
	if err != nil {
		return nil, err
	}
	keys := make([]gen.AdminKeyQuota, 0, len(states))
	for _, state := range states {
		keys = append(keys, toAdminKeyQuota(state, h.Node))
	}
	return gen.GetAdminQuota200JSONResponse(gen.AdminQuota{Keys: keys}), nil
}

// GetAdminCallStats returns the call and completed-work aggregates for admins.
func (h QuotaHandler) GetAdminCallStats(ctx context.Context, request gen.GetAdminCallStatsRequestObject) (gen.GetAdminCallStatsResponseObject, error) {
	if _, ok := h.actor(ctx); !ok {
		return gen.GetAdminCallStats401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	window := string(request.Params.Window)
	if window != "1h" && window != "24h" {
		return gen.GetAdminCallStats400JSONResponse(gen.Error{Code: "invalid_window", Message: "Window must be 1h or 24h"}), nil
	}
	if h.CallStats == nil {
		return nil, errors.New("quota: call stats service is required")
	}
	stats, err := app.ReadCallStats(ctx, h.CallStats, window)
	if err != nil {
		return nil, err
	}
	response := gen.CallStats{Window: gen.CallStatsWindow(stats.Window)}
	for _, row := range stats.CallsPerHour {
		response.CallsPerHour = append(response.CallsPerHour, gen.CallOutcomeHour{Hour: row.Hour, Outcome: gen.CallOutcomeHourOutcome(row.Outcome), Calls: row.Calls})
	}
	for _, row := range stats.TopErrorCodes {
		response.TopErrorCodes = append(response.TopErrorCodes, gen.CallErrorCode{Code: row.Code, Calls: row.Calls})
	}
	for _, row := range stats.LatencyByMethod {
		response.LatencyByMethod = append(response.LatencyByMethod, gen.CallMethodLatency{Method: row.Method, P50Ms: float32(row.P50MS), P95Ms: float32(row.P95MS)})
	}
	for _, row := range stats.RetriesByFacet {
		response.RetriesByFacet = append(response.RetriesByFacet, gen.CallFacetRetries{Facet: row.Facet, Retries: row.Retries})
	}
	for _, row := range stats.CompletedPerHour {
		response.CompletedPerHour = append(response.CompletedPerHour, gen.CallCompletedHour{Hour: row.Hour, Photos: row.Photos, People: row.People})
	}
	return gen.GetAdminCallStats200JSONResponse(response), nil
}

func (h QuotaHandler) actor(ctx context.Context) (int64, bool) {
	if h.ActorID == nil {
		return 0, false
	}
	return h.ActorID(ctx)
}

func toKeyQuota(s app.KeyState) gen.KeyQuota {
	return gen.KeyQuota{
		KeyID:   s.Key.ID,
		Used:    s.Used,
		Limit:   s.Limit,
		Reserve: s.Reserve,
		ResetAt: resetAt(s),
		State:   gen.KeyQuotaState(s.State),
		Lanes:   gen.LaneUsage{Interactive: s.Lanes.Interactive, Person: s.Lanes.Person, Photo: s.Lanes.Photo},
	}
}

func toAdminKeyQuota(s app.KeyState, node string) gen.AdminKeyQuota {
	userID := s.Key.UserID
	return gen.AdminKeyQuota{
		KeyID:   s.Key.ID,
		UserID:  &userID,
		Label:   s.Key.Label,
		Node:    node,
		Used:    s.Used,
		Limit:   s.Limit,
		ResetAt: resetAt(s),
		State:   gen.AdminKeyQuotaState(s.State),
		Trend:   gen.AdminKeyQuotaTrend(s.Trend),
	}
}

func resetAt(s app.KeyState) *time.Time {
	if s.ResetAt.IsZero() {
		return nil
	}
	at := s.ResetAt.UTC()
	return &at
}
