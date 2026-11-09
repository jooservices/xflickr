package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/api/rest"
	"github.com/jooservices/xflickr/internal/settings/app"
	"github.com/jooservices/xflickr/internal/settings/domain"
)

type fakeFeatureFlagService struct {
	flags    []domain.FeatureFlag
	flag     domain.FeatureFlag
	updated  []domain.FeatureFlag
	err      error
	getErr   error
	getCalls int
}

func (service *fakeFeatureFlagService) List(context.Context) ([]domain.FeatureFlag, error) {
	return service.flags, service.err
}

func (service *fakeFeatureFlagService) Get(_ context.Context, _ string) (domain.FeatureFlag, error) {
	service.getCalls++
	return service.flag, service.getErr
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
	if !ok || len(flags) != 2 || flags[0].Name != "targeted" || flags[0].Audience != gen.FeatureFlagAudienceUserIds || len(flags[0].UserIds) != 2 || flags[1].Name != "public" || flags[1].UserIds == nil {
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

func TestGetAdminFlag(t *testing.T) {
	base := domain.FeatureFlag{Name: "admin.banner", Enabled: true, Audience: domain.AudienceAll, UserIDs: nil}
	service := &fakeFeatureFlagService{flag: base}
	handler := Handler{Flags: service}
	ctx := t.Context()
	first := mustVisitFlagResponse(t, handler, ctx, base.Name)
	second := mustVisitFlagResponse(t, handler, ctx, base.Name)
	if first.Code != 200 || first.Header().Get("Content-Type") != "application/json" || first.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("success response headers/status = %d %v", first.Code, first.Header())
	}
	var body struct {
		Success bool   `json:"success"`
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Name     string  `json:"name"`
			Enabled  bool    `json:"enabled"`
			Audience string  `json:"audience"`
			UserIDs  []int64 `json:"user_ids"`
			Links    map[string]struct {
				Href   string `json:"href"`
				Method string `json:"method"`
			} `json:"links"`
		} `json:"data"`
		Meta    map[string]any `json:"meta"`
		Errors  any            `json:"errors"`
		TraceID string         `json:"trace_id"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	self := body.Data.Links["self"]
	if !body.Success || body.Code != 200 || body.Message != "Success" || body.Data.Name != base.Name || !body.Data.Enabled || body.Data.Audience != "all" || body.Data.UserIDs == nil || len(body.Data.UserIDs) != 0 || self.Href != "/api/v1/admin/flags/admin.banner" || self.Method != "GET" || body.Meta == nil || body.Errors != nil || service.getCalls != 2 {
		t.Fatalf("success representation = %+v; calls = %d", body, service.getCalls)
	}
	if first.Header().Get("ETag") == "" || first.Header().Get("ETag") != second.Header().Get("ETag") {
		t.Fatalf("unchanged representation ETags = %q / %q", first.Header().Get("ETag"), second.Header().Get("ETag"))
	}
	if got, want := first.Header().Get("ETag"), rest.ETag(first.Body.Bytes()); got != want {
		t.Fatalf("ETag = %q, want full response validator %q", got, want)
	}

	traceContext := trace.ContextWithSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}}))
	traced := mustVisitFlagResponse(t, handler, traceContext, base.Name)
	var tracedBody struct {
		TraceID string `json:"trace_id"`
	}
	if err := json.Unmarshal(traced.Body.Bytes(), &tracedBody); err != nil {
		t.Fatal(err)
	}
	if tracedBody.TraceID == body.TraceID || traced.Header().Get("ETag") == first.Header().Get("ETag") {
		t.Fatalf("trace changed body without changing ETag: first=%q/%q traced=%q/%q", body.TraceID, first.Header().Get("ETag"), tracedBody.TraceID, traced.Header().Get("ETag"))
	}
	if got, want := traced.Header().Get("ETag"), rest.ETag(traced.Body.Bytes()); got != want {
		t.Fatalf("traced ETag = %q, want full response validator %q", got, want)
	}

	for _, changed := range []domain.FeatureFlag{
		{Name: "admin.banner.v2", Enabled: base.Enabled, Audience: base.Audience},
		{Name: base.Name, Enabled: false, Audience: base.Audience},
		{Name: base.Name, Enabled: base.Enabled, Audience: domain.AudienceAdmins},
		{Name: base.Name, Enabled: base.Enabled, Audience: domain.AudienceUserIDs, UserIDs: []int64{9, 3}},
	} {
		service.flag = changed
		response := mustVisitFlagResponse(t, handler, ctx, changed.Name)
		if response.Header().Get("ETag") == first.Header().Get("ETag") {
			t.Errorf("changed field did not change ETag: %+v", changed)
		}
	}
	service.flag = domain.FeatureFlag{Name: base.Name, Enabled: true, Audience: domain.AudienceUserIDs, UserIDs: []int64{9, 3}}
	firstOrder := mustVisitFlagResponse(t, handler, ctx, base.Name).Header().Get("ETag")
	service.flag.UserIDs = []int64{3, 9}
	secondOrder := mustVisitFlagResponse(t, handler, ctx, base.Name).Header().Get("ETag")
	if firstOrder == secondOrder {
		t.Fatalf("user ID order did not affect ETag: %q", firstOrder)
	}
	if got := apiFeatureFlag(domain.FeatureFlag{Name: "empty", Audience: domain.AudienceAll}).UserIds; !reflect.DeepEqual(got, []int64{}) {
		t.Fatalf("empty user_ids = %#v, want []", got)
	}
}

func TestGetAdminFlagReturnsSafeProblems(t *testing.T) {
	for _, test := range []struct {
		name       string
		flagName   string
		service    *fakeFeatureFlagService
		wantStatus int
		wantType   string
		wantErrors bool
		leak       string
	}{
		{name: "not found", flagName: "missing", service: &fakeFeatureFlagService{getErr: app.ErrFeatureFlagNotFound}, wantStatus: 404, wantType: "/problems/not-found"},
		{name: "storage failure", flagName: "secret.flag", service: &fakeFeatureFlagService{getErr: errors.New("SQL password=hidden")}, wantStatus: 500, wantType: "/problems/internal-error", leak: "SQL password=hidden"},
		{name: "invalid name", flagName: "INVALID", service: &fakeFeatureFlagService{}, wantStatus: 422, wantType: "/problems/validation-error", wantErrors: true},
		{name: "missing service", flagName: "valid", wantStatus: 500, wantType: "/problems/internal-error"},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := Handler{Flags: test.service}
			if test.name == "missing service" {
				handler = Handler{}
			}
			recorder := mustVisitFlagResponse(t, handler, t.Context(), test.flagName)
			var body struct {
				Type     string `json:"type"`
				Status   int    `json:"status"`
				Instance string `json:"instance"`
				TraceID  string `json:"trace_id"`
				Errors   any    `json:"errors"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != test.wantStatus || recorder.Header().Get("Content-Type") != "application/problem+json" || body.Status != test.wantStatus || body.Type != test.wantType || body.Instance == "" || (body.Errors != nil) != test.wantErrors {
				t.Fatalf("problem response = %d %q %+v", recorder.Code, recorder.Header().Get("Content-Type"), body)
			}
			if test.name == "invalid name" && test.service.getCalls != 0 {
				t.Fatal("invalid flag name reached the feature flag service")
			}
			if test.leak != "" && strings.Contains(recorder.Body.String(), test.leak) {
				t.Fatalf("problem leaked repository error: %s", recorder.Body.String())
			}
		})
	}
}

func mustVisitFlagResponse(t *testing.T, handler Handler, ctx context.Context, name string) *httptest.ResponseRecorder {
	t.Helper()
	response, err := handler.GetAdminFlag(ctx, gen.GetAdminFlagRequestObject{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	if err := response.VisitGetAdminFlagResponse(recorder); err != nil {
		t.Fatal(err)
	}
	return recorder
}

func TestUpdateAdminFlagsMapsRequestAndValidation(t *testing.T) {
	service := &fakeFeatureFlagService{}
	handler := Handler{Flags: service}
	request := gen.UpdateAdminFlagsRequestObject{Body: &gen.UpdateAdminFlagsRequest{
		Flags: []gen.FeatureFlag{
			{Name: "admin", Enabled: true, Audience: gen.FeatureFlagAudienceAdmins, UserIds: []int64{}},
			{Name: "targeted", Enabled: true, Audience: gen.FeatureFlagAudienceUserIds, UserIds: []int64{2, 5}},
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
		{Body: &gen.UpdateAdminFlagsRequest{Flags: []gen.FeatureFlag{{Name: "INVALID", Audience: gen.FeatureFlagAudienceAll}}}},
		{Body: &gen.UpdateAdminFlagsRequest{Flags: []gen.FeatureFlag{{Name: "duplicate", Audience: gen.FeatureFlagAudienceAll}, {Name: "duplicate", Audience: gen.FeatureFlagAudienceAll}}}},
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
