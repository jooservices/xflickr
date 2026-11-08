package river_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/jooservices/xflickr/internal/platform/metrics/adapters/river"
	"github.com/jooservices/xflickr/internal/platform/metrics/domain"
)

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

type fakeQuerier struct {
	queueRows []queueRow
	backlog   int64
	oldest    *time.Time
	queryErr  error
	outboxErr error
}

type queueRow struct {
	queue                                               string
	available, running, retryable, discarded, completed float64
	oldest                                              *float64
}

func (f fakeQuerier) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	if contains(query, "river_job") {
		return &fakeRows{rows: f.queueRows}, nil
	}
	return nil, pgx.ErrNoRows
}

func (f fakeQuerier) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	if f.outboxErr != nil {
		return errRow{err: f.outboxErr}
	}
	if contains(query, "outbox_events") {
		return outboxRow{backlog: f.backlog, oldest: f.oldest}
	}
	return errRow{err: pgx.ErrNoRows}
}

type fakeRows struct {
	rows []queueRow
	i    int
	err  error
}

func (r *fakeRows) Close()                                       {}
func (r *fakeRows) Err() error                                   { return r.err }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) Next() bool {
	if r.i >= len(r.rows) {
		return false
	}
	r.i++
	return true
}
func (r *fakeRows) Scan(dest ...any) error {
	row := r.rows[r.i-1]
	*(dest[0].(*string)) = row.queue
	*(dest[1].(*float64)) = row.available
	*(dest[2].(*float64)) = row.running
	*(dest[3].(*float64)) = row.retryable
	*(dest[4].(*float64)) = row.discarded
	*(dest[5].(*float64)) = row.completed
	*(dest[6].(**float64)) = row.oldest
	return nil
}
func (r *fakeRows) Values() ([]any, error) { return nil, nil }
func (r *fakeRows) RawValues() [][]byte    { return nil }
func (r *fakeRows) Conn() *pgx.Conn        { return nil }
func (r *fakeRows) TypeMap() *pgtype.Map   { return nil }

type outboxRow struct {
	backlog int64
	oldest  *time.Time
}

func (r outboxRow) Scan(dest ...any) error {
	*(dest[0].(*int64)) = r.backlog
	*(dest[1].(**time.Time)) = r.oldest
	return nil
}

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || stringIndex(s, sub) >= 0)
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestRiverCollectorEmitsCatalogueSamples(t *testing.T) {
	now := time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC)
	age := 12.5
	oldest := now.Add(-30 * time.Second)
	collector, err := river.NewCollector(fakeQuerier{
		queueRows: []queueRow{{queue: "photo", available: 2, running: 1, retryable: 0, discarded: 3, completed: 4, oldest: &age}},
		backlog:   7,
		oldest:    &oldest,
	}, fixedClock{at: now}, "crawl", "node-1")
	if err != nil {
		t.Fatal(err)
	}
	samples, err := collector.Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := domain.ValidateSamples(samples); err != nil {
		t.Fatal(err)
	}
	byName := map[string]float64{}
	for _, sample := range samples {
		byName[sample.Name] = sample.Value
	}
	if byName[domain.MetricQueueJobsAvailable] != 2 || byName[domain.MetricOutboxBacklog] != 7 {
		t.Fatalf("samples=%v", byName)
	}
	if byName[domain.MetricQueueWorkerHeartbeat] != 1 {
		t.Fatal("missing heartbeat")
	}
}
