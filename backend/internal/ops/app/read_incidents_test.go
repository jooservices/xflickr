package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/ops/domain"
)

func TestOpenIncidentsListsOnlyTheRequestedPage(t *testing.T) {
	at := time.Date(2026, 11, 12, 1, 2, 3, 456000000, time.UTC)
	want := IncidentPage{
		Items: []OpenIncident{{
			ID: 17, Severity: domain.SeverityWarning, FirstSeen: at.Add(-time.Hour), LastSeen: at, Count: 4,
		}},
		Next: &IncidentCursor{LastSeen: at.Add(-time.Minute), ID: 12},
	}
	reader := &openIncidentReaderStub{page: want}
	service, err := NewOpenIncidents(reader)
	if err != nil {
		t.Fatal(err)
	}

	got, err := service.List(t.Context(), IncidentListQuery{
		Limit: 25, After: &IncidentCursor{LastSeen: at.Add(-2 * time.Minute), ID: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].ID != 17 || got.Items[0].Severity != domain.SeverityWarning ||
		!got.Items[0].FirstSeen.Equal(at.Add(-time.Hour)) || !got.Items[0].LastSeen.Equal(at) || got.Items[0].Count != 4 ||
		got.Next == nil || got.Next.ID != 12 {
		t.Fatalf("List() page = %+v", got)
	}
	if reader.calls != 1 || reader.limit != 25 || reader.after == nil || reader.after.ID != 10 {
		t.Fatalf("reader call = %+v, want one call with the supplied page query", reader)
	}
}

func TestOpenIncidentsRejectsInvalidCalls(t *testing.T) {
	if _, err := NewOpenIncidents(nil); !errors.Is(err, ErrIncidentReaderRequired) {
		t.Fatalf("NewOpenIncidents(nil) error = %v", err)
	}
	service, err := NewOpenIncidents(&openIncidentReaderStub{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.List(nil, IncidentListQuery{Limit: 1}); !errors.Is(err, ErrIncidentContextRequired) {
		t.Fatalf("List(nil context) error = %v", err)
	}
	for _, limit := range []int{0, 101} {
		if _, err := service.List(t.Context(), IncidentListQuery{Limit: limit}); !errors.Is(err, ErrInvalidIncidentPage) {
			t.Errorf("List(limit %d) error = %v", limit, err)
		}
	}
	for _, cursor := range []*IncidentCursor{{ID: 1}, {LastSeen: time.Now()}, {LastSeen: time.Now(), ID: 0}} {
		if _, err := service.List(t.Context(), IncidentListQuery{Limit: 1, After: cursor}); !errors.Is(err, ErrInvalidIncidentPage) {
			t.Errorf("List(cursor %+v) error = %v", cursor, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := service.List(ctx, IncidentListQuery{Limit: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("List(canceled context) error = %v", err)
	}
}

func TestOpenIncidentsPropagatesReaderErrors(t *testing.T) {
	want := errors.New("database unavailable")
	service, err := NewOpenIncidents(&openIncidentReaderStub{err: want})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.List(t.Context(), IncidentListQuery{Limit: 1}); !errors.Is(err, want) {
		t.Fatalf("List() error = %v, want %v", err, want)
	}
}

type openIncidentReaderStub struct {
	page  IncidentPage
	err   error
	calls int
	limit int
	after *IncidentCursor
}

func (reader *openIncidentReaderStub) ListOpen(_ context.Context, after *IncidentCursor, limit int) (IncidentPage, error) {
	reader.calls++
	reader.after = after
	reader.limit = limit
	if reader.err != nil {
		return IncidentPage{}, reader.err
	}
	return reader.page, nil
}
