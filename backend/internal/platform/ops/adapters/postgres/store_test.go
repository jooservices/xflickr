package postgres

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jooservices/xflickr/internal/platform/ops"
)

func TestSnapshotUsesTransactionTimeout(t *testing.T) {
	oldest := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	pool := &snapshotDB{oldest: &oldest, estimate: 3}
	txDB := &beginDB{pool: pool}
	snapshot, err := (Store{Pool: pool, DB: txDB, StatementTimeout: 2 * time.Second}).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !txDB.began || !txDB.committed || !pool.sawTimeout || snapshot.Counts.Photos != 3 {
		t.Fatalf("began=%v committed=%v timeout=%v counts=%+v", txDB.began, txDB.committed, pool.sawTimeout, snapshot.Counts)
	}
}

func TestSnapshotBuildsHealthSummary(t *testing.T) {
	oldest := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	pool := &snapshotDB{oldest: &oldest, estimate: 3}
	snapshot, err := (Store{Pool: pool}).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Counts != (ops.Counts{People: 3, Photos: 3, Favorites: 3, Albums: 3, Galleries: 3, Groups: 3, PhotoSizes: 8}) {
		t.Fatalf("counts = %+v", snapshot.Counts)
	}
	if !snapshot.CountsEstimated {
		t.Fatal("expected counts_estimated")
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
	if snapshot.CredentialDecryptionFailures != 17 || snapshot.CallsLast10Min != 18 || snapshot.QuotaWaitsLastHour != 21 || snapshot.QuotaWaitsLast10Min != 22 || len(snapshot.Keys) != 1 || snapshot.Keys[0].KeyID != 19 {
		t.Fatalf("other snapshot fields = %+v", snapshot)
	}
	if pool.groupedQueries != 4 {
		t.Fatalf("grouped=%d", pool.groupedQueries)
	}
}

func TestSnapshotReportsEveryQueryFailure(t *testing.T) {
	for failAt := 1; failAt <= 40; failAt++ {
		pool := &snapshotDB{failAt: failAt, estimate: 1}
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
	counts, estimated, err := (Store{Pool: pool}).counts(context.Background(), pool, &ops.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if !estimated {
		t.Fatal("expected estimated")
	}
	if counts.People != 99 || counts.Photos != 99 || counts.Favorites != 99 || counts.Albums != 99 || counts.Galleries != 99 || counts.Groups != 99 || counts.PhotoSizes != 8 {
		t.Fatalf("counts = %+v", counts)
	}
}

func TestCountsNeverExactScanLargeTables(t *testing.T) {
	pool := &snapshotDB{estimate: 0}
	counts, _, err := (Store{Pool: pool}).counts(context.Background(), pool, &ops.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if counts.Photos != 0 || counts.Favorites != 0 {
		t.Fatalf("large tables must stay at estimate 0, got photos=%d favorites=%d", counts.Photos, counts.Favorites)
	}
	if pool.exactPhotos || pool.exactFavorites {
		t.Fatal("exact count(*) on photos/favorites must not run")
	}
	if counts.People != 2 {
		t.Fatalf("small table fallback people=%d", counts.People)
	}
}

func TestDegradedSnapshotOnStatementTimeout(t *testing.T) {
	pool := &snapshotDB{
		estimate:      1,
		timeoutQuery:  "pg_class c",
		timeoutOnCall: 1,
	}
	snapshot, err := (Store{Pool: pool}).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Degraded {
		t.Fatalf("expected degraded snapshot: %+v", snapshot)
	}
	if len(snapshot.DegradedQueries) == 0 {
		t.Fatal("expected degraded query names")
	}
}

func TestGroupedAndKeysReportRowFailures(t *testing.T) {
	boom := errors.New("injected query failure")
	snap := &ops.Snapshot{}
	if _, err := (Store{Pool: &snapshotDB{queryErr: boom}}).grouped(context.Background(), &snapshotDB{queryErr: boom}, snap, "grouped", nil, "grouped"); !errors.Is(err, boom) {
		t.Fatalf("grouped query error = %v", err)
	}
	if _, err := (Store{Pool: &snapshotDB{scanErr: boom}}).grouped(context.Background(), &snapshotDB{scanErr: boom}, snap, "grouped", nil, "grouped"); !errors.Is(err, boom) {
		t.Fatalf("grouped scan error = %v", err)
	}
	if _, err := (Store{Pool: &snapshotDB{rowsErr: boom}}).grouped(context.Background(), &snapshotDB{rowsErr: boom}, snap, "grouped", nil, "grouped"); !errors.Is(err, boom) {
		t.Fatalf("grouped rows error = %v", err)
	}
	if _, err := (Store{Pool: &snapshotDB{scanErr: boom}}).keys(context.Background(), &snapshotDB{scanErr: boom}, snap); !errors.Is(err, boom) {
		t.Fatalf("keys scan error = %v", err)
	}
	if _, err := (Store{Pool: &snapshotDB{rowsErr: boom}}).keys(context.Background(), &snapshotDB{rowsErr: boom}, snap); !errors.Is(err, boom) {
		t.Fatalf("keys rows error = %v", err)
	}
}

func TestLoadHealthSettingsBounds(t *testing.T) {
	pool := &snapshotDB{settings: map[string]string{
		"ops.health_interval":          "9",
		"ops.health_statement_timeout": "61",
	}}
	got, err := LoadHealthSettings(context.Background(), pool)
	if err != nil {
		t.Fatal(err)
	}
	if got.Interval != 10*time.Second || got.StatementTimeout != time.Minute {
		t.Fatalf("clamped settings = %+v", got)
	}
	mid := &snapshotDB{settings: map[string]string{
		"ops.health_interval":          "120",
		"ops.health_statement_timeout": "15",
	}}
	got, err = LoadHealthSettings(context.Background(), mid)
	if err != nil {
		t.Fatal(err)
	}
	if got.Interval != 2*time.Minute || got.StatementTimeout != 15*time.Second {
		t.Fatalf("mid settings = %+v", got)
	}
	if _, err := LoadHealthSettings(context.Background(), &snapshotDB{settings: map[string]string{"ops.health_interval": "nope"}}); err == nil {
		t.Fatal("expected parse error")
	}
}

type snapshotDB struct {
	pgx.Rows
	oldest         *time.Time
	groupedQueries int
	calls          int
	failAt         int
	estimate       int64
	queryErr       error
	scanErr        error
	rowsErr        error
	exactPhotos    bool
	exactFavorites bool
	timeoutQuery   string
	timeoutOnCall  int
	timeoutCalls   int
	settings       map[string]string
	sawTimeout     bool
}

type beginDB struct {
	pool      *snapshotDB
	began     bool
	committed bool
}

func (b *beginDB) Begin(context.Context) (pgx.Tx, error) {
	b.began = true
	return &snapshotTx{db: b.pool, parent: b}, nil
}

type snapshotTx struct {
	db     *snapshotDB
	parent *beginDB
}

func (tx *snapshotTx) Begin(context.Context) (pgx.Tx, error) { return tx, nil }
func (tx *snapshotTx) Commit(context.Context) error          { tx.parent.committed = true; return nil }
func (tx *snapshotTx) Rollback(context.Context) error        { return nil }
func (tx *snapshotTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (tx *snapshotTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (tx *snapshotTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }

func (tx *snapshotTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (tx *snapshotTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return tx.db.Exec(ctx, sql, args...)
}

func (tx *snapshotTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return tx.db.Query(ctx, sql, args...)
}

func (tx *snapshotTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return tx.db.QueryRow(ctx, sql, args...)
}
func (tx *snapshotTx) Conn() *pgx.Conn { return nil }

func (db *snapshotDB) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "statement_timeout") {
		db.sawTimeout = true
	}
	return pgconn.CommandTag{}, nil
}

func (db *snapshotDB) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	db.calls++
	if db.calls == db.failAt {
		return nil, errors.New("injected query failure")
	}
	if db.queryErr != nil {
		return nil, db.queryErr
	}
	if strings.Contains(query, "FROM app_settings") {
		rows := make([][]any, 0, len(db.settings))
		for key, value := range db.settings {
			rows = append(rows, []any{key, value})
		}
		return &snapshotRows{rows: rows, scanErr: db.scanErr, err: db.rowsErr}, nil
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

func (db *snapshotDB) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	db.calls++
	if db.calls == db.failAt {
		return snapshotRow{err: errors.New("injected query failure")}
	}
	if db.timeoutQuery != "" && strings.Contains(query, db.timeoutQuery) {
		db.timeoutCalls++
		if db.timeoutOnCall == 0 || db.timeoutCalls == db.timeoutOnCall {
			return snapshotRow{err: &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}}
		}
	}
	var values []any
	switch {
	case strings.Contains(query, "FROM photos") && strings.Contains(query, "tombstoned_at IS NULL"):
		values = []any{int64(5)}
	case strings.Contains(query, "FROM river_job") && strings.Contains(query, "photo.sizes"):
		values = []any{int64(6), int64(12), int64(13)} // waiting=6 => pending 5+6=11
	case strings.Contains(query, "most_common_freqs") || strings.Contains(query, "WITH ORDINALITY"):
		values = []any{int64(8)}
	case strings.Contains(query, "pg_class c"):
		values = []any{db.estimate}
	case strings.Contains(query, "SELECT count(*) FROM people"):
		values = []any{int64(2)}
	case strings.Contains(query, "SELECT count(*) FROM photos"):
		db.exactPhotos = true
		values = []any{int64(3)}
	case strings.Contains(query, "SELECT count(*) FROM favorites"):
		db.exactFavorites = true
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
		values = []any{int64(18), int64(21), int64(22)}
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
