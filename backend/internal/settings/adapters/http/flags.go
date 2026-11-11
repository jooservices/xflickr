package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"go.opentelemetry.io/otel/trace"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/api/rest"
	"github.com/jooservices/xflickr/internal/settings/app"
	"github.com/jooservices/xflickr/internal/settings/domain"
)

// FeatureFlagService provides configured rollout flags to the HTTP adapter.
type FeatureFlagService interface {
	List(context.Context) ([]domain.FeatureFlag, error)
	// Get reads one flag directly from storage.
	Get(context.Context, string) (domain.FeatureFlag, error)
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

// GetAdminFlag returns one configured flag with a strong representation ETag.
func (handler Handler) GetAdminFlag(ctx context.Context, request gen.GetAdminFlagRequestObject) (gen.GetAdminFlagResponseObject, error) {
	if err := domain.ValidateFeatureFlag(domain.FeatureFlag{Name: request.Name, Audience: domain.AudienceAll}); err != nil {
		return newFlagProblem(request.Name, "validation-error", "The feature flag name is invalid", rest.FieldErrors{"name": {"Name must match the feature flag naming rules"}}, featureFlagTraceID(ctx)), nil
	}
	if handler.Flags == nil {
		return newFlagProblem(request.Name, "internal-error", "Feature flag storage is unavailable", nil, featureFlagTraceID(ctx)), nil
	}
	flag, err := handler.Flags.Get(ctx, request.Name)
	if errors.Is(err, app.ErrFeatureFlagNotFound) {
		return newFlagProblem(request.Name, "not-found", "Feature flag not found", nil, featureFlagTraceID(ctx)), nil
	}
	if err != nil {
		return newFlagProblem(request.Name, "internal-error", "Feature flag storage is unavailable", nil, featureFlagTraceID(ctx)), nil
	}
	return flagReadResponse{flag: flag, name: request.Name, traceID: featureFlagTraceID(ctx)}, nil
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

type publicFeatureFlag struct {
	Name     string              `json:"name"`
	Enabled  bool                `json:"enabled"`
	Audience domain.FlagAudience `json:"audience"`
	UserIDs  []int64             `json:"user_ids"`
}

type flagReadResponse struct {
	flag    domain.FeatureFlag
	name    string
	traceID string
	problem *rest.Problem
}

func (response flagReadResponse) VisitGetAdminFlagResponse(w http.ResponseWriter) error {
	w.Header().Set("Cache-Control", "private, no-store")
	if response.problem != nil {
		return response.writeProblem(w, response.problem)
	}
	public := publicFeatureFlag{
		Name:     response.flag.Name,
		Enabled:  response.flag.Enabled,
		Audience: response.flag.Audience,
		UserIDs:  append([]int64{}, response.flag.UserIDs...),
	}
	href := "/api/v1/admin/flags/" + url.PathEscape(response.name)
	method := gen.LinkMethod("GET")
	data := gen.RESTFeatureFlag{
		Name:     public.Name,
		Enabled:  public.Enabled,
		Audience: gen.FeatureFlagAudience(public.Audience),
		UserIds:  public.UserIDs,
		Links:    gen.Links{Self: gen.Link{Href: href, Method: &method}},
	}
	body := gen.RESTFeatureFlagEnvelope{
		Code:    http.StatusOK,
		Data:    data,
		Errors:  nil,
		Message: "Success",
		Meta:    map[string]interface{}{},
		Success: gen.RESTFeatureFlagEnvelopeSuccessTrue,
		TraceID: response.traceID,
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return response.writeProblem(w, rest.NewProblem("internal-error", "Feature flag storage is unavailable", nil))
	}
	encoded = append(encoded, '\n')
	return (gen.GetAdminFlag200JSONResponse{
		Body: body,
		Headers: gen.GetAdminFlag200ResponseHeaders{
			CacheControl: "private, no-store",
			ETag:         rest.ETag(encoded),
		},
	}).VisitGetAdminFlagResponse(w)
}

func (response flagReadResponse) writeProblem(w http.ResponseWriter, problem *rest.Problem) error {
	request := &http.Request{URL: &url.URL{Path: "/api/v1/admin/flags/" + url.PathEscape(response.name)}}
	return rest.WriteProblem(w, request, problem, response.traceID)
}

func newFlagProblem(name, slug, detail string, fields rest.FieldErrors, traceID string) flagReadResponse {
	return flagReadResponse{name: name, traceID: traceID, problem: rest.NewProblem(slug, detail, fields)}
}

func featureFlagTraceID(ctx context.Context) string {
	span := trace.SpanContextFromContext(ctx)
	if span.IsValid() {
		return span.TraceID().String()
	}
	return ""
}
