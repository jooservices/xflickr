package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jooservices/xflickr/internal/ops/app"
	"github.com/jooservices/xflickr/internal/ops/domain"
)

func TestOpenIncidentRepositoryReturnsSafeBoundedPage(t *testing.T) {
	at := time.Date(2026, 11, 12, 1, 2, 3, 456000000, time.UTC)
	database := &openIncidentDBStub{rows: &openIncidentRowsStub{items: []openIncidentRow{
		{id: 8, severity: "critical", firstSeen: at.Add(-time.Hour), lastSeen: at, count: 3},
		{id: 7, severity: "warning", firstSeen: at.Add(-2 * time.Hour), lastSeen: at.Add(-time.Minute), count: 1},
	}}}
	repository, err := NewOpenIncidentRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	page, err := repository.ListOpen(t.Context(), nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != 8 || page.Items[0].Severity != domain.SeverityCritical ||
		page.Items[0].Count != 3 || page.Next == nil || page.Next.ID != 8 || !page.Next.LastSeen.Equal(at) {
		t.Fatalf("ListOpen() page = %+v", page)
	}
	if database.queryCount != 1 || database.limit != 2 || database.query != listOpenIncidentsFirstPageSQL {
		t.Fatalf("query = %q, count=%d, limit=%d", database.query, database.queryCount, database.limit)
	}
}

func TestOpenIncidentRepositoryUsesKeysetCursor(t *testing.T) {
	at := time.Date(2026, 11, 12, 1, 2, 3, 456000000, time.UTC)
	database := &openIncidentDBStub{rows: &openIncidentRowsStub{items: []openIncidentRow{}}}
	repository, err := NewOpenIncidentRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ListOpen(t.Context(), &app.IncidentCursor{LastSeen: at, ID: 42}, 10); err != nil {
		t.Fatal(err)
	}
	if database.query != listOpenIncidentsAfterCursorSQL || database.limit != 11 || len(database.args) != 3 ||
		!database.args[0].(time.Time).Equal(at) || database.args[1] != int64(42) {
		t.Fatalf("cursor query args = %q %+v", database.query, database.args)
	}
}

func TestOpenIncidentRepositoryReturnsQueryAndScanErrors(t *testing.T) {
	want := errors.New("database unavailable")
	database := &openIncidentDBStub{queryErr: want}
	repository, err := NewOpenIncidentRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ListOpen(t.Context(), nil, 10); !errors.Is(err, want) {
		t.Fatalf("ListOpen(query error) = %v", err)
	}
	database = &openIncidentDBStub{rows: &openIncidentRowsStub{
		items: []openIncidentRow{{id: 1, severity: "info"}}, scanErr: want,
	}}
	repository, err = NewOpenIncidentRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ListOpen(t.Context(), nil, 10); !errors.Is(err, want) {
		t.Fatalf("ListOpen(scan error) = %v", err)
	}
	database = &openIncidentDBStub{rows: &openIncidentRowsStub{rowsErr: want}}
	repository, err = NewOpenIncidentRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ListOpen(t.Context(), nil, 10); !errors.Is(err, want) {
		t.Fatalf("ListOpen(rows error) = %v", err)
	}
}

type openIncidentDBStub struct {
	rows       pgx.Rows
	queryErr   error
	query      string
	args       []any
	limit      int
	queryCount int
}

func (database *openIncidentDBStub) Query(_ context.Context, query string, args ...any) (pgx.Rows, error) {
	database.queryCount++
	database.query = query
	database.args = args
	database.limit = args[len(args)-1].(int)
	return database.rows, database.queryErr
}

type openIncidentRowsStub struct {
	pgx.Rows
	items   []openIncidentRow
	index   int
	current openIncidentRow
	scanErr error
	rowsErr error
}

type openIncidentRow struct {
	id        int64
	severity  string
	firstSeen time.Time
	lastSeen  time.Time
	count     int64
}

func (rows *openIncidentRowsStub) Next() bool {
	if rows.index >= len(rows.items) {
		return false
	}
	rows.current = rows.items[rows.index]
	rows.index++
	return true
}

func (rows *openIncidentRowsStub) Scan(destinations ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	*destinations[0].(*int64) = rows.current.id
	*destinations[1].(*string) = rows.current.severity
	*destinations[2].(*time.Time) = rows.current.firstSeen
	*destinations[3].(*time.Time) = rows.current.lastSeen
	*destinations[4].(*int64) = rows.current.count
	return nil
}

func (rows *openIncidentRowsStub) Err() error { return rows.rowsErr }
func (rows *openIncidentRowsStub) Close()     {}
