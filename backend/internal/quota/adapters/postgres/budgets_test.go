package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/quota/app"
)

func TestBudgetLoadsAndValidatesLimits(t *testing.T) {
	query := &fakeQueryer{row: fakeRow{values: []any{int64(1800), int64(4), int64(5), []byte("100"), []byte("0.3"), []byte("0.1")}}}
	budget, err := (Budgets{Pool: query}).Budget(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if budget.HourlyLimit != 1800 || budget.PerSecondRate != 4 || budget.Burst != 5 || budget.Reserve != 100 || budget.PersonLaneShare != .3 || budget.PhotoLaneFloorShare != .1 {
		t.Fatalf("budget = %+v", budget)
	}
	if !strings.Contains(query.query, "(usable.value #>> '{}')::bigint") || !strings.Contains(query.query, "usable.key = 'usable_hourly_quota'") {
		t.Fatalf("budget query omits the usable hourly quota: %s", query.query)
	}
}

func TestBudgetUnitUsesSmallestHourlyCeiling(t *testing.T) {
	tests := []struct {
		name   string
		limit  int64
		daily  string
		usable string
		want   int64
	}{
		{name: "usable quota caps key", limit: 3600, daily: "0", usable: "3400", want: 3400},
		{name: "key limit stays lower", limit: 1800, daily: "0", usable: "3400", want: 1800},
		{name: "daily limit caps both", limit: 3600, daily: "2500", usable: "3400", want: 2500},
		{name: "usable quota beats daily limit", limit: 3600, daily: "3600", usable: "3400", want: 3400},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := &fakeQueryer{row: fakeRow{values: []any{test.want, int64(4), int64(5), []byte("100"), []byte("0.3"), []byte("0.1")}}}
			if _, err := (Budgets{Pool: query}).Budget(context.Background(), 7); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(query.query, "LEAST(k.per_hour_limit") || !strings.Contains(query.query, "daily.key = 'daily_call_limit'") {
				t.Fatalf("budget query does not apply all hourly ceilings: %s", query.query)
			}
		})
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
	missing := &fakeQueryer{row: fakeRow{err: pgx.ErrNoRows}}
	if _, err := (Budgets{Pool: missing}).Budget(context.Background(), 1); !errors.Is(err, app.ErrKeyUnavailable) {
		t.Fatalf("missing budget error = %v, want unavailable key", err)
	}
}

func TestBudgetRejectsInvalidStoredSettings(t *testing.T) {
	for _, values := range [][]any{
		{int64(100), int64(1), int64(1), []byte("bad"), []byte("0.2"), []byte("0.1")},
		{int64(100), int64(1), int64(1), []byte("0"), []byte("2"), []byte("0.1")},
		{int64(100), int64(1), int64(1), []byte("0"), []byte("bad"), []byte("0.1")},
		{int64(100), int64(1), int64(1), []byte("0"), []byte("0.3"), []byte("bad")},
		{int64(100), int64(1), int64(1), []byte("0"), []byte("0.3"), []byte("0.51")},
		{int64(0), int64(1), int64(1), []byte("0"), []byte("0.3"), []byte("0.1")},
	} {
		query := &fakeQueryer{row: fakeRow{values: values}}
		if _, err := (Budgets{Pool: query}).Budget(context.Background(), 1); err == nil {
			t.Fatal("Budget accepted invalid stored settings")
		}
	}
}

type fakeQueryer struct {
	row   fakeRow
	query string
}

func (q *fakeQueryer) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	q.query = query
	return q.row
}

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
