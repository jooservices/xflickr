package http

import (
	"context"
	"errors"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/settings/app"
	"github.com/jooservices/xflickr/internal/settings/domain"
)

type FeatureFlagService interface {
	List(context.Context) ([]domain.FeatureFlag, error)
	Update(context.Context, []domain.FeatureFlag) error
}

// Handler implements the administrator feature flag API. Authorization is
// enforced by the route access middleware before these methods are invoked.
type Handler struct{ Flags FeatureFlagService }

func (handler Handler) GetAdminFlags(ctx context.Context, _ gen.GetAdminFlagsRequestObject) (gen.GetAdminFlagsResponseObject, error) {
	if handler.Flags == nil {
		return nil, errors.New("settings: feature flag service is required")
	}
	flags, err := handler.Flags.List(ctx)
	if err != nil {
		return nil, err
	}
	response := make(gen.GetAdminFlags200JSONResponse, 0, len(flags))
	for _, flag := range flags {
		response = append(response, apiFeatureFlag(flag))
	}
	return response, nil
}

func (handler Handler) UpdateAdminFlags(ctx context.Context, request gen.UpdateAdminFlagsRequestObject) (gen.UpdateAdminFlagsResponseObject, error) {
	if handler.Flags == nil {
		return nil, errors.New("settings: feature flag service is required")
	}
	if request.Body == nil || len(request.Body.Flags) == 0 {
		return gen.UpdateAdminFlags400JSONResponse{Code: "invalid_request", Message: "At least one feature flag is required"}, nil
	}
	flags := make([]domain.FeatureFlag, 0, len(request.Body.Flags))
	for _, flag := range request.Body.Flags {
		flags = append(flags, domain.FeatureFlag{
			Name:     flag.Name,
			Enabled:  flag.Enabled,
			Audience: domain.FlagAudience(flag.Audience),
			UserIDs:  append([]int64(nil), flag.UserIds...),
		})
	}
	if err := handler.Flags.Update(ctx, flags); err != nil {
		if errors.Is(err, domain.ErrInvalidFeatureFlag) || errors.Is(err, app.ErrNoFeatureFlags) || errors.Is(err, app.ErrDuplicateFeatureFlag) {
			return gen.UpdateAdminFlags400JSONResponse{Code: "invalid_request", Message: "Feature flag definitions are invalid"}, nil
		}
		return nil, err
	}
	return gen.UpdateAdminFlags204Response{}, nil
}

func apiFeatureFlag(flag domain.FeatureFlag) gen.FeatureFlag {
	return gen.FeatureFlag{
		Name:     flag.Name,
		Enabled:  flag.Enabled,
		Audience: gen.FeatureFlagAudience(flag.Audience),
		UserIds:  append([]int64{}, flag.UserIDs...),
	}
}
