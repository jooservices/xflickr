package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jooservices/xflickr/internal/ops/domain"
)

const (
	IncidentListDefaultLimit = 50
	IncidentListMaxLimit     = 100
)

var (
	ErrIncidentReaderRequired = errors.New("ops: incident reader is required")
	ErrInvalidIncidentPage    = errors.New("ops: invalid incident page")
)

// OpenIncident contains only fields approved for the first administrator read.
type OpenIncident struct {
	ID        int64
	Severity  domain.Severity
	FirstSeen time.Time
	LastSeen  time.Time
	Count     int64
}

// IncidentCursor identifies the final item in a stable newest-first page.
type IncidentCursor struct {
	LastSeen time.Time
	ID       int64
}

// IncidentPage is one bounded page of safe open-incident metadata.
type IncidentPage struct {
	Items []OpenIncident
	Next  *IncidentCursor
}

// IncidentListQuery selects a bounded page of open incidents.
type IncidentListQuery struct {
	Limit int
	After *IncidentCursor
}

// IncidentPageReader reads open incidents without exposing detector payloads.
type IncidentPageReader interface {
	ListOpen(context.Context, *IncidentCursor, int) (IncidentPage, error)
}

// OpenIncidents provides the bounded read use case for administrator incidents.
type OpenIncidents struct {
	reader IncidentPageReader
}

// NewOpenIncidents creates an open-incident reader.
func NewOpenIncidents(reader IncidentPageReader) (*OpenIncidents, error) {
	if reader == nil {
		return nil, ErrIncidentReaderRequired
	}
	return &OpenIncidents{reader: reader}, nil
}

// List returns a stable, bounded page of open incidents.
func (service *OpenIncidents) List(ctx context.Context, query IncidentListQuery) (IncidentPage, error) {
	if service == nil || service.reader == nil {
		return IncidentPage{}, ErrIncidentReaderRequired
	}
	if ctx == nil {
		return IncidentPage{}, ErrIncidentContextRequired
	}
	if err := ctx.Err(); err != nil {
		return IncidentPage{}, err
	}
	if query.Limit < 1 || query.Limit > IncidentListMaxLimit {
		return IncidentPage{}, ErrInvalidIncidentPage
	}
	if query.After != nil && (query.After.ID <= 0 || query.After.LastSeen.IsZero() ||
		query.After.LastSeen.Nanosecond()%int(time.Microsecond) != 0) {
		return IncidentPage{}, ErrInvalidIncidentPage
	}
	page, err := service.reader.ListOpen(ctx, query.After, query.Limit)
	if err != nil {
		return IncidentPage{}, fmt.Errorf("ops: list open incidents: %w", err)
	}
	return page, nil
}
