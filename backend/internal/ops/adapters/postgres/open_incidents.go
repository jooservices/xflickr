package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/ops/app"
	"github.com/jooservices/xflickr/internal/ops/domain"
)

const listOpenIncidentsFirstPageSQL = `
SELECT id, severity, first_seen, last_seen, count
FROM incidents
WHERE state = 'open'
ORDER BY last_seen DESC, id DESC
LIMIT $1`

const listOpenIncidentsAfterCursorSQL = `
SELECT id, severity, first_seen, last_seen, count
FROM incidents
WHERE state = 'open' AND (last_seen, id) < ($1::timestamptz, $2::bigint)
ORDER BY last_seen DESC, id DESC
LIMIT $3`

type incidentListQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// OpenIncidentRepository reads the safe fields required by the Issues slice.
type OpenIncidentRepository struct {
	database incidentListQueryer
}

var _ app.IncidentPageReader = (*OpenIncidentRepository)(nil)

// NewOpenIncidentRepository creates a bounded incident reader.
func NewOpenIncidentRepository(database incidentListQueryer) (*OpenIncidentRepository, error) {
	if database == nil {
		return nil, errors.New("ops: incident list database is required")
	}
	return &OpenIncidentRepository{database: database}, nil
}

// ListOpen returns at most limit open incidents and a cursor when more exist.
func (repository *OpenIncidentRepository) ListOpen(ctx context.Context, after *app.IncidentCursor, limit int) (app.IncidentPage, error) {
	if repository == nil || repository.database == nil {
		return app.IncidentPage{}, errors.New("ops: incident list repository is required")
	}
	if ctx == nil {
		return app.IncidentPage{}, errors.New("ops: incident list context is required")
	}
	if err := ctx.Err(); err != nil {
		return app.IncidentPage{}, err
	}
	if limit < 1 || limit > app.IncidentListMaxLimit {
		return app.IncidentPage{}, app.ErrInvalidIncidentPage
	}

	query, args := listOpenIncidentQuery(after, limit+1)
	rows, err := repository.database.Query(ctx, query, args...)
	if err != nil {
		return app.IncidentPage{}, fmt.Errorf("ops: query open incidents: %w", err)
	}
	defer rows.Close()

	page := app.IncidentPage{Items: make([]app.OpenIncident, 0, limit)}
	for rows.Next() {
		if len(page.Items) == limit {
			last := page.Items[len(page.Items)-1]
			page.Next = &app.IncidentCursor{LastSeen: last.LastSeen, ID: last.ID}
			break
		}
		var incident app.OpenIncident
		var severity string
		if err := rows.Scan(&incident.ID, &severity, &incident.FirstSeen, &incident.LastSeen, &incident.Count); err != nil {
			return app.IncidentPage{}, fmt.Errorf("ops: scan open incident: %w", err)
		}
		incident.Severity = domain.Severity(severity)
		page.Items = append(page.Items, incident)
	}
	if err := rows.Err(); err != nil {
		return app.IncidentPage{}, fmt.Errorf("ops: iterate open incidents: %w", err)
	}
	return page, nil
}

func listOpenIncidentQuery(after *app.IncidentCursor, limit int) (string, []any) {
	if after == nil {
		return listOpenIncidentsFirstPageSQL, []any{limit}
	}
	return listOpenIncidentsAfterCursorSQL, []any{after.LastSeen.UTC(), after.ID, limit}
}
