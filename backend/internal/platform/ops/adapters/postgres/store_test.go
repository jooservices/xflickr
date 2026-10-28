package postgres

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jooservices/xflickr/internal/platform/ops"
)

func TestSnapshotBuildsHealthSummary(t *testing.T) {
	oldest := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	pool := &snapshotDB{oldest: &oldest}
	snapshot, err := (Store{Pool: pool}).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Counts != (ops.Counts{People: 2, Photos: 3, Favorites: 4, Albums: 5, Galleries: 6, Groups: 7, PhotoSizes: 8}) {
		t.Fatalf("counts = %+v", snapshot.Counts)
	}
	if snapshot.WorkItems["failed"] != 9 || snapshot.RiverJobs["discarded"] != 10 || snapshot.FailedWorkItems != 9 || snapshot.DeadJobs != 10 {
		t.Fatalf("work=%v river=%v", snapshot.WorkItems, snapshot.RiverJobs)
	}
	if snapshot.PhotoSizesJobs != (ops.LaneJobs{Pending: 11, Ready: 12, Running: 13}) {
		t.Fatalf("photo sizes = %+v", snapshot.PhotoSizesJobs)
	}
	if snapshot.Pending.Total != 14 || snapshot.Pending.WithoutKey != 15 || snapshot.Pending.NotBeforeLater != 16 || !snapshot.Pending.Oldest.Equal(oldest) {
		t.Fatalf("pending = %+v", snapshot.Pending)
	}
	if snapshot.CredentialDecryptionFailures != 17 || snapshot.CallsLast10Min != 18 || len(snapshot.Keys) != 1 || snapshot.Keys[0].KeyID != 19 {
		t.Fatalf("other snapshot fields = %+v", snapshot)
	}
	if !pool.sawEstimateFallback || pool.groupedQueries != 4 {
		t.Fatalf("fallback=%v grouped=%d", pool.sawEstimateFallback, pool.groupedQueries)
	}
}

func TestSnapshotReportsEveryQueryFailure(t *testing.T) {
	for failAt := 1; failAt <= 40; failAt++ {
		pool := &snapshotDB{failAt: failAt}
		_, err := (Store{Pool: pool}).Snapshot(context.Background())
		if pool.calls < failAt {
			if err != nil {
				t.Fatalf("snapshot failed without injected query: %v", err)
			}
			return
		}
		if err == nil {
			t.Fatalf("query %d failure was ignored", failAt)
		}
	}
	t.Fatal("snapshot issued more than 40 queries")
}

func TestSnapshotRejectsMissingDependencies(t *testing.T) {
	if _, err := (Store{}).Snapshot(context.Background()); err == nil {
		t.Fatal("Snapshot accepted missing pool")
	}
	if _, err := (Store{Pool: &snapshotDB{}}).Snapshot(nil); err == nil {
		t.Fatal("Snapshot accepted missing context")
	}
}

func TestCountsPreferPositivePlannerEstimates(t *testing.T) {
	pool := &snapshotDB{estimate: 99}
	counts, err := (Store{Pool: pool}).counts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if counts.People != 99 || counts.Photos != 99 || counts.Favorites != 99 || counts.Albums != 99 || counts.Galleries != 99 || counts.Groups != 99 || counts.PhotoSizes != 8 {
		t.Fatalf("counts = %+v", counts)
	}
}

func TestGroupedAndKeysReportRowFailures(t *testing.T) {
	boom := errors.New("injected query failure")
	if _, err := (Store{Pool: &snapshotDB{queryErr: boom}}).grouped(context.Background(), nil, "grouped"); !errors.Is(err, boom) {
		t.Fatalf("grouped query error = %v", err)
	}
	if _, err := (Store{Pool: &snapshotDB{scanErr: boom}}).grouped(context.Background(), nil, "grouped"); !errors.Is(err, boom) {
		t.Fatalf("grouped scan error = %v", err)
	}
	if _, err := (Store{Pool: &snapshotDB{rowsErr: boom}}).grouped(context.Background(), nil, "grouped"); !errors.Is(err, boom) {
		t.Fatalf("grouped rows error = %v", err)
	}
	if _, err := (Store{Pool: &snapshotDB{scanErr: boom}}).keys(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("keys scan error = %v", err)
	}
	if _, err := (Store{Pool: &snapshotDB{rowsErr: boom}}).keys(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("keys rows error = %v", err)
	}
}

type snapshotDB struct {
	pgx.Rows
	oldest              *time.Time
	sawEstimateFallback bool
	groupedQueries      int
	calls               int
	failAt              int
	estimate            int64
	queryErr            error
	scanErr             error
	rowsErr             error
}

func (db *snapshotDB) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	db.calls++
	if db.calls == db.failAt {
		return nil, errors.New("injected query failure")
	}
	if db.queryErr != nil {
		return nil, db.queryErr
	}
	db.groupedQueries++
	switch {
	case strings.Contains(query, "FROM work_items GROUP BY state"):
		return &snapshotRows{rows: [][]any{{"failed", int64(9)}, {"custom", int64(1)}}, scanErr: db.scanErr, err: db.rowsErr}, nil
	case strings.Contains(query, "FROM river_job GROUP BY state"):
		return &snapshotRows{rows: [][]any{{"discarded", int64(10)}}, scanErr: db.scanErr, err: db.rowsErr}, nil
	case strings.Contains(query, "FROM flickr_accounts GROUP BY status"):
		return &snapshotRows{rows: [][]any{{"ok", int64(2)}}, scanErr: db.scanErr, err: db.rowsErr}, nil
	case strings.Contains(query, "FROM api_keys k"):
		return &snapshotRows{rows: [][]any{{int64(19), "primary", "valid", int64(100), int64(20)}}, scanErr: db.scanErr, err: db.rowsErr}, nil
	default:
		return &snapshotRows{rows: [][]any{{"state", int64(1)}}, scanErr: db.scanErr, err: db.rowsErr}, nil
	}
}

func (db *snapshotDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	db.calls++
	if db.calls == db.failAt {
		return snapshotRow{err: errors.New("injected query failure")}
	}
	var values []any
	switch {
	case strings.Contains(query, "pg_class c"):
		db.sawEstimateFallback = true
		values = []any{db.estimate}
	case strings.Contains(query, "FROM river_job WHERE args->>'kind'='photo.sizes'"):
		values = []any{int64(11), int64(12), int64(13)}
	case strings.Contains(query, "SELECT count(*) FROM people"):
		values = []any{int64(2)}
	case strings.Contains(query, "SELECT count(*) FROM photos WHERE sizes_status"):
		values = []any{int64(8)}
	case strings.Contains(query, "SELECT count(*) FROM photos"):
		values = []any{int64(3)}
	case strings.Contains(query, "SELECT count(*) FROM favorites"):
		values = []any{int64(4)}
	case strings.Contains(query, "SELECT count(*) FROM albums"):
		values = []any{int64(5)}
	case strings.Contains(query, "SELECT count(*) FROM galleries"):
		values = []any{int64(6)}
	case strings.Contains(query, "SELECT count(*) FROM groups"):
		values = []any{int64(7)}
	case strings.Contains(query, "credential_decryption_unavailable"):
		values = []any{int64(17)}
	case strings.Contains(query, "api_call_log WHERE called_at"):
		values = []any{int64(18)}
	case strings.Contains(query, "FROM work_items WHERE state = 'pending'"):
		values = []any{int64(14), int64(15), int64(16), db.oldest}
	default:
		values = []any{int64(2)}
	}
	return snapshotRow{values: values}
}

type snapshotRow struct {
	values []any
	err    error
}

func (row snapshotRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	return assignSnapshot(dest, row.values)
}

type snapshotRows struct {
	pgx.Rows
	rows    [][]any
	index   int
	scanErr error
	err     error
}

func (rows *snapshotRows) Next() bool {
	if rows.index >= len(rows.rows) {
		return false
	}
	rows.index++
	return true
}
func (*snapshotRows) Close()          {}
func (rows *snapshotRows) Err() error { return rows.err }
func (rows *snapshotRows) Scan(dest ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	return assignSnapshot(dest, rows.rows[rows.index-1])
}

func assignSnapshot(dest []any, values []any) error {
	if len(dest) != len(values) {
		return errors.New("unexpected number of scan values")
	}
	for i := range dest {
		out, value := reflect.ValueOf(dest[i]), reflect.ValueOf(values[i])
		if out.Kind() != reflect.Pointer || !value.Type().AssignableTo(out.Elem().Type()) {
			return errors.New("unexpected scan value type")
		}
		out.Elem().Set(value)
	}
	return nil
}
