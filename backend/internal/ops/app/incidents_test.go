package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/ops/domain"
)

func TestIncidentManagerLifecycle(t *testing.T) {
	now := time.Date(2026, 10, 28, 20, 0, 0, 0, time.FixedZone("offset", 2*60*60))
	store := &incidentRepositoryStub{}
	manager := NewIncidentsWithClock(store, func() time.Time { return now })
	ctx := t.Context()

	opened, err := manager.Open(ctx, domain.Incident{
		Kind: "crawl.stalled", Scope: "worker-1", Severity: domain.SeverityCritical,
		Title: "Crawl stalled", Detail: json.RawMessage(" "),
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if opened.State != domain.IncidentOpen || opened.Count != 1 || !opened.FirstSeen.Equal(now.UTC()) ||
		string(store.opened.Detail) != "{}" {
		t.Fatalf("Open() incident = %+v, stored = %+v", opened, store.opened)
	}
	if store.openCalls != 1 {
		t.Fatalf("Open calls = %d, want 1", store.openCalls)
	}

	acked, err := manager.Ack(ctx, 9, 41)
	if err != nil || acked.State != domain.IncidentAcked || store.ackUser != 41 {
		t.Fatalf("Ack() = %+v, %v", acked, err)
	}
	closed, err := manager.Close(ctx, 9)
	if err != nil || closed.State != domain.IncidentClosed || !store.closeAt.Equal(now.UTC()) {
		t.Fatalf("Close() = %+v, %v", closed, err)
	}
	count, err := manager.AutoClose(ctx, "crawl.stalled", "worker-1")
	if err != nil || count != 3 || store.autoKind != "crawl.stalled" || store.autoScope != "worker-1" {
		t.Fatalf("AutoClose() = %d, %v", count, err)
	}
}

func TestIncidentManagerRejectsInvalidCalls(t *testing.T) {
	if _, err := (*Incidents)(nil).Open(t.Context(), domain.Incident{}); !errors.Is(err, ErrIncidentRepositoryRequired) {
		t.Fatalf("nil manager error = %v", err)
	}
	if _, err := NewIncidents(nil).Close(t.Context(), 1); !errors.Is(err, ErrIncidentRepositoryRequired) {
		t.Fatalf("missing repository error = %v", err)
	}
	manager := NewIncidents(&incidentRepositoryStub{})
	if _, err := manager.Open(nil, domain.Incident{}); !errors.Is(err, ErrIncidentContextRequired) {
		t.Fatalf("nil context error = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := manager.Open(ctx, domain.Incident{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v", err)
	}
	if _, err := manager.Open(t.Context(), domain.Incident{}); !errors.Is(err, domain.ErrInvalidIncident) {
		t.Fatalf("invalid incident error = %v", err)
	}
	if _, err := manager.Ack(t.Context(), 0, 1); !errors.Is(err, ErrIncidentIDRequired) {
		t.Fatalf("invalid ack id error = %v", err)
	}
	if _, err := manager.Ack(t.Context(), 1, 0); !errors.Is(err, domain.ErrInvalidIncident) {
		t.Fatalf("invalid ack user error = %v", err)
	}
	if _, err := manager.Close(t.Context(), -1); !errors.Is(err, ErrIncidentIDRequired) {
		t.Fatalf("invalid close id error = %v", err)
	}
	if _, err := manager.AutoClose(t.Context(), " ", "scope"); !errors.Is(err, domain.ErrInvalidIncident) {
		t.Fatalf("invalid auto-close identity error = %v", err)
	}
	if _, err := manager.AutoClose(t.Context(), "kind", " "); !errors.Is(err, domain.ErrInvalidIncident) {
		t.Fatalf("invalid auto-close scope error = %v", err)
	}
}

func TestIncidentManagerPropagatesRepositoryErrors(t *testing.T) {
	wantErr := errors.New("database unavailable")
	store := &incidentRepositoryStub{err: wantErr}
	manager := NewIncidents(store)
	input := domain.Incident{Kind: "service.down", Scope: "global", Severity: domain.SeverityInfo, Title: "Service down"}
	if _, err := manager.Open(t.Context(), input); !errors.Is(err, wantErr) {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := manager.Ack(t.Context(), 1, 2); !errors.Is(err, wantErr) {
		t.Fatalf("Ack() error = %v", err)
	}
	if _, err := manager.Close(t.Context(), 1); !errors.Is(err, wantErr) {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := manager.AutoClose(t.Context(), "service.down", "global"); !errors.Is(err, wantErr) {
		t.Fatalf("AutoClose() error = %v", err)
	}
}

type incidentRepositoryStub struct {
	opened    domain.Incident
	openCalls int
	ackUser   int64
	closeAt   time.Time
	autoKind  string
	autoScope string
	err       error
}

func (repository *incidentRepositoryStub) Open(_ context.Context, incident domain.Incident) (domain.Incident, error) {
	repository.openCalls++
	repository.opened = incident
	if repository.err != nil {
		return domain.Incident{}, repository.err
	}
	incident.ID = 9
	return incident, nil
}

func (repository *incidentRepositoryStub) Ack(_ context.Context, id, userID int64) (domain.Incident, error) {
	repository.ackUser = userID
	if repository.err != nil {
		return domain.Incident{}, repository.err
	}
	return domain.Incident{ID: id, State: domain.IncidentAcked}, nil
}

func (repository *incidentRepositoryStub) Close(_ context.Context, id int64, at time.Time) (domain.Incident, error) {
	repository.closeAt = at
	if repository.err != nil {
		return domain.Incident{}, repository.err
	}
	return domain.Incident{ID: id, State: domain.IncidentClosed}, nil
}

func (repository *incidentRepositoryStub) AutoClose(_ context.Context, kind, scope string, _ time.Time) (int64, error) {
	repository.autoKind, repository.autoScope = kind, scope
	if repository.err != nil {
		return 0, repository.err
	}
	return 3, nil
}
