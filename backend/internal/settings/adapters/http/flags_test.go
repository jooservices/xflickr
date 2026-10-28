package http

import (
	"context"
	"errors"
	"testing"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/settings/app"
	"github.com/jooservices/xflickr/internal/settings/domain"
)

type fakeFeatureFlagService struct {
	flags   []domain.FeatureFlag
	updated []domain.FeatureFlag
	err     error
}

func (service *fakeFeatureFlagService) List(context.Context) ([]domain.FeatureFlag, error) {
	return service.flags, service.err
}

func (service *fakeFeatureFlagService) Update(_ context.Context, flags []domain.FeatureFlag) error {
	if service.err != nil {
		return service.err
	}
	if len(flags) == 0 {
		return app.ErrNoFeatureFlags
	}
	seen := make(map[string]struct{}, len(flags))
	for _, flag := range flags {
		if err := domain.ValidateFeatureFlag(flag); err != nil {
			return err
		}
		if _, exists := seen[flag.Name]; exists {
			return app.ErrDuplicateFeatureFlag
		}
		seen[flag.Name] = struct{}{}
	}
	service.updated = flags
	return nil
}

func TestGetAdminFlagsMapsResponse(t *testing.T) {
	service := &fakeFeatureFlagService{flags: []domain.FeatureFlag{
		{Name: "targeted", Enabled: true, Audience: domain.AudienceUserIDs, UserIDs: []int64{3, 8}},
		{Name: "public", Audience: domain.AudienceAll},
	}}
	response, err := (Handler{Flags: service}).GetAdminFlags(context.Background(), gen.GetAdminFlagsRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	flags, ok := response.(gen.GetAdminFlags200JSONResponse)
	if !ok || len(flags) != 2 || flags[0].Name != "targeted" || flags[0].Audience != gen.UserIds || len(flags[0].UserIds) != 2 || flags[1].Name != "public" || flags[1].UserIds == nil {
		t.Fatalf("response = %#v", response)
	}
	if _, err := (Handler{}).GetAdminFlags(context.Background(), gen.GetAdminFlagsRequestObject{}); err == nil {
		t.Fatal("missing feature flag service accepted")
	}
	service.err = errors.New("read failed")
	if _, err := (Handler{Flags: service}).GetAdminFlags(context.Background(), gen.GetAdminFlagsRequestObject{}); err == nil {
		t.Fatal("service error swallowed")
	}
}

func TestUpdateAdminFlagsMapsRequestAndValidation(t *testing.T) {
	service := &fakeFeatureFlagService{}
	handler := Handler{Flags: service}
	request := gen.UpdateAdminFlagsRequestObject{Body: &gen.UpdateAdminFlagsRequest{
		Flags: []gen.FeatureFlag{
			{Name: "admin", Enabled: true, Audience: gen.Admins, UserIds: []int64{}},
			{Name: "targeted", Enabled: true, Audience: gen.UserIds, UserIds: []int64{2, 5}},
		},
	}}
	response, err := handler.UpdateAdminFlags(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := response.(gen.UpdateAdminFlags204Response); !ok || len(service.updated) != 2 || service.updated[0].Audience != domain.AudienceAdmins || service.updated[1].Audience != domain.AudienceUserIDs || service.updated[1].UserIDs[1] != 5 {
		t.Fatalf("response/service update = %#v/%+v", response, service.updated)
	}

	for _, invalid := range []gen.UpdateAdminFlagsRequestObject{
		{},
		{Body: &gen.UpdateAdminFlagsRequest{}},
		{Body: &gen.UpdateAdminFlagsRequest{Flags: []gen.FeatureFlag{{Name: "INVALID", Audience: gen.All}}}},
		{Body: &gen.UpdateAdminFlagsRequest{Flags: []gen.FeatureFlag{{Name: "duplicate", Audience: gen.All}, {Name: "duplicate", Audience: gen.All}}}},
	} {
		response, err := handler.UpdateAdminFlags(context.Background(), invalid)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := response.(gen.UpdateAdminFlags400JSONResponse); !ok {
			t.Fatalf("invalid request response = %T", response)
		}
	}
	if _, err := (Handler{}).UpdateAdminFlags(context.Background(), request); err == nil {
		t.Fatal("missing feature flag service accepted")
	}
	service.err = errors.New("write failed")
	if _, err := handler.UpdateAdminFlags(context.Background(), request); err == nil {
		t.Fatal("service error swallowed")
	}
	service.err = app.ErrNoFeatureFlags
	if response, err := handler.UpdateAdminFlags(context.Background(), request); err != nil {
		t.Fatal(err)
	} else if _, ok := response.(gen.UpdateAdminFlags400JSONResponse); !ok {
		t.Fatalf("empty update error response = %T", response)
	}
}
