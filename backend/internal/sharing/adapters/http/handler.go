package http

import (
	"context"
	"errors"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/api/rest"
	"github.com/jooservices/xflickr/internal/sharing/app"
	"github.com/jooservices/xflickr/internal/sharing/domain"
)

const sharingPath = "/api/v1/me/sharing"

// Handler adapts sharing use cases to the generated REST server.
type Handler struct {
	Change  app.Change
	Read    app.Read
	ActorID func(context.Context) (int64, bool)
}

type sharingResponse struct {
	setting domain.Setting
	status  int
	problem *rest.Problem
	traceID string
}

func (response sharingResponse) write(w http.ResponseWriter) error {
	w.Header().Set("Cache-Control", "private, no-store")
	if response.problem != nil {
		return rest.WriteProblemAt(w, sharingPath, response.problem, response.traceID)
	}
	var changedAt *time.Time
	if !response.setting.ChangedAt.IsZero() {
		value := response.setting.ChangedAt.UTC()
		changedAt = &value
	}
	links := rest.Links{
		"self":   rest.NewLink(sharingPath, http.MethodGet),
		"update": rest.NewLink(sharingPath, http.MethodPut),
		"me":     rest.NewLink("/api/v1/me", http.MethodGet),
	}
	data := map[string]any{"enabled": response.setting.Enabled, "changed_at": changedAt, "links": links}
	return rest.WriteSuccess(w, response.status, http.StatusText(response.status), data, nil, response.traceID)
}

func (response sharingResponse) VisitGetMeSharingResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func (response sharingResponse) VisitPutMeSharingResponse(w http.ResponseWriter) error {
	return response.write(w)
}

func sharingTraceID(ctx context.Context) string {
	span := trace.SpanContextFromContext(ctx)
	if span.IsValid() {
		return span.TraceID().String()
	}
	return ""
}

func (h Handler) GetMeSharing(ctx context.Context, _ gen.GetMeSharingRequestObject) (gen.GetMeSharingResponseObject, error) {
	userID, ok := h.actorID(ctx)
	if !ok {
		return sharingResponse{problem: rest.NewProblem("unauthenticated", "Authentication required", nil), traceID: sharingTraceID(ctx)}, nil
	}
	setting, err := h.Read.Execute(ctx, userID)
	if errors.Is(err, app.ErrInvalidUser) {
		return sharingResponse{problem: rest.NewProblem("unauthenticated", "Authentication required", nil), traceID: sharingTraceID(ctx)}, nil
	}
	if err != nil {
		return nil, err
	}
	return sharingResponse{setting: setting, status: http.StatusOK, traceID: sharingTraceID(ctx)}, nil
}

func (h Handler) PutMeSharing(ctx context.Context, request gen.PutMeSharingRequestObject) (gen.PutMeSharingResponseObject, error) {
	if request.Body == nil {
		return sharingResponse{problem: rest.NewProblem("malformed-request", "An enabled value is required", rest.FieldErrors{"enabled": {"Field is required"}}), traceID: sharingTraceID(ctx)}, nil
	}
	userID, ok := h.actorID(ctx)
	if !ok {
		return sharingResponse{problem: rest.NewProblem("unauthenticated", "Authentication required", nil), traceID: sharingTraceID(ctx)}, nil
	}
	setting, err := h.Change.Execute(ctx, userID, request.Body.Enabled)
	if errors.Is(err, app.ErrInvalidUser) {
		return sharingResponse{problem: rest.NewProblem("unauthenticated", "Authentication required", nil), traceID: sharingTraceID(ctx)}, nil
	}
	if err != nil {
		return nil, err
	}
	return sharingResponse{setting: setting, status: http.StatusOK, traceID: sharingTraceID(ctx)}, nil
}

func (h Handler) actorID(ctx context.Context) (int64, bool) {
	if h.ActorID == nil {
		return 0, false
	}
	userID, ok := h.ActorID(ctx)
	return userID, ok && userID > 0
}
