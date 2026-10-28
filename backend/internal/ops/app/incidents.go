package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jooservices/xflickr/internal/ops/domain"
)

var (
	ErrIncidentRepositoryRequired = errors.New("ops: incident repository is required")
	ErrIncidentNotFound           = errors.New("ops: incident not found")
	ErrIncidentContextRequired    = errors.New("ops: incident context is required")
	ErrIncidentIDRequired         = errors.New("ops: incident id is required")
)

// IncidentRepository persists incident lifecycle changes. Open must deduplicate
// by kind and scope and publish the critical notification in the same transaction.
type IncidentRepository interface {
	Open(context.Context, domain.Incident) (domain.Incident, error)
	Ack(context.Context, int64, int64) (domain.Incident, error)
	Close(context.Context, int64, time.Time) (domain.Incident, error)
	AutoClose(context.Context, string, string, time.Time) (int64, error)
}

// Incidents coordinates incident validation and lifecycle operations.
type Incidents struct {
	repository IncidentRepository
	now        func() time.Time
}

// NewIncidents creates an incident manager using the system clock.
func NewIncidents(repository IncidentRepository) *Incidents {
	return NewIncidentsWithClock(repository, time.Now)
}

// NewIncidentsWithClock creates a manager with a replaceable clock for tests.
func NewIncidentsWithClock(repository IncidentRepository, now func() time.Time) *Incidents {
	if now == nil {
		now = time.Now
	}
	return &Incidents{repository: repository, now: now}
}

// Open creates an incident, or increments the active incident with the same
// kind and scope. Detail defaults to an empty object.
func (manager *Incidents) Open(ctx context.Context, incident domain.Incident) (domain.Incident, error) {
	if err := manager.ready(ctx); err != nil {
		return domain.Incident{}, err
	}
	if len(strings.TrimSpace(string(incident.Detail))) == 0 {
		incident.Detail = json.RawMessage("{}")
	}
	if err := incident.ValidateOpen(); err != nil {
		return domain.Incident{}, err
	}
	now := manager.now().UTC()
	incident.State = domain.IncidentOpen
	incident.FirstSeen = now
	incident.LastSeen = now
	incident.Count = 1
	incident.AckedBy = nil
	incident.ClosedAt = nil
	opened, err := manager.repository.Open(ctx, incident)
	if err != nil {
		return domain.Incident{}, fmt.Errorf("ops: open incident: %w", err)
	}
	return opened, nil
}

// Ack acknowledges an open incident for an administrator.
func (manager *Incidents) Ack(ctx context.Context, incidentID, userID int64) (domain.Incident, error) {
	if err := manager.ready(ctx); err != nil {
		return domain.Incident{}, err
	}
	if incidentID <= 0 {
		return domain.Incident{}, ErrIncidentIDRequired
	}
	if userID <= 0 {
		return domain.Incident{}, fmt.Errorf("%w: acknowledging user is required", domain.ErrInvalidIncident)
	}
	updated, err := manager.repository.Ack(ctx, incidentID, userID)
	if err != nil {
		return domain.Incident{}, fmt.Errorf("ops: acknowledge incident: %w", err)
	}
	return updated, nil
}

// Close closes an open or acknowledged incident.
func (manager *Incidents) Close(ctx context.Context, incidentID int64) (domain.Incident, error) {
	if err := manager.ready(ctx); err != nil {
		return domain.Incident{}, err
	}
	if incidentID <= 0 {
		return domain.Incident{}, ErrIncidentIDRequired
	}
	updated, err := manager.repository.Close(ctx, incidentID, manager.now().UTC())
	if err != nil {
		return domain.Incident{}, fmt.Errorf("ops: close incident: %w", err)
	}
	return updated, nil
}

// AutoClose resolves active incidents for one detector kind and scope. It is
// idempotent and returns the number of rows transitioned.
func (manager *Incidents) AutoClose(ctx context.Context, kind, scope string) (int64, error) {
	if err := manager.ready(ctx); err != nil {
		return 0, err
	}
	if err := domain.ValidateIdentity(kind, scope); err != nil {
		return 0, err
	}
	count, err := manager.repository.AutoClose(ctx, kind, scope, manager.now().UTC())
	if err != nil {
		return 0, fmt.Errorf("ops: auto-close incidents: %w", err)
	}
	return count, nil
}

func (manager *Incidents) ready(ctx context.Context) error {
	if manager == nil || manager.repository == nil {
		return ErrIncidentRepositoryRequired
	}
	if ctx == nil {
		return ErrIncidentContextRequired
	}
	return ctx.Err()
}
