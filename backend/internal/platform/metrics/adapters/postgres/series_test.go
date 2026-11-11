package postgres

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	metricsapp "github.com/jooservices/xflickr/internal/platform/metrics/app"
	"github.com/jooservices/xflickr/internal/platform/metrics/domain"
)

type seriesQuerierStub struct {
	rows     pgx.Rows
	queryErr error
	query    string
	args     []any
}

func (s *seriesQuerierStub) Query(_ context.Context, query string, args ...any) (pgx.Rows, error) {
	s.query = query
	s.args = append([]any(nil), args...)
	return s.rows, s.queryErr
}

type seriesRowsStub struct {
	pgx.Rows
	values    []seriesRowValue
	index     int
	scanErr   error
	rowsErr   error
	closeCall bool
}

type seriesRowValue struct {
	at    time.Time
	value float64
}

func (r *seriesRowsStub) Next() bool {
	if r.index >= len(r.values) {
		return false
	}
	r.index++
	return true
}

func (r *seriesRowsStub) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	if len(dest) != 2 {
		return errors.New("unexpected scan destination count")
	}
	*dest[0].(*time.Time) = r.values[r.index-1].at
	*dest[1].(*float64) = r.values[r.index-1].value
	return nil
}

func (r *seriesRowsStub) Err() error { return r.rowsErr }

func (r *seriesRowsStub) Close() { r.closeCall = true }

func TestSeriesStoreReadSeries(t *testing.T) {
	from := time.Date(2026, 11, 11, 11, 0, 0, 0, time.FixedZone("UTC+3", 3*60*60))
	to := from.Add(time.Hour)
	at := from.UTC()
	rows := &seriesRowsStub{values: []seriesRowValue{{at: at, value: 0}, {at: at.Add(time.Minute), value: 12}}}
	pool := &seriesQuerierStub{rows: rows}
	query := metricsapp.SeriesQuery{Queue: "default", Role: "scheduler", Node: "node-1", From: from, To: to}
	points, err := (SeriesStore{Pool: pool}).ReadSeries(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 || points[0] != (metricsapp.Point{At: at, Value: 0}) || points[1] != (metricsapp.Point{At: at.Add(time.Minute), Value: 12}) {
		t.Fatalf("points=%+v", points)
	}
	wantHash, err := domain.LabelsHash(map[string]string{"queue": "default", "role": "scheduler", "node": "node-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pool.args) != 4 || pool.args[0] != domain.MetricQueueJobsAvailable || pool.args[1] != wantHash || !pool.args[2].(time.Time).Equal(from.UTC()) || !pool.args[3].(time.Time).Equal(to.UTC()) {
		t.Fatalf("query arguments=%v", pool.args)
	}
	for _, fragment := range []string{"SELECT bucket, value", "name = $1", "labels_hash = $2", "bucket >= $3", "bucket < $4", "ORDER BY bucket ASC", "LIMIT 60"} {
		if !strings.Contains(pool.query, fragment) {
			t.Errorf("query missing %q: %s", fragment, pool.query)
		}
	}
	if strings.Contains(strings.ToLower(pool.query), "count(") || strings.Contains(pool.query, "river_job") {
		t.Fatalf("query reads unrelated or aggregate data: %s", pool.query)
	}
	if !rows.closeCall {
		t.Fatal("rows were not closed")
	}
}

func TestSeriesStoreReadSeriesEmpty(t *testing.T) {
	rows := &seriesRowsStub{}
	pool := &seriesQuerierStub{rows: rows}
	points, err := (SeriesStore{Pool: pool}).ReadSeries(t.Context(), metricsapp.SeriesQuery{})
	if err != nil || points == nil || len(points) != 0 || !rows.closeCall {
		t.Fatalf("points=%v err=%v closed=%t", points, err, rows.closeCall)
	}
}

func TestSeriesStoreReadSeriesErrors(t *testing.T) {
	query := metricsapp.SeriesQuery{Queue: "default", Role: "scheduler", Node: "node-1"}
	tests := []struct {
		name string
		pool *seriesQuerierStub
		ctx  context.Context
	}{
		{name: "nil context", pool: &seriesQuerierStub{rows: &seriesRowsStub{}}},
		{name: "nil pool"},
		{name: "query error", pool: &seriesQuerierStub{queryErr: errors.New("query failed")}, ctx: context.Background()},
		{name: "nil rows", pool: &seriesQuerierStub{}, ctx: context.Background()},
		{name: "scan error", pool: &seriesQuerierStub{rows: &seriesRowsStub{values: []seriesRowValue{{value: 1}}, scanErr: errors.New("scan failed")}}, ctx: context.Background()},
		{name: "rows error", pool: &seriesQuerierStub{rows: &seriesRowsStub{rowsErr: errors.New("rows failed")}}, ctx: context.Background()},
		{name: "NaN value", pool: &seriesQuerierStub{rows: &seriesRowsStub{values: []seriesRowValue{{value: math.NaN()}}}}, ctx: context.Background()},
		{name: "infinite value", pool: &seriesQuerierStub{rows: &seriesRowsStub{values: []seriesRowValue{{value: math.Inf(1)}}}}, ctx: context.Background()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.name == "nil context" {
				test.ctx = nil
			} else if test.ctx == nil {
				test.ctx = context.Background()
			}
			store := SeriesStore{}
			if test.pool != nil {
				store.Pool = test.pool
			}
			points, err := store.ReadSeries(test.ctx, query)
			if err == nil || points != nil {
				t.Fatalf("points=%v err=%v, want error", points, err)
			}
			if test.pool != nil && test.name != "nil context" && test.name != "query error" && test.name != "nil rows" {
				if rows, ok := test.pool.rows.(*seriesRowsStub); ok && !rows.closeCall {
					t.Fatal("rows were not closed after an error")
				}
			}
		})
	}
}
