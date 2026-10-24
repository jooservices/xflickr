package gen

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type queryRow struct{}

func (queryRow) Scan(destinations ...any) error {
	*destinations[0].(*string) = "xflickr_test"
	return nil
}

func (queryRow) Err() error { return nil }

type queryDB struct{}

func (queryDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (queryDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, nil
}

func (queryDB) QueryRow(context.Context, string, ...any) pgx.Row { return queryRow{} }

func TestCurrentDatabaseNameUsesQuerier(t *testing.T) {
	queries := New(queryDB{})
	name, err := queries.CurrentDatabaseName(context.Background())
	if err != nil {
		t.Fatalf("CurrentDatabaseName() error = %v", err)
	}
	if name != "xflickr_test" {
		t.Fatalf("CurrentDatabaseName() = %q", name)
	}

	if got := queries.WithTx(nil); got.db != nil {
		t.Fatal("WithTx(nil) did not use the provided transaction")
	}
}
