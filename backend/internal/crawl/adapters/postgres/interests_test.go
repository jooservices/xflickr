package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	crawldomain "github.com/jooservices/xflickr/internal/crawl/domain"
)

func TestNewRequiresDatabase(t *testing.T) {
	if _, err := New(nil); !errors.Is(err, errDatabaseRequired) {
		t.Fatalf("New(nil) error = %v", err)
	}
}

func TestMethodsValidateRepositoryContextAndInput(t *testing.T) {
	var repository *Repository
	if err := repository.Add(t.Context(), crawldomain.Interest{}); !errors.Is(err, errDatabaseRequired) {
		t.Fatalf("nil Add() error = %v", err)
	}
	if _, err := repository.ForPerson(t.Context(), "person"); !errors.Is(err, errDatabaseRequired) {
		t.Fatalf("nil ForPerson() error = %v", err)
	}
	valid := &Repository{pool: nil}
	if err := valid.Add(t.Context(), crawldomain.Interest{}); !errors.Is(err, errDatabaseRequired) {
		t.Fatalf("unconfigured Add() error = %v", err)
	}
	if _, err := valid.ForPerson(t.Context(), "person"); !errors.Is(err, errDatabaseRequired) {
		t.Fatalf("unconfigured ForPerson() error = %v", err)
	}
	database := &databaseFake{}
	repository = &Repository{pool: database}
	if err := repository.Add(nil, crawldomain.Interest{}); !errors.Is(err, errContextRequired) {
		t.Fatalf("nil context Add() error = %v", err)
	}
	if _, err := repository.ForPerson(nil, "person"); !errors.Is(err, errContextRequired) {
		t.Fatalf("nil context ForPerson() error = %v", err)
	}
	if err := repository.Add(t.Context(), crawldomain.Interest{}); !errors.Is(err, crawldomain.ErrInvalidInterest) {
		t.Fatalf("invalid Add() error = %v", err)
	}
	if _, err := repository.ForPerson(t.Context(), " "); !errors.Is(err, crawldomain.ErrInvalidInterest) {
		t.Fatalf("empty ForPerson() error = %v", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := repository.Add(canceled, crawldomain.Interest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Add() error = %v", err)
	}
	if _, err := repository.ForPerson(canceled, "person"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ForPerson() error = %v", err)
	}
}

func TestAddExecutesUpsertAndWrapsDatabaseErrors(t *testing.T) {
	database := &databaseFake{}
	repository := &Repository{pool: database}
	interest := crawldomain.Interest{UserID: 3, PersonNSID: " person ", Depth: 1, Via: " contact "}
	if err := repository.Add(t.Context(), interest); err != nil {
		t.Fatal(err)
	}
	if database.execCalls != 1 || len(database.execArgs) != 4 || database.execArgs[1] != "person" || database.execArgs[3] != "contact" {
		t.Fatalf("Exec args = %#v", database.execArgs)
	}
	database.execErr = errors.New("write failed")
	if err := repository.Add(t.Context(), interest); err == nil || !errors.Is(err, database.execErr) {
		t.Fatalf("Add() error = %v, want wrapped write failure", err)
	}
}

func TestForPersonScansRowsAndReportsFailures(t *testing.T) {
	database := &databaseFake{queryRows: &rowsFake{values: []crawldomain.InterestDepth{{UserID: 1, Depth: 2}, {UserID: 4, Depth: 0}}}}
	repository := &Repository{pool: database}
	got, err := repository.ForPerson(t.Context(), " person ")
	if err != nil || len(got) != 2 || got[0].UserID != 1 || got[1].Depth != 0 || database.queryArgs[0] != "person" {
		t.Fatalf("ForPerson() = %+v, %v", got, err)
	}
	database.queryErr = errors.New("query failed")
	if _, err := repository.ForPerson(t.Context(), "person"); !errors.Is(err, database.queryErr) {
		t.Fatalf("ForPerson() query error = %v", err)
	}
	database.queryErr = nil
	database.queryRows = &rowsFake{scanErr: errors.New("scan failed"), values: []crawldomain.InterestDepth{{UserID: 1}}}
	if _, err := repository.ForPerson(t.Context(), "person"); err == nil {
		t.Fatal("ForPerson() ignored scan failure")
	}
	database.queryRows = &rowsFake{rowsErr: errors.New("rows failed")}
	if _, err := repository.ForPerson(t.Context(), "person"); err == nil {
		t.Fatal("ForPerson() ignored row iteration failure")
	}
}

type databaseFake struct {
	execErr   error
	execCalls int
	execArgs  []any
	queryErr  error
	queryRows rows
	queryArgs []any
}

func (database *databaseFake) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	database.execCalls++
	database.execArgs = args
	return pgconn.NewCommandTag("INSERT 0 1"), database.execErr
}

func (database *databaseFake) Query(_ context.Context, _ string, args ...any) (rows, error) {
	database.queryArgs = args
	return database.queryRows, database.queryErr
}

type rowsFake struct {
	values  []crawldomain.InterestDepth
	index   int
	scanErr error
	rowsErr error
}

func (rows *rowsFake) Next() bool {
	if rows.index >= len(rows.values) {
		return false
	}
	rows.index++
	return true
}

func (rows *rowsFake) Scan(values ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	current := rows.values[rows.index-1]
	*values[0].(*int64) = current.UserID
	*values[1].(*int) = current.Depth
	return nil
}
func (rows *rowsFake) Err() error { return rows.rowsErr }
func (rows *rowsFake) Close()     {}
