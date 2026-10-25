package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestBudgetLoadsAndValidatesLimits(t *testing.T) {
	query := &fakeQueryer{row: fakeRow{values: []any{int64(1800), int64(4), int64(5), []byte("100"), []byte("0.3")}}}
	budget, err := (Budgets{Pool: query}).Budget(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if budget.HourlyLimit != 1800 || budget.PerSecondRate != 4 || budget.Burst != 5 || budget.Reserve != 100 || budget.PersonLaneShare != .3 {
		t.Fatalf("budget = %+v", budget)
	}
}

func TestBudgetRejectsMissingDependenciesAndQueryFailures(t *testing.T) {
	if _, err := (Budgets{}).Budget(context.Background(), 1); err == nil {
		t.Fatal("Budget accepted nil pool")
	}
	if _, err := (Budgets{Pool: &fakeQueryer{}}).Budget(nil, 1); err == nil {
		t.Fatal("Budget accepted nil context")
	}
	if _, err := (Budgets{Pool: &fakeQueryer{}}).Budget(context.Background(), 0); err == nil {
		t.Fatal("Budget accepted invalid key id")
	}
	query := &fakeQueryer{row: fakeRow{err: errors.New("offline")}}
	if _, err := (Budgets{Pool: query}).Budget(context.Background(), 1); err == nil {
		t.Fatal("Budget ignored query failure")
	}
}

func TestBudgetRejectsInvalidStoredSettings(t *testing.T) {
	for _, values := range [][]any{
		{int64(100), int64(1), int64(1), []byte("bad"), []byte("0.2")},
		{int64(100), int64(1), int64(1), []byte("0"), []byte("2")},
	} {
		query := &fakeQueryer{row: fakeRow{values: values}}
		if _, err := (Budgets{Pool: query}).Budget(context.Background(), 1); err == nil {
			t.Fatal("Budget accepted invalid stored settings")
		}
	}
}

type fakeQueryer struct{ row fakeRow }

func (q *fakeQueryer) QueryRow(context.Context, string, ...any) pgx.Row { return q.row }

type fakeRow struct {
	values []any
	err    error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("unexpected scan count")
	}
	for i, value := range r.values {
		switch target := dest[i].(type) {
		case *int64:
			*target = value.(int64)
		case *[]byte:
			*target = append((*target)[:0], value.([]byte)...)
		default:
			return errors.New("unexpected scan target")
		}
	}
	return nil
}
