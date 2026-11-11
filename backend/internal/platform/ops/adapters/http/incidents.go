package http

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	opsapp "github.com/jooservices/xflickr/internal/ops/app"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/api/rest"
)

// ListAdminOpenIncidents returns a bounded page without detector-supplied text.
func (h Handler) ListAdminOpenIncidents(ctx context.Context, request gen.ListAdminOpenIncidentsRequestObject) (gen.ListAdminOpenIncidentsResponseObject, error) {
	if h.ActorID == nil {
		return gen.ListAdminOpenIncidents401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	if _, ok := h.ActorID(ctx); !ok {
		return gen.ListAdminOpenIncidents401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	if h.OpenIncidents == nil {
		return internalIncidentListError(), nil
	}

	limit := opsapp.IncidentListDefaultLimit
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	var after *opsapp.IncidentCursor
	if request.Params.Cursor != nil {
		var ok bool
		after, ok = decodeIncidentCursor(*request.Params.Cursor)
		if !ok {
			return invalidIncidentListQuery(), nil
		}
	}
	page, err := h.OpenIncidents.List(ctx, opsapp.IncidentListQuery{Limit: limit, After: after})
	if err != nil {
		if errors.Is(err, opsapp.ErrInvalidIncidentPage) {
			return invalidIncidentListQuery(), nil
		}
		return internalIncidentListError(), nil
	}

	items := make([]gen.AdminOpenIncident, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, gen.AdminOpenIncident{
			ID: item.ID, Severity: gen.AdminOpenIncidentSeverity(item.Severity),
			FirstSeen: item.FirstSeen.UTC(), LastSeen: item.LastSeen.UTC(), Count: item.Count,
		})
	}
	nextCursor := ""
	if page.Next != nil {
		nextCursor = encodeIncidentCursor(*page.Next)
	}
	return gen.ListAdminOpenIncidents200JSONResponse(gen.AdminOpenIncidentPage{
		Items: items, NextCursor: nextCursor, ServerTime: h.now().UTC(),
	}), nil
}

func encodeIncidentCursor(cursor opsapp.IncidentCursor) string {
	value := fmt.Sprintf("%d:%d", cursor.LastSeen.UnixMicro(), cursor.ID)
	return rest.EncodeCursor([]byte(value))
}

func decodeIncidentCursor(value string) (*opsapp.IncidentCursor, bool) {
	if value == "" {
		return nil, true
	}
	decoded, err := rest.DecodeCursor(value)
	if err != nil {
		return nil, false
	}
	parts := strings.Split(string(decoded), ":")
	if len(parts) != 2 {
		return nil, false
	}
	micros, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, false
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 {
		return nil, false
	}
	return &opsapp.IncidentCursor{LastSeen: time.UnixMicro(micros).UTC(), ID: id}, true
}

func invalidIncidentListQuery() gen.ListAdminOpenIncidents400JSONResponse {
	return gen.ListAdminOpenIncidents400JSONResponse(gen.Error{Code: "invalid_request", Message: "Invalid incident list query"})
}

func internalIncidentListError() gen.ListAdminOpenIncidents500JSONResponse {
	return gen.ListAdminOpenIncidents500JSONResponse(gen.Error{Code: "internal_error", Message: "Internal server error"})
}
