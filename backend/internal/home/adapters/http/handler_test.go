package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/home/app"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"go.opentelemetry.io/otel/trace"
)

type fakeSnapshotter struct {
	snapshot app.Snapshot
	err      error
	userID   int64
	calls    int
}

func (f *fakeSnapshotter) Snapshot(_ context.Context, userID int64) (app.Snapshot, error) {
	f.userID = userID
	f.calls++
	return f.snapshot, f.err
}

func TestGetMeHomeReturnsAvailableDataAndDeclaresMissingSections(t *testing.T) {
	now := time.Date(2026, 11, 12, 1, 0, 0, 0, time.UTC)
	snapshots := &fakeSnapshotter{snapshot: app.Snapshot{
		GeneratedAt:         now,
		Contacts:            app.ContactCounts{Total: 5, Direct: 2, Discovered: 3},
		UnavailableSections: []app.Section{app.SectionPhotos, app.SectionStarredContacts, app.SectionActivity},
	}}
	handler := Handler{Snapshots: snapshots, ActorID: func(context.Context) (int64, bool) { return 31, true }}
	response, err := handler.GetMeHome(t.Context(), gen.GetMeHomeRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	if err := response.VisitGetMeHomeResponse(recorder); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || recorder.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("response = %d, cache-control %q", recorder.Code, recorder.Header().Get("Cache-Control"))
	}
	var envelope struct {
		Success bool `json:"success"`
		Code    int  `json:"code"`
		Data    struct {
			GeneratedAt         time.Time                  `json:"generated_at"`
			Contacts            app.ContactCounts          `json:"contacts"`
			Photos              map[string]json.RawMessage `json:"photos"`
			UnavailableSections []app.Section              `json:"unavailable_sections"`
			Links               map[string]any             `json:"links"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.Success || envelope.Code != http.StatusOK || !envelope.Data.GeneratedAt.Equal(now) {
		t.Fatalf("envelope = %+v", envelope)
	}
	if envelope.Data.Contacts != (app.ContactCounts{Total: 5, Direct: 2, Discovered: 3}) {
		t.Fatalf("contacts = %+v", envelope.Data.Contacts)
	}
	wantUnavailable := []app.Section{app.SectionPhotos, app.SectionStarredContacts, app.SectionActivity}
	if len(envelope.Data.UnavailableSections) != len(wantUnavailable) {
		t.Fatalf("unavailable sections = %v", envelope.Data.UnavailableSections)
	}
	for i, want := range wantUnavailable {
		if envelope.Data.UnavailableSections[i] != want {
			t.Fatalf("unavailable sections = %v, want %v", envelope.Data.UnavailableSections, wantUnavailable)
		}
	}
	if len(envelope.Data.Links) != 2 || envelope.Data.Links["self"] == nil {
		t.Fatalf("links = %v", envelope.Data.Links)
	}
	var body struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, metric := range []string{"total", "sizes_ready", "captioned"} {
		if string(envelope.Data.Photos[metric]) != "null" {
			t.Errorf("photos.%s = %s, want null", metric, envelope.Data.Photos[metric])
		}
	}
	for _, field := range []string{"new_photos", "starred_contacts", "activity"} {
		if _, exists := body.Data[field]; exists {
			t.Errorf("unimplemented field %q was returned", field)
		}
	}
	if snapshots.userID != 31 || snapshots.calls != 1 {
		t.Fatalf("snapshot call user=%d calls=%d", snapshots.userID, snapshots.calls)
	}
}

func TestGetMeHomeRejectsMissingActor(t *testing.T) {
	response, err := (Handler{Snapshots: &fakeSnapshotter{}}).
		GetMeHome(t.Context(), gen.GetMeHomeRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	if err := response.VisitGetMeHomeResponse(recorder); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestGetMeHomeRejectsInvalidSnapshotUser(t *testing.T) {
	handler := Handler{
		Snapshots: &fakeSnapshotter{err: app.ErrInvalidUser},
		ActorID:   func(context.Context) (int64, bool) { return 31, true },
	}
	response, err := handler.GetMeHome(t.Context(), gen.GetMeHomeRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	if err := response.VisitGetMeHomeResponse(recorder); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestGetMeHomeCarriesTraceID(t *testing.T) {
	traceID, err := trace.TraceIDFromHex("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	spanID, err := trace.SpanIDFromHex("0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	ctx := trace.ContextWithSpanContext(t.Context(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	}))
	handler := Handler{
		Snapshots: &fakeSnapshotter{},
		ActorID:   func(context.Context) (int64, bool) { return 31, true },
	}
	response, err := handler.GetMeHome(ctx, gen.GetMeHomeRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	if err := response.VisitGetMeHomeResponse(recorder); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(recorder.Body.String(), traceID.String()) {
		t.Fatalf("response trace_id did not include %s: %s", traceID, recorder.Body.String())
	}
}

func TestGetMeHomePropagatesSnapshotError(t *testing.T) {
	readErr := errors.New("database unavailable")
	handler := Handler{Snapshots: &fakeSnapshotter{err: readErr}, ActorID: func(context.Context) (int64, bool) { return 31, true }}
	if _, err := handler.GetMeHome(t.Context(), gen.GetMeHomeRequestObject{}); !errors.Is(err, readErr) {
		t.Fatalf("GetMeHome() error = %v", err)
	}
}

func TestGetMeHomeRequiresSnapshotService(t *testing.T) {
	handler := Handler{ActorID: func(context.Context) (int64, bool) { return 31, true }}
	if _, err := handler.GetMeHome(t.Context(), gen.GetMeHomeRequestObject{}); err == nil {
		t.Fatal("GetMeHome() succeeded without snapshot service")
	}
}
