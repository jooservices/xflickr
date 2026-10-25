// Package http serves GET /admin/ops/status.
package http

import (
	"context"
	"errors"
	"time"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/ops"
)

// StatusService reads the crawl health report.
type StatusService interface {
	Status(context.Context) (ops.Status, error)
}

var _ StatusService = ops.Service{}

// Handler implements GET /admin/ops/status. /admin is already limited to
// administrators by the identity middleware; ActorID guards the handler when
// it is mounted without that middleware.
type Handler struct {
	Service StatusService
	ActorID func(context.Context) (int64, bool)
}

// GetAdminOpsStatus returns the crawl health report.
func (h Handler) GetAdminOpsStatus(ctx context.Context, _ gen.GetAdminOpsStatusRequestObject) (gen.GetAdminOpsStatusResponseObject, error) {
	if h.ActorID == nil {
		return gen.GetAdminOpsStatus401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	if _, ok := h.ActorID(ctx); !ok {
		return gen.GetAdminOpsStatus401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	if h.Service == nil {
		return nil, errors.New("ops: status service is required")
	}
	status, err := h.Service.Status(ctx)
	if err != nil {
		return nil, err
	}
	return gen.GetAdminOpsStatus200JSONResponse(toStatus(status)), nil
}

func toStatus(s ops.Status) gen.OpsStatus {
	out := gen.OpsStatus{
		GeneratedAt: s.GeneratedAt,
		Counts: gen.OpsCounts{
			People: s.Counts.People, Photos: s.Counts.Photos, PhotoSizes: s.Counts.PhotoSizes,
			Favorites: s.Counts.Favorites, Albums: s.Counts.Albums, Galleries: s.Counts.Galleries, Groups: s.Counts.Groups,
		},
		WorkItems:               s.WorkItems,
		ReadyJobs:               s.ReadyJobs,
		RiverJobs:               s.RiverJobs,
		Keys:                    make([]gen.OpsKey, 0, len(s.Keys)),
		CallsLastHour:           s.CallsLastHour,
		CallsLast10Min:          s.CallsLast10Min,
		PendingItems:            s.PendingItems,
		OldestPendingAgeSeconds: int64(s.OldestPendingAge / time.Second),
		FailedWorkItems:         s.FailedWorkItems,
		DeadJobs:                s.DeadJobs,
		TokenStatus:             gen.OpsStatusTokenStatus(s.TokenStatus),
	}
	for _, key := range s.Keys {
		out.Keys = append(out.Keys, gen.OpsKey{
			KeyID: key.KeyID, Label: key.Label, Status: gen.OpsKeyStatus(key.Status),
			CallsLastHour: key.CallsLastHour, Used: key.Used(), Remaining: key.Remaining(), Limit: key.Limit,
		})
	}
	if s.IdleReason != "" {
		reason := gen.OpsStatusIdleReason(s.IdleReason)
		out.IdleReason = &reason
	}
	return out
}
