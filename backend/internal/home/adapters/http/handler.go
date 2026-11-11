// Package http adapts Home snapshot use cases to the generated REST server.
package http

import (
	"context"
	"errors"
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/jooservices/xflickr/internal/home/app"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/api/rest"
)

const homePath = "/api/v1/me/home"

type snapshotter interface {
	Snapshot(ctx context.Context, userID int64) (app.Snapshot, error)
}

// Handler adapts the user-scoped Home snapshot to HTTP.
type Handler struct {
	Snapshots snapshotter
	ActorID   func(context.Context) (int64, bool)
}

type homeResponse struct {
	snapshot app.Snapshot
	problem  *rest.Problem
	traceID  string
}

func (response homeResponse) VisitGetMeHomeResponse(w http.ResponseWriter) error {
	w.Header().Set("Cache-Control", "private, no-store")
	if response.problem != nil {
		return rest.WriteProblemAt(w, homePath, response.problem, response.traceID)
	}
	data := map[string]any{
		"generated_at": response.snapshot.GeneratedAt.UTC(),
		"contacts": map[string]int64{
			"total": response.snapshot.Contacts.Total, "direct": response.snapshot.Contacts.Direct,
			"discovered": response.snapshot.Contacts.Discovered,
		},
		"photos":               response.snapshot.Photos,
		"unavailable_sections": response.snapshot.UnavailableSections,
		"links": rest.Links{
			"self": rest.NewLink(homePath, http.MethodGet),
			"me":   rest.NewLink("/api/v1/me", http.MethodGet),
		},
	}
	return rest.WriteSuccess(w, http.StatusOK, "Success", data, nil, response.traceID)
}

func (h Handler) GetMeHome(ctx context.Context, _ gen.GetMeHomeRequestObject) (gen.GetMeHomeResponseObject, error) {
	userID, ok := h.actorID(ctx)
	if !ok {
		return homeResponse{problem: rest.NewProblem("unauthenticated", "Authentication required", nil), traceID: homeTraceID(ctx)}, nil
	}
	if h.Snapshots == nil {
		return nil, errors.New("home: snapshot service is required")
	}
	snapshot, err := h.Snapshots.Snapshot(ctx, userID)
	if errors.Is(err, app.ErrInvalidUser) {
		return homeResponse{problem: rest.NewProblem("unauthenticated", "Authentication required", nil), traceID: homeTraceID(ctx)}, nil
	}
	if err != nil {
		return nil, err
	}
	return homeResponse{snapshot: snapshot, traceID: homeTraceID(ctx)}, nil
}

func (h Handler) actorID(ctx context.Context) (int64, bool) {
	if h.ActorID == nil {
		return 0, false
	}
	userID, ok := h.ActorID(ctx)
	return userID, ok && userID > 0
}

func homeTraceID(ctx context.Context) string {
	span := trace.SpanContextFromContext(ctx)
	if span.IsValid() {
		return span.TraceID().String()
	}
	return ""
}
