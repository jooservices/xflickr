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

func TestGetAdminFlagMapsRepresentation(t *testing.T) {
	service := &fakeFeatureFlagService{flag: domain.FeatureFlag{Name: "admin.banner", Enabled: true, Audience: domain.AudienceAll}}
	response := mustVisitFlagResponse(t, Handler{Flags: service}, t.Context(), service.flag.Name)
	if response.Code != 200 || response.Header().Get("Content-Type") != "application/json" || response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("success response headers/status = %d %v", response.Code, response.Header())
	}
	var body gen.RESTFeatureFlagEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	self := body.Data.Links.Self
	if !bool(body.Success) || body.Code != 200 || body.Message != "Success" || body.Data.Name != service.flag.Name || !body.Data.Enabled || body.Data.Audience != gen.FeatureFlagAudience(domain.AudienceAll) || !reflect.DeepEqual(body.Data.UserIds, []int64{}) || self.Href != "/api/v1/admin/flags/admin.banner" || self.Method == nil || *self.Method != gen.LinkMethod("GET") || body.Meta == nil || body.Errors != nil || service.getCalls != 1 || body.TraceID != "" {
		t.Fatalf("success representation = %+v; calls = %d", body, service.getCalls)
	}
	if response.Header().Get("X-Trace-Id") != "" {
		t.Fatal("unexpected trace header without a valid span")
	}
}

func flagTraceContext(ctx context.Context, id byte) context.Context {
	return trace.ContextWithSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{id}, SpanID: trace.SpanID{id}}))
}

func TestGetAdminFlagStableAcrossTraces(t *testing.T) {
	handler := Handler{Flags: &fakeFeatureFlagService{flag: domain.FeatureFlag{Name: "banner", Audience: domain.AudienceAll}}}
	var previous *httptest.ResponseRecorder
	for _, id := range []byte{1, 2} {
		ctx := flagTraceContext(t.Context(), id)
		response := mustVisitFlagResponse(t, handler, ctx, "banner")
		if response.Header().Get("X-Trace-Id") != trace.SpanContextFromContext(ctx).TraceID().String() {
			t.Fatalf("trace header = %q", response.Header().Get("X-Trace-Id"))
		}
		var body gen.RESTFeatureFlagEnvelope
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.TraceID != "" {
			t.Fatalf("body trace = %q", body.TraceID)
		}
		assertFlagETag(t, response)
		if previous != nil && (previous.Body.String() != response.Body.String() || previous.Header().Get("ETag") != response.Header().Get("ETag")) {
			t.Fatal("request trace changed representation or ETag")
		}
		previous = response
	}
}

func assertFlagETag(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	tag := response.Header().Get("ETag")
	if tag != rest.ETag(response.Body.Bytes()) || !strings.HasPrefix(tag, "\"") || !strings.HasSuffix(response.Body.String(), "\n") {
		t.Fatalf("ETag = %q, want strong full-body validator including newline", tag)
	}
}

func TestGetAdminFlagETagTracksRepresentation(t *testing.T) {
	base := domain.FeatureFlag{Name: "banner", Enabled: true, Audience: domain.AudienceUserIDs, UserIDs: []int64{9, 3}}
	service := &fakeFeatureFlagService{flag: base}
	handler := Handler{Flags: service}
	first := mustVisitFlagResponse(t, handler, t.Context(), base.Name)
	assertFlagETag(t, first)
	for _, field := range []string{"name", "enabled", "audience", "user ID content", "user ID order", "nil IDs"} {
		t.Run(field, func(t *testing.T) {
			changed := base
			switch field {
			case "name":
				changed.Name = "banner.v2"
			case "enabled":
				changed.Enabled = false
			case "audience":
				changed.Audience = domain.AudienceAdmins
			case "user ID content":
				changed.UserIDs = []int64{9, 4}
			case "user ID order":
				changed.UserIDs = []int64{3, 9}
			case "nil IDs":
				changed.UserIDs = nil
			}
			service.flag = changed
			response := mustVisitFlagResponse(t, handler, t.Context(), changed.Name)
			assertFlagETag(t, response)
			if response.Header().Get("ETag") == first.Header().Get("ETag") {
				t.Fatal("changed field did not change ETag")
			}
		})
	}
	if got := apiFeatureFlag(domain.FeatureFlag{Name: "empty", Audience: domain.AudienceAll}).UserIds; !reflect.DeepEqual(got, []int64{}) {
		t.Fatalf("empty user_ids = %#v, want []", got)
	}
}

type flagProblemCase struct {
	name       string
	flagName   string
	service    *fakeFeatureFlagService
	wantStatus int
	wantType   string
	wantErrors bool
	leak       string
}

func TestGetAdminFlagReturnsSafeProblems(t *testing.T) {
	for _, test := range []flagProblemCase{
		{name: "not found", flagName: "missing", service: &fakeFeatureFlagService{getErr: app.ErrFeatureFlagNotFound}, wantStatus: 404, wantType: "/problems/not-found"},
		{name: "storage failure", flagName: "secret.flag", service: &fakeFeatureFlagService{getErr: errors.New("SQL password=hidden")}, wantStatus: 500, wantType: "/problems/internal-error", leak: "SQL password=hidden"},
		{name: "invalid name", flagName: "INVALID", service: &fakeFeatureFlagService{}, wantStatus: 422, wantType: "/problems/validation-error", wantErrors: true},
		{name: "missing service", flagName: "valid", wantStatus: 500, wantType: "/problems/internal-error"},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertFlagProblem(t, test)
		})
	}
}

func assertFlagProblem(t *testing.T, test flagProblemCase) {
	t.Helper()
	handler := Handler{Flags: test.service}
	if test.name == "missing service" {
		handler = Handler{}
	}
	ctx := flagTraceContext(t.Context(), 3)
	recorder := mustVisitFlagResponse(t, handler, ctx, test.flagName)
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
	if recorder.Code != test.wantStatus || recorder.Header().Get("Content-Type") != "application/problem+json" || body.Status != test.wantStatus || body.Type != test.wantType || body.Instance == "" || body.TraceID != trace.SpanContextFromContext(ctx).TraceID().String() || (body.Errors != nil) != test.wantErrors {
		t.Fatalf("problem response = %d %q %+v", recorder.Code, recorder.Header().Get("Content-Type"), body)
	}
	if test.name == "invalid name" && test.service.getCalls != 0 {
		t.Fatal("invalid flag name reached the feature flag service")
	}
	if test.leak != "" && strings.Contains(recorder.Body.String(), test.leak) {
		t.Fatalf("problem leaked repository error: %s", recorder.Body.String())
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
