package http

import (
	"context"
	"errors"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/sharing/app"
)

type Handler struct {
	Change  app.Change
	ActorID func(context.Context) (int64, bool)
}

func (h Handler) PutMeSharing(ctx context.Context, request gen.PutMeSharingRequestObject) (gen.PutMeSharingResponseObject, error) {
	if request.Body == nil {
		return gen.PutMeSharing400JSONResponse(gen.Error{Code: "invalid_request", Message: "Enabled is required"}), nil
	}
	if h.ActorID == nil {
		return gen.PutMeSharing401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	userID, ok := h.ActorID(ctx)
	if !ok || userID <= 0 {
		return gen.PutMeSharing401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	setting, err := h.Change.Execute(ctx, userID, request.Body.Enabled)
	if errors.Is(err, app.ErrInvalidUser) {
		return gen.PutMeSharing401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	if err != nil {
		return nil, err
	}
	return gen.PutMeSharing200JSONResponse(gen.SharingSetting{Enabled: setting.Enabled, ChangedAt: setting.ChangedAt}), nil
}
