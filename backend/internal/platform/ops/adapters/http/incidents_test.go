package http

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/ops/app"
	"github.com/jooservices/xflickr/internal/ops/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/api/rest"
)

func TestListAdminOpenIncidentsReturnsSafeMetadataAndCursor(t *testing.T) {
	at := time.Date(2026, 11, 12, 1, 2, 3, 456000000, time.UTC)
	service, err := app.NewOpenIncidents(&openIncidentsReaderStub{page: app.IncidentPage{
		Items: []app.OpenIncident{{
			ID: 42, Severity: domain.SeverityWarning, FirstSeen: at.Add(-time.Hour), LastSeen: at, Count: 3,
		}},
		Next: &app.IncidentCursor{LastSeen: at, ID: 42},
	}})
	if err != nil {
		t.Fatal(err)
	}
	limit := 10
	handler := Handler{
		OpenIncidents: service,
		ActorID:       func(context.Context) (int64, bool) { return 7, true },
		Now:           func() time.Time { return at.Add(time.Second) },
	}
	response, err := handler.ListAdminOpenIncidents(t.Context(), gen.ListAdminOpenIncidentsRequestObject{
		Params: gen.ListAdminOpenIncidentsParams{Limit: &limit},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, ok := response.(gen.ListAdminOpenIncidents200JSONResponse)
	if !ok || len(body.Items) != 1 || body.Items[0].ID != 42 || body.Items[0].Severity != gen.AdminOpenIncidentSeverityWarning ||
		body.Items[0].Count != 3 || !body.Items[0].LastSeen.Equal(at) || body.NextCursor == "" || !body.ServerTime.Equal(at.Add(time.Second)) {
		t.Fatalf("response = %T %+v", response, response)
	}
}

func TestListAdminOpenIncidentsRejectsUnauthorizedAndInvalidQueries(t *testing.T) {
	limit := 1
	unauthorized := Handler{}.ListAdminOpenIncidents
	response, err := unauthorized(t.Context(), gen.ListAdminOpenIncidentsRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := response.(gen.ListAdminOpenIncidents401JSONResponse); !ok {
		t.Fatalf("unauthenticated response = %T", response)
	}
	service, err := app.NewOpenIncidents(&openIncidentsReaderStub{})
	if err != nil {
		t.Fatal(err)
	}
	handler := Handler{OpenIncidents: service, ActorID: func(context.Context) (int64, bool) { return 1, true }}
	for _, request := range []gen.ListAdminOpenIncidentsRequestObject{
		{Params: gen.ListAdminOpenIncidentsParams{Limit: intPointer(101)}},
		{Params: gen.ListAdminOpenIncidentsParams{Cursor: stringPointer("not-a-cursor")}},
	} {
		response, err := handler.ListAdminOpenIncidents(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := response.(gen.ListAdminOpenIncidents400JSONResponse); !ok {
			t.Errorf("invalid query response = %T", response)
		}
	}
	response, err = handler.ListAdminOpenIncidents(t.Context(), gen.ListAdminOpenIncidentsRequestObject{
		Params: gen.ListAdminOpenIncidentsParams{Limit: &limit},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := response.(gen.ListAdminOpenIncidents200JSONResponse); !ok {
		t.Fatalf("valid query response = %T", response)
	}
}

func TestListAdminOpenIncidentsHidesReaderErrors(t *testing.T) {
	service, err := app.NewOpenIncidents(&openIncidentsReaderStub{err: errors.New("database details must not escape")})
	if err != nil {
		t.Fatal(err)
	}
	handler := Handler{OpenIncidents: service, ActorID: func(context.Context) (int64, bool) { return 1, true }}
	response, err := handler.ListAdminOpenIncidents(t.Context(), gen.ListAdminOpenIncidentsRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	body, ok := response.(gen.ListAdminOpenIncidents500JSONResponse)
	if !ok || body.Message != "Internal server error" {
		t.Fatalf("internal error response = %T %+v", response, response)
	}
}

func TestIncidentCursorRoundTripAndValidation(t *testing.T) {
	want := app.IncidentCursor{LastSeen: time.Date(2026, 11, 12, 1, 2, 3, 456000000, time.UTC), ID: 55}
	encoded := encodeIncidentCursor(want)
	got, ok := decodeIncidentCursor(encoded)
	if !ok || got == nil || got.ID != want.ID || !got.LastSeen.Equal(want.LastSeen) {
		t.Fatalf("decoded cursor = %+v, %v", got, ok)
	}
	for _, value := range []string{"not-base64", encodeInvalidIncidentCursor("not-a-time:4"), encodeInvalidIncidentCursor("1:0")} {
		if _, ok := decodeIncidentCursor(value); ok {
			t.Errorf("decodeIncidentCursor(%q) succeeded", value)
		}
	}
}

type openIncidentsReaderStub struct {
	page  app.IncidentPage
	err   error
	calls int
}

func (reader *openIncidentsReaderStub) ListOpen(context.Context, *app.IncidentCursor, int) (app.IncidentPage, error) {
	reader.calls++
	if reader.err != nil {
		return app.IncidentPage{}, reader.err
	}
	return reader.page, nil
}

func intPointer(value int) *int { return &value }

func stringPointer(value string) *string { return &value }

func encodeInvalidIncidentCursor(value string) string { return rest.EncodeCursor([]byte(value)) }
