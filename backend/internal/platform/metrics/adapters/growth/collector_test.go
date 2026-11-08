package growth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/jooservices/xflickr/internal/platform/metrics/adapters/growth"
	"github.com/jooservices/xflickr/internal/platform/metrics/domain"
)

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

type fakePool struct {
	estimates map[string]int64
	dbSize    int64
	top       []struct {
		name string
		size int64
	}
}

func (f fakePool) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	if contains(query, "pg_total_relation_size") {
		return &topRows{rows: f.top}, nil
	}
	return nil, errors.New("unexpected query")
}

func (f fakePool) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	if contains(query, "pg_database_size") {
		return valueRow{values: []any{f.dbSize}}
	}
	if contains(query, "reltuples") {
		table, _ := args[0].(string)
		return valueRow{values: []any{f.estimates[table]}}
	}
	return valueRow{err: errors.New("unexpected")}
}

type valueRow struct {
	values []any
	err    error
}

func (r valueRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		if d, ok := dest[i].(*int64); ok {
			*d = r.values[i].(int64)
		}
	}
	return nil
}

type topRows struct {
	rows []struct {
		name string
		size int64
	}
	i int
}

func (r *topRows) Close()                                       {}
func (r *topRows) Err() error                                   { return nil }
func (r *topRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *topRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *topRows) Next() bool {
	if r.i >= len(r.rows) {
		return false
	}
	r.i++
	return true
}

func (r *topRows) Scan(dest ...any) error {
	row := r.rows[r.i-1]
	*dest[0].(*string) = row.name
	*dest[1].(*int64) = row.size
	return nil
}
func (r *topRows) Values() ([]any, error) { return nil, nil }
func (r *topRows) RawValues() [][]byte    { return nil }
func (r *topRows) Conn() *pgx.Conn        { return nil }
func (r *topRows) TypeMap() *pgtype.Map   { return nil }

type fakeRedis struct {
	used uint64
	keys int64
	err  error
}

func (f fakeRedis) UsedMemory(context.Context) (uint64, error) { return f.used, f.err }
func (f fakeRedis) KeyCount(context.Context) (int64, error)    { return f.keys, f.err }

type fakeDisk struct {
	used, free uint64
	err        error
}

func (f fakeDisk) Usage(string) (uint64, uint64, error) { return f.used, f.free, f.err }

func contains(s, sub string) bool {
	return len(s) >= len(sub) && stringIndex(s, sub) >= 0
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestGrowthCollectorSkipsMissingRedisAndSearch(t *testing.T) {
	now := time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC)
	collector, err := growth.NewCollector(fakePool{
		estimates: map[string]int64{"people": 10, "photos": 20, "albums": 1, "galleries": 2, "groups": 3, "favorites": 4, "observations": 5, "api_call_log": 6, "work_items": 7, "outbox_events": 8},
		dbSize:    12345,
		top: []struct {
			name string
			size int64
		}{{"photos", 1000}, {"people", 500}},
	}, fixedClock{at: now})
	if err != nil {
		t.Fatal(err)
	}
	collector.Redis = fakeRedis{err: errors.New("down")}
	collector.Search = nil
	collector.MediaPath = "/tmp"
	collector.Disk = fakeDisk{used: 9, free: 99}
	samples, err := collector.Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := domain.ValidateSamples(samples); err != nil {
		t.Fatal(err)
	}
	var sawMedia, sawRedis bool
	for _, sample := range samples {
		if sample.Name == domain.MetricGrowthMediaUsedBytes {
			sawMedia = true
		}
		if sample.Name == domain.MetricGrowthRedisUsedMemory {
			sawRedis = true
		}
	}
	if !sawMedia || sawRedis {
		t.Fatalf("media=%v redis=%v samples=%d", sawMedia, sawRedis, len(samples))
	}
	collector.MainTables = nil
	collector.TopN = 0
	collector.Search = fakeSearchErr{}
	collector.Redis = nil
	collector.MediaPath = "/no/such/path"
	collector.Disk = fakeDisk{err: errors.New("missing")}
	samples, err = collector.Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range samples {
		if sample.Name == domain.MetricGrowthESDocs || sample.Name == domain.MetricGrowthMediaUsedBytes {
			t.Fatalf("unexpected sample %s", sample.Name)
		}
	}
}

type fakeSearchErr struct{}

func (fakeSearchErr) DocumentCount(context.Context) (int64, error) {
	return 0, errors.New("search down")
}
func (fakeSearchErr) StoreBytes(context.Context) (int64, error) { return 0, errors.New("search down") }
