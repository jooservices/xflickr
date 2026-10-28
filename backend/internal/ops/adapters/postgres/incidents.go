package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/ops/app"
	"github.com/jooservices/xflickr/internal/ops/domain"
	"github.com/jooservices/xflickr/internal/platform/db"
	"github.com/jooservices/xflickr/internal/platform/outbox"
)

const incidentColumns = `id, kind, scope, severity, title, detail, state,
       first_seen, last_seen, count, acked_by, closed_at`

const openIncidentSQL = `
INSERT INTO incidents (kind, scope, severity, title, detail, state, first_seen, last_seen, count)
VALUES ($1, $2, $3, $4, $5, 'open', $6, $6, 1)
ON CONFLICT (kind, scope) WHERE state = 'open'
DO UPDATE SET
    last_seen = GREATEST(incidents.last_seen, EXCLUDED.last_seen),
    count = incidents.count + 1
RETURNING ` + incidentColumns + `, (xmax = 0)`

const lockIncidentSQL = `
SELECT ` + incidentColumns + `
FROM incidents
WHERE id = $1
FOR UPDATE`

const acknowledgeIncidentSQL = `
UPDATE incidents
SET state = 'acked', acked_by = $2
WHERE id = $1 AND state = 'open'
RETURNING ` + incidentColumns

const closeIncidentSQL = `
UPDATE incidents
SET state = 'closed', closed_at = $2
WHERE id = $1 AND state IN ('open', 'acked')
RETURNING ` + incidentColumns

const autoCloseIncidentsSQL = `
UPDATE incidents
SET state = 'closed', closed_at = $3
WHERE kind = $1 AND scope = $2 AND state IN ('open', 'acked')`

type incidentScanner interface {
	Scan(...any) error
}

// IncidentRepository persists incident state and transactional notifications.
type IncidentRepository struct {
	unit *db.UnitOfWork
}

// NewIncidentRepository creates an incident store backed by PostgreSQL.
func NewIncidentRepository(database db.Beginner) (*IncidentRepository, error) {
	if database == nil {
		return nil, errors.New("ops: incident postgres database is required")
	}
	return &IncidentRepository{unit: db.NewUnitOfWork(database)}, nil
}

// Open deduplicates active incidents and publishes a notification request for a
// new critical incident in the same transaction.
func (repository *IncidentRepository) Open(ctx context.Context, incident domain.Incident) (domain.Incident, error) {
	if err := repository.ready(ctx); err != nil {
		return domain.Incident{}, err
	}
	if err := incident.ValidateOpen(); err != nil {
		return domain.Incident{}, err
	}
	if len(strings.TrimSpace(string(incident.Detail))) == 0 {
		incident.Detail = []byte("{}")
	}
	now := incident.FirstSeen
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	var opened domain.Incident
	err := repository.unit.Do(ctx, func(tx db.Tx) error {
		created, err := scanIncidentCreated(tx.QueryRow(ctx, openIncidentSQL,
			incident.Kind, incident.Scope, string(incident.Severity), incident.Title,
			incident.Detail, now,
		), &opened)
		if err != nil {
			return fmt.Errorf("ops: upsert incident: %w", err)
		}
		if created && opened.Severity == domain.SeverityCritical {
			request := domain.NotificationRequested{
				Kind:       domain.NotificationIncident,
				IncidentID: opened.ID,
				Severity:   opened.Severity,
				Title:      opened.Title,
				Recipients: []string{"admins"},
				Channels:   []string{"alert"},
			}
			if err := outbox.PublishContext(ctx, tx, domain.NotificationRequestedTopic, fmt.Sprint(opened.ID), request); err != nil {
				return fmt.Errorf("ops: publish critical incident notification: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return domain.Incident{}, err
	}
	return opened, nil
}

// Ack acknowledges an open incident.
func (repository *IncidentRepository) Ack(ctx context.Context, incidentID, userID int64) (domain.Incident, error) {
	if err := repository.ready(ctx); err != nil {
		return domain.Incident{}, err
	}
	if incidentID <= 0 || userID <= 0 {
		return domain.Incident{}, fmt.Errorf("%w: incident and user IDs are required", domain.ErrInvalidIncident)
	}
	var updated domain.Incident
	err := repository.unit.Do(ctx, func(tx db.Tx) error {
		current, err := scanIncident(tx.QueryRow(ctx, lockIncidentSQL, incidentID))
		if errors.Is(err, pgx.ErrNoRows) {
			return app.ErrIncidentNotFound
		}
		if err != nil {
			return fmt.Errorf("ops: lock incident for acknowledgment: %w", err)
		}
		if _, err := current.Acknowledge(userID); err != nil {
			return err
		}
		updated, err = scanIncident(tx.QueryRow(ctx, acknowledgeIncidentSQL, incidentID, userID))
		if err != nil {
			return fmt.Errorf("ops: acknowledge incident: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.Incident{}, err
	}
	return updated, nil
}

// Close closes an open or acknowledged incident.
func (repository *IncidentRepository) Close(ctx context.Context, incidentID int64, at time.Time) (domain.Incident, error) {
	if err := repository.ready(ctx); err != nil {
		return domain.Incident{}, err
	}
	if incidentID <= 0 || at.IsZero() {
		return domain.Incident{}, fmt.Errorf("%w: incident ID and close time are required", domain.ErrInvalidIncident)
	}
	var updated domain.Incident
	err := repository.unit.Do(ctx, func(tx db.Tx) error {
		current, err := scanIncident(tx.QueryRow(ctx, lockIncidentSQL, incidentID))
		if errors.Is(err, pgx.ErrNoRows) {
			return app.ErrIncidentNotFound
		}
		if err != nil {
			return fmt.Errorf("ops: lock incident for close: %w", err)
		}
		if _, err := current.Close(at); err != nil {
			return err
		}
		updated, err = scanIncident(tx.QueryRow(ctx, closeIncidentSQL, incidentID, at.UTC()))
		if err != nil {
			return fmt.Errorf("ops: close incident: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.Incident{}, err
	}
	return updated, nil
}

// AutoClose closes active incidents for one detector kind and scope.
func (repository *IncidentRepository) AutoClose(ctx context.Context, kind, scope string, at time.Time) (int64, error) {
	if err := repository.ready(ctx); err != nil {
		return 0, err
	}
	if err := domain.ValidateIdentity(kind, scope); err != nil {
		return 0, err
	}
	if at.IsZero() {
		return 0, domain.ErrInvalidIncident
	}
	var count int64
	err := repository.unit.Do(ctx, func(tx db.Tx) error {
		result, err := tx.Exec(ctx, autoCloseIncidentsSQL, kind, scope, at.UTC())
		if err != nil {
			return fmt.Errorf("ops: auto-close incidents: %w", err)
		}
		count = result.RowsAffected()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (repository *IncidentRepository) ready(ctx context.Context) error {
	if repository == nil || repository.unit == nil {
		return errors.New("ops: incident postgres repository is required")
	}
	if ctx == nil {
		return errors.New("ops: incident context is required")
	}
	return ctx.Err()
}

func scanIncident(row incidentScanner) (domain.Incident, error) {
	var incident domain.Incident
	var severity, state string
	if err := row.Scan(
		&incident.ID, &incident.Kind, &incident.Scope, &severity, &incident.Title,
		&incident.Detail, &state, &incident.FirstSeen, &incident.LastSeen,
		&incident.Count, &incident.AckedBy, &incident.ClosedAt,
	); err != nil {
		return domain.Incident{}, err
	}
	incident.Severity = domain.Severity(severity)
	incident.State = domain.State(state)
	return incident, nil
}

func scanIncidentCreated(row incidentScanner, incident *domain.Incident) (bool, error) {
	var created bool
	var severity, state string
	if err := row.Scan(
		&incident.ID, &incident.Kind, &incident.Scope, &severity, &incident.Title,
		&incident.Detail, &state, &incident.FirstSeen, &incident.LastSeen,
		&incident.Count, &incident.AckedBy, &incident.ClosedAt, &created,
	); err != nil {
		return false, err
	}
	incident.Severity = domain.Severity(severity)
	incident.State = domain.State(state)
	return created, nil
}

var _ app.IncidentRepository = (*IncidentRepository)(nil)
