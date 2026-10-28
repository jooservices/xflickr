package river

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jooservices/xflickr/internal/platform/queue"
	"github.com/jooservices/xflickr/internal/schedule/app"
	"github.com/jooservices/xflickr/internal/schedule/domain"
)

type coverageRow struct {
	values []any
	err    error
}

func (r coverageRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return fmt.Errorf("scan arity %d != %d", len(dest), len(r.values))
	}
	for i, value := range r.values {
		ptr := reflect.ValueOf(dest[i])
		if ptr.Kind() != reflect.Pointer || ptr.IsNil() {
			return fmt.Errorf("destination %d is not a pointer", i)
		}
		v := reflect.ValueOf(value)
		if !v.IsValid() {
			ptr.Elem().SetZero()
			continue
		}
		if v.Type().AssignableTo(ptr.Elem().Type()) {
			ptr.Elem().Set(v)
			continue
		}
		if v.Type().ConvertibleTo(ptr.Elem().Type()) {
			ptr.Elem().Set(v.Convert(ptr.Elem().Type()))
			continue
		}
		return fmt.Errorf("cannot scan %T into %T", value, dest[i])
	}
	return nil
}

type coverageRows struct {
	pgx.Rows
	values  [][]any
	index   int
	err     error
	scanErr error
	closed  bool
}

func coverageRowsOf(values ...[]any) *coverageRows { return &coverageRows{values: values, index: -1} }
func (r *coverageRows) Next() bool                 { r.index++; return r.index < len(r.values) }
func (r *coverageRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	return (coverageRow{values: r.values[r.index]}).Scan(dest...)
}
func (r *coverageRows) Err() error { return r.err }
func (r *coverageRows) Close()     { r.closed = true }

type coverageTx struct {
	pgx.Tx
	queryFn       func(context.Context, string, ...any) (pgx.Rows, error)
	rowFn         func(context.Context, string, ...any) pgx.Row
	execFn        func(context.Context, string, ...any) (pgconn.CommandTag, error)
	commitErr     error
	commitCount   int
	rollbackCount int
}

func (tx *coverageTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if tx.queryFn == nil {
		return coverageRowsOf(), nil
	}
	return tx.queryFn(ctx, sql, args...)
}
func (tx *coverageTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if tx.rowFn == nil {
		return coverageRow{err: errors.New("unexpected tx query row")}
	}
	return tx.rowFn(ctx, sql, args...)
}
func (tx *coverageTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if tx.execFn == nil {
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	return tx.execFn(ctx, sql, args...)
}
func (tx *coverageTx) Commit(context.Context) error   { tx.commitCount++; return tx.commitErr }
func (tx *coverageTx) Rollback(context.Context) error { tx.rollbackCount++; return nil }

type coverageConnection struct {
	rowFn    func(context.Context, string, ...any) pgx.Row
	execFn   func(context.Context, string, ...any) (pgconn.CommandTag, error)
	beginFn  func(context.Context) (pgx.Tx, error)
	released bool
}

func (c *coverageConnection) Begin(ctx context.Context) (pgx.Tx, error) {
	if c.beginFn == nil {
		return nil, errors.New("unexpected connection begin")
	}
	return c.beginFn(ctx)
}
func (c *coverageConnection) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if c.execFn == nil {
		return pgconn.NewCommandTag("SELECT 1"), nil
	}
	return c.execFn(ctx, sql, args...)
}
func (c *coverageConnection) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if c.rowFn == nil {
		return coverageRow{err: errors.New("unexpected connection query row")}
	}
	return c.rowFn(ctx, sql, args...)
}
func (c *coverageConnection) Release() { c.released = true }

type coverageDatabase struct {
	acquireFn func(context.Context) (scheduleConnection, error)
	beginFn   func(context.Context) (pgx.Tx, error)
	execFn    func(context.Context, string, ...any) (pgconn.CommandTag, error)
	queryFn   func(context.Context, string, ...any) (pgx.Rows, error)
	rowFn     func(context.Context, string, ...any) pgx.Row
}

func (db *coverageDatabase) Acquire(ctx context.Context) (scheduleConnection, error) {
	if db.acquireFn == nil {
		return nil, errors.New("unexpected acquire")
	}
	return db.acquireFn(ctx)
}
func (db *coverageDatabase) Begin(ctx context.Context) (pgx.Tx, error) {
	if db.beginFn == nil {
		return nil, errors.New("unexpected begin")
	}
	return db.beginFn(ctx)
}
func (db *coverageDatabase) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if db.execFn == nil {
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	return db.execFn(ctx, sql, args...)
}
func (db *coverageDatabase) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if db.queryFn == nil {
		return coverageRowsOf(), nil
	}
	return db.queryFn(ctx, sql, args...)
}
func (db *coverageDatabase) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if db.rowFn == nil {
		return coverageRow{err: errors.New("unexpected query row")}
	}
	return db.rowFn(ctx, sql, args...)
}

type coverageJobs struct {
	jobs    []queue.Job
	options []queue.Options
	err     error
}

func (q *coverageJobs) Enqueue(_ context.Context, _ pgx.Tx, job queue.Job, options queue.Options) error {
	q.jobs = append(q.jobs, job)
	q.options = append(q.options, options)
	return q.err
}
func (*coverageJobs) Register(string, queue.Handler) error { return nil }

type coverageCredits struct {
	capacity int
	err      error
	calls    int
}

func (c *coverageCredits) Capacity(context.Context, int64, time.Duration) (int, error) {
	c.calls++
	return c.capacity, c.err
}

type unavailableKey struct{}

func (unavailableKey) Error() string        { return "key unavailable" }
func (unavailableKey) KeyUnavailable() bool { return true }

func coverageSettings() app.PumpSettings {
	return app.PumpSettings{PerKeyReadyCap: 10, NodeReadyCap: 20, MediaReadyCap: 10, AIReadyCap: 10, SyncReadyCap: 10, RetryMax: 3, RetryBackoff: time.Second, LeaseTimeout: time.Minute, AgingBatchSize: 10, MaxDepth: 5, ScoreWeights: domain.ScoreWeights{PlanWeight: 1, DirectAction: 1, Starred: 1, Depth: 1, AgeMinutes: 1}}
}

func TestLoadSettingsReadsValidSettingsAndRejectsMalformedValues(t *testing.T) {
	ctx := context.Background()
	good := map[string]string{"per_key_ready_cap": "10", "node_ready_cap": "20", "media_ready_cap": "7", "ai_ready_cap": "8", "sync_ready_cap": "9", "retry_max": "4", "retry_backoff": "3", "lease_timeout": "30", "max_depth": "5", "score_aging_batch_size": "11", "score_weight_plan": "1.5", "score_weight_direct_action": "2", "score_weight_starred": "3", "score_weight_depth": "4", "score_weight_age_minutes": "0.5"}
	validRows := func(values map[string]string) *coverageRows {
		rows := make([][]any, 0, len(values))
		for key, value := range values {
			rows = append(rows, []any{key, value})
		}
		return coverageRowsOf(rows...)
	}
	pool := &coverageDatabase{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) { return validRows(good), nil }}
	got, err := loadSettings(ctx, pool)
	if err != nil || got.MediaReadyCap != 7 || got.RetryBackoff != 3*time.Second || got.ScoreWeights.AgeMinutes != 0.5 {
		t.Fatalf("settings=%#v err=%v", got, err)
	}
	tests := []struct {
		name     string
		queryErr error
		rows     *coverageRows
		values   map[string]string
		want     string
	}{
		{name: "query", queryErr: errors.New("offline"), want: "query pump settings"},
		{name: "scan", rows: &coverageRows{index: -1, values: [][]any{{int64(1)}}, scanErr: errors.New("scan")}, want: "scan pump setting"},
		{name: "iteration", rows: &coverageRows{index: -1, err: errors.New("iterate")}, want: "read pump settings"},
		{name: "missing", values: map[string]string{}, want: "is missing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := &coverageDatabase{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				if tc.rows != nil {
					return tc.rows, nil
				}
				if tc.queryErr != nil {
					return nil, tc.queryErr
				}
				return validRows(tc.values), nil
			}}
			_, err := loadSettings(ctx, db)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want %q", err, tc.want)
			}
		})
	}
	for _, tc := range []struct{ name, key, value, want string }{
		{name: "integer parse", key: "retry_max", value: "bad", want: "parse pump setting retry_max"},
		{name: "nonpositive age batch", key: "score_aging_batch_size", value: "0", want: "score aging batch size must be positive"},
		{name: "decimal parse", key: "score_weight_plan", value: "bad", want: "parse pump setting score_weight_plan"},
		{name: "settings validation", key: "media_ready_cap", value: "-1", want: "invalid pump settings"},
		{name: "score validation", key: "score_weight_plan", value: "-1", want: "invalid score settings"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{}
			for key, value := range good {
				values[key] = value
			}
			values[tc.key] = tc.value
			db := &coverageDatabase{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) { return validRows(values), nil }}
			_, err := loadSettings(ctx, db)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want %q", err, tc.want)
			}
		})
	}
	if _, err := loadSettings(nil, pool); err == nil || !strings.Contains(err.Error(), "settings context") {
		t.Fatalf("nil context error=%v", err)
	}
}

func TestPumpLeaderTransactionsAndKeyHandling(t *testing.T) {
	ctx := context.Background()
	settings := coverageSettings()
	jobs := &coverageJobs{}
	credits := &coverageCredits{capacity: 5}
	if _, err := (Store{}).Pump(ctx, settings, credits, jobs); err == nil {
		t.Fatal("accepted missing backend")
	}
	if _, err := (Store{database: &coverageDatabase{}}).Pump(ctx, settings, nil, jobs); err == nil {
		t.Fatal("accepted missing credit provider")
	}
	if _, err := (Store{database: &coverageDatabase{}}).Pump(nil, settings, credits, jobs); err == nil {
		t.Fatal("accepted nil context")
	}
	if _, err := (Store{database: &coverageDatabase{acquireFn: func(context.Context) (scheduleConnection, error) { return nil, errors.New("acquire") }}}).Pump(ctx, settings, credits, jobs); err == nil || !strings.Contains(err.Error(), "acquire leader") {
		t.Fatalf("acquire error=%v", err)
	}
	for _, tc := range []struct {
		name                                                         string
		lockRow                                                      pgx.Row
		leader                                                       bool
		beginErr, assignErr, keylessErr, listErr, pumpErr, commitErr error
		keys                                                         []int64
		want                                                         int
		wantErr                                                      string
	}{
		{name: "not leader"},
		{name: "lock query failure", lockRow: coverageRow{err: errors.New("lock")}, wantErr: "leader lock"},
		{name: "transaction begin failure", leader: true, beginErr: errors.New("begin"), wantErr: "begin pump"},
		{name: "owner key assignment failure", leader: true, assignErr: errors.New("assign"), wantErr: "assign owner keys"},
		{name: "keyless scan failure", leader: true, keylessErr: errors.New("keyless"), wantErr: "count keyless pending"},
		{name: "key listing failure", leader: true, listErr: errors.New("list"), wantErr: "list"},
		{name: "unavailable key skipped", leader: true, keys: []int64{1, 2}},
		{name: "pump failure", leader: true, keys: []int64{1}, pumpErr: errors.New("quota"), wantErr: "quota"},
		{name: "commit failure", leader: true, keys: []int64{1}, commitErr: errors.New("commit"), wantErr: "commit pump"},
		{name: "success", leader: true, keys: []int64{1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &coverageTx{rowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				if strings.Contains(sql, "count(*)") {
					if tc.keylessErr != nil {
						return coverageRow{err: tc.keylessErr}
					}
					return coverageRow{values: []any{int64(0)}}
				}
				return coverageRow{err: fmt.Errorf("unexpected tx row %s", sql)}
			}, execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "UPDATE work_items w SET key_id") && tc.assignErr != nil {
					return pgconn.CommandTag{}, tc.assignErr
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			}, commitErr: tc.commitErr}
			conn := &coverageConnection{rowFn: func(context.Context, string, ...any) pgx.Row {
				if tc.lockRow != nil {
					return tc.lockRow
				}
				return coverageRow{values: []any{tc.leader}}
			}, beginFn: func(context.Context) (pgx.Tx, error) {
				if tc.beginErr != nil {
					return nil, tc.beginErr
				}
				return tx, nil
			}}
			db := &coverageDatabase{acquireFn: func(context.Context) (scheduleConnection, error) { return conn, nil }}
			store := Store{database: db, listKeys: func(context.Context, pgx.Tx) ([]int64, error) {
				if tc.listErr != nil {
					return nil, tc.listErr
				}
				return tc.keys, nil
			}, inflight: func(_ context.Context, _ pgx.Tx, key int64, worker string) (int, error) {
				if tc.pumpErr != nil && worker == "" && key == 1 {
					return 0, tc.pumpErr
				}
				if tc.name == "unavailable key skipped" && key == 1 {
					return 0, unavailableKey{}
				}
				if tc.name == "unavailable key skipped" && key == 2 {
					return 0, nil
				}
				return 0, nil
			}}
			got, err := store.Pump(ctx, settings, credits, jobs)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error=%v want %q", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("dispatched=%d want=%d", got, tc.want)
			}
			if !conn.released {
				t.Fatal("leader connection was not released")
			}
		})
	}
}

func TestPumpHelpersAndQueueDispatch(t *testing.T) {
	ctx := context.Background()
	settings := coverageSettings()
	tx := &coverageTx{}
	if got := configuredQueues(settings); len(got) != 3 || got[0].name != "media" || got[1].name != "ai" || got[2].name != "sync" {
		t.Fatalf("queues=%#v", got)
	}
	for _, tc := range []struct {
		name       string
		credits    int
		creditErr  error
		counts     []int
		countErrAt int
		settings   app.PumpSettings
		queryRows  []*coverageRows
		queryErrAt int
		execErr    error
		queueErr   error
		want       int
		wantErr    string
	}{
		{name: "no credits", credits: 0},
		{name: "credit provider error", creditErr: errors.New("credits"), wantErr: "quota credits"},
		{name: "count error", credits: 5, countErrAt: 2, wantErr: "count failed"},
		{name: "key cap reached", credits: 5, counts: []int{10}},
		{name: "worker limit reached", credits: 5, settings: app.PumpSettings{PerKeyReadyCap: 10, NodeReadyCap: 20, MediaReadyCap: 0, AIReadyCap: 0, SyncReadyCap: 0}},
		{name: "share group and selected job", credits: 5, want: 1},
		{name: "count backlog error", credits: 5, queryErrAt: 1, wantErr: "count plan backlog"},
		{name: "scan backlog error", credits: 5, queryRows: []*coverageRows{{index: -1, values: [][]any{{"bad"}}}}, wantErr: "scan plan backlog"},
		{name: "read backlog error", credits: 5, queryRows: []*coverageRows{{index: -1, err: errors.New("read")}}, wantErr: "read plan backlog"},
		{name: "invalid share", credits: 5, queryRows: []*coverageRows{{index: -1, values: [][]any{{int64(1), -1.0, 1}}}}, wantErr: "invalid share group"},
		{name: "select pending error", credits: 5, queryErrAt: 2, wantErr: "select pending work"},
		{name: "scan pending error", credits: 5, queryRows: []*coverageRows{{index: -1, values: [][]any{{int64(1), 1.0, 1}}}, {index: -1, values: [][]any{{"bad"}}}}, wantErr: "scan pending work"},
		{name: "read pending error", credits: 5, queryRows: []*coverageRows{{index: -1, values: [][]any{{int64(1), 1.0, 1}}}, {index: -1, err: errors.New("read")}}, wantErr: "read pending work"},
		{name: "enqueue error", credits: 5, queryRows: []*coverageRows{{index: -1, values: [][]any{{int64(1), 1.0, 1}}}, {index: -1, values: [][]any{{int64(1), "job", "subject", json.RawMessage(`{}`)}}}}, queueErr: errors.New("enqueue"), wantErr: "enqueue"},
		{name: "lease update error", credits: 5, queryRows: []*coverageRows{{index: -1, values: [][]any{{int64(1), 1.0, 1}}}, {index: -1, values: [][]any{{int64(1), "job", "subject", json.RawMessage(`{}`)}}}}, execErr: errors.New("update"), wantErr: "mark work ready"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := tc.settings
			if limits == (app.PumpSettings{}) {
				limits = settings
			}
			queryCount := 0
			tx := &coverageTx{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				queryCount++
				if tc.queryErrAt == queryCount {
					return nil, errors.New("query")
				}
				if len(tc.queryRows) >= queryCount {
					return tc.queryRows[queryCount-1], nil
				}
				if queryCount == 1 {
					return coverageRowsOf([]any{int64(1), 1.0, 1}), nil
				}
				if queryCount == 2 {
					return coverageRowsOf([]any{int64(1), "job", "subject", json.RawMessage(`{}`)}), nil
				}
				return coverageRowsOf(), nil
			}, execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				if tc.execErr != nil {
					return pgconn.CommandTag{}, tc.execErr
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			}}
			countCalls := 0
			store := Store{inflight: func(context.Context, pgx.Tx, int64, string) (int, error) {
				countCalls++
				if tc.countErrAt == countCalls {
					return 0, errors.New("count failed")
				}
				if len(tc.counts) > countCalls-1 {
					return tc.counts[countCalls-1], nil
				}
				return 0, nil
			}}
			credit := &coverageCredits{capacity: tc.credits, err: tc.creditErr}
			jobs := &coverageJobs{err: tc.queueErr}
			got, err := store.pumpKey(ctx, tx, 1, limits, credit, jobs, store.inflight)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error=%v want %q", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("dispatched=%d want=%d", got, tc.want)
			}
		})
	}
	_ = tx
	for _, tc := range []struct {
		name    string
		err     error
		want    int
		wantErr string
	}{
		{name: "key unavailable skipped", want: 0},
		{name: "other failure stops", err: errors.New("quota"), wantErr: "quota"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			store := Store{inflight: func(context.Context, pgx.Tx, int64, string) (int, error) {
				calls++
				if tc.name == "key unavailable skipped" && calls == 1 {
					return 0, unavailableKey{}
				}
				if tc.err != nil {
					return 0, tc.err
				}
				return 0, nil
			}}
			got, err := store.pumpKeys(ctx, &coverageTx{}, []int64{1, 2}, settings, &coverageCredits{capacity: 5}, &coverageJobs{})
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error=%v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got=%d want=%d", got, tc.want)
			}
		})
	}
}

func TestScheduleSQLHelpersAndRescore(t *testing.T) {
	ctx := context.Background()
	if err := assignOwnerKeys(ctx, &coverageTx{}); err != nil {
		t.Fatal(err)
	}
	if err := assignOwnerKeys(ctx, &coverageTx{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, errors.New("assign")
	}}); err == nil {
		t.Fatal("ignored assignment error")
	}
	if err := warnKeylessPending(ctx, &coverageTx{rowFn: func(context.Context, string, ...any) pgx.Row { return coverageRow{err: errors.New("count")} }}); err == nil {
		t.Fatal("ignored warning count error")
	}
	for _, count := range []int64{0, 2} {
		if err := warnKeylessPending(ctx, &coverageTx{rowFn: func(context.Context, string, ...any) pgx.Row { return coverageRow{values: []any{count}} }}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name     string
		rows     pgx.Rows
		queryErr error
		wantErr  string
		want     int
	}{
		{name: "query error", queryErr: errors.New("query"), wantErr: "list pending keys"},
		{name: "scan error", rows: &coverageRows{index: -1, values: [][]any{{"bad"}}}, wantErr: "scan pending key"},
		{name: "rows error", rows: &coverageRows{index: -1, err: errors.New("iterate")}, wantErr: "read pending keys"},
		{name: "success", rows: coverageRowsOf([]any{int64(3)}, []any{int64(7)}), want: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &coverageTx{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) { return tc.rows, tc.queryErr }}
			keys, err := pendingKeys(ctx, tx)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error=%v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if len(keys) != tc.want {
				t.Fatalf("keys=%v", keys)
			}
		})
	}
	if _, err := countInflight(ctx, &coverageTx{rowFn: func(context.Context, string, ...any) pgx.Row { return coverageRow{err: errors.New("count")} }}, 1, "media"); err == nil {
		t.Fatal("ignored inflight error")
	}
	if got, err := countInflight(ctx, &coverageTx{rowFn: func(context.Context, string, ...any) pgx.Row { return coverageRow{values: []any{int(4)}} }}, 1, "media"); err != nil || got != 4 {
		t.Fatalf("count=%d err=%v", got, err)
	}

	settings := coverageSettings()
	tests := []struct {
		name                                                     string
		beginErr, queryErr, scanErr, rowsErr, execErr, commitErr error
		weights                                                  domain.ScoreWeights
		maxDepth                                                 int
		want                                                     int
		wantErr                                                  string
	}{
		{name: "success", maxDepth: 5, want: 1},
		{name: "begin error", beginErr: errors.New("begin"), wantErr: "begin score aging"},
		{name: "select error", queryErr: errors.New("query"), wantErr: "select score aging batch"},
		{name: "scan error", scanErr: errors.New("scan"), wantErr: "scan score aging row"},
		{name: "score error", weights: domain.ScoreWeights{PlanWeight: -1}, maxDepth: 5, wantErr: "score values"},
		{name: "rows error", rowsErr: errors.New("iterate"), maxDepth: 5, wantErr: "read score aging rows"},
		{name: "update error", execErr: errors.New("update"), maxDepth: 5, wantErr: "store work score"},
		{name: "commit error", commitErr: errors.New("commit"), maxDepth: 5, wantErr: "commit score aging"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			weights := tc.weights
			if weights == (domain.ScoreWeights{}) {
				weights = settings.ScoreWeights
			}
			tx := &coverageTx{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				if tc.queryErr != nil {
					return nil, tc.queryErr
				}
				return &coverageRows{index: -1, values: [][]any{{int64(1), 2, true, true, 1, 10.0}}, scanErr: tc.scanErr, err: tc.rowsErr}, nil
			}, execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				if tc.execErr != nil {
					return pgconn.CommandTag{}, tc.execErr
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			}, commitErr: tc.commitErr}
			db := &coverageDatabase{beginFn: func(context.Context) (pgx.Tx, error) {
				if tc.beginErr != nil {
					return nil, tc.beginErr
				}
				return tx, nil
			}}
			got, err := (Store{database: db}).RecomputeScores(ctx, 3, weights, tc.maxDepth)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error=%v want %q", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("count=%d want=%d", got, tc.want)
			}
		})
	}
	store := Store{database: &coverageDatabase{beginFn: func(context.Context) (pgx.Tx, error) {
		return &coverageTx{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) { return coverageRowsOf(), nil }}, nil
	}}}
	if _, err := store.RecomputeScores(ctx, 1, settings.ScoreWeights, -1); err == nil {
		t.Fatal("accepted negative max depth")
	}
	if _, err := store.RescorePerson(ctx, 0, "person", settings.ScoreWeights, 1); err == nil {
		t.Fatal("accepted invalid user")
	}
	if _, err := store.RescorePerson(ctx, 1, "", settings.ScoreWeights, 1); err == nil {
		t.Fatal("accepted empty person")
	}
	if _, err := store.RecomputeScores(nil, 1, settings.ScoreWeights, 1); err == nil {
		t.Fatal("accepted nil context")
	}
}

func TestRequeueAndReclaimLeasePaths(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC)
	if _, err := (Store{}).Requeue(ctx, nil, now); err == nil {
		t.Fatal("Requeue accepted missing database")
	}
	if _, err := (Store{}).Reclaim(ctx, 3, time.Second, now); err == nil {
		t.Fatal("Reclaim accepted missing database")
	}
	db := &coverageDatabase{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("UPDATE 4"), nil
	}}
	if n, err := (Store{database: db}).Requeue(ctx, []int64{2}, now); err != nil || n != 4 {
		t.Fatalf("requeue=%d err=%v", n, err)
	}
	db.execFn = func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, errors.New("update")
	}
	if _, err := (Store{database: db}).Requeue(ctx, nil, now); err == nil {
		t.Fatal("ignored requeue DB error")
	}
	if _, err := (Store{database: &coverageDatabase{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}}}).Requeue(ctx, nil, now); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name                                                                                  string
		beginErr, queryErr, scanErr, rowsErr, leaseExecErr, outboxErr, selfHealErr, commitErr error
		leases                                                                                [][]any
		retryMax                                                                              int
		want                                                                                  int
		wantErr                                                                               string
	}{
		{name: "no expired rows", retryMax: 3},
		{name: "begin error", beginErr: errors.New("begin"), wantErr: "begin reclaim"},
		{name: "select error", queryErr: errors.New("query"), retryMax: 3, wantErr: "select expired leases"},
		{name: "scan error", scanErr: errors.New("scan"), leases: [][]any{{int64(1), 2, "photo.sizes"}}, retryMax: 3, wantErr: "scan expired lease"},
		{name: "read error", rowsErr: errors.New("read"), retryMax: 3, wantErr: "read expired leases"},
		{name: "retry lease", leases: [][]any{{int64(1), 0, "photo.sizes"}}, retryMax: 3, want: 1},
		{name: "retry update error", leases: [][]any{{int64(1), 0, "photo.sizes"}}, retryMax: 3, leaseExecErr: errors.New("lease update"), wantErr: "return expired lease"},
		{name: "exhausted publishes dead letter", leases: [][]any{{int64(2), 2, "photo.sizes"}}, retryMax: 3, want: 1},
		{name: "dead letter error", leases: [][]any{{int64(2), 2, "photo.sizes"}}, retryMax: 3, outboxErr: errors.New("outbox"), wantErr: "outbox: insert event"},
		{name: "failed lease update error", leases: [][]any{{int64(2), 2, "photo.sizes"}}, retryMax: 3, leaseExecErr: errors.New("failed update"), wantErr: "fail exhausted lease"},
		{name: "self heal error", retryMax: 3, selfHealErr: errors.New("selfheal"), wantErr: "self-heal lease-expired work"},
		{name: "commit error", retryMax: 3, commitErr: errors.New("commit"), wantErr: "commit reclaim"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows := &coverageRows{index: -1, values: tc.leases, scanErr: tc.scanErr, err: tc.rowsErr}
			tx := &coverageTx{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				if tc.queryErr != nil {
					return nil, tc.queryErr
				}
				return rows, nil
			}, execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				switch {
				case strings.Contains(sql, "outbox_events"):
					if tc.outboxErr != nil {
						return pgconn.CommandTag{}, tc.outboxErr
					}
				case strings.Contains(sql, "last_error='lease expired' AND updated_at"):
					if tc.selfHealErr != nil {
						return pgconn.CommandTag{}, tc.selfHealErr
					}
				case strings.Contains(sql, "SET state='failed'"):
					if tc.leaseExecErr != nil {
						return pgconn.CommandTag{}, tc.leaseExecErr
					}
				case strings.Contains(sql, "SET state='pending'"):
					if tc.leaseExecErr != nil {
						return pgconn.CommandTag{}, tc.leaseExecErr
					}
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			}, commitErr: tc.commitErr}
			db := &coverageDatabase{beginFn: func(context.Context) (pgx.Tx, error) {
				if tc.beginErr != nil {
					return nil, tc.beginErr
				}
				return tx, nil
			}}
			got, err := (Store{database: db}).Reclaim(ctx, tc.retryMax, time.Second, now)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error=%v want %q", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("reclaimed=%d want=%d", got, tc.want)
			}
			if tc.beginErr == nil && tx.rollbackCount == 0 {
				t.Fatal("transaction rollback cleanup missing")
			}
		})
	}
	for _, tc := range []struct {
		name               string
		attempts, retryMax int
		base               time.Duration
		wantErr            string
	}{
		{name: "delay doubles", attempts: 1, retryMax: 4, base: time.Second},
		{name: "delay caps", attempts: 8, retryMax: 20, base: time.Hour},
		{name: "outbox insert error", attempts: 3, retryMax: 3, base: time.Second, wantErr: "outbox: insert event"},
		{name: "pending update error", attempts: 1, retryMax: 3, base: time.Second, wantErr: "return expired lease"},
		{name: "failed update error", attempts: 2, retryMax: 3, base: time.Second, wantErr: "fail exhausted lease"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &coverageTx{execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				if tc.name == "outbox insert error" && strings.Contains(sql, "outbox_events") || tc.name == "pending update error" && strings.Contains(sql, "SET state='pending'") || tc.name == "failed update error" && strings.Contains(sql, "SET state='failed'") {
					return pgconn.CommandTag{}, errors.New("expected")
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			}}
			err := applyExpiredLeases(ctx, tx, []expiredLease{{id: 1, attempts: tc.attempts, kind: "photo.sizes"}}, tc.retryMax, tc.base, now)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error=%v want %q", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPumpQueueEmptyRowsAndCreditsBoundaries(t *testing.T) {
	ctx := context.Background()
	settings := coverageSettings()
	if _, err := app.Credits(app.CreditInput{RemainingQuota: -1}); err == nil {
		t.Fatal("Credits accepted negative values")
	}
	if _, err := app.Credits(app.CreditInput{RemainingQuota: 4, PerKeyReadyCap: 8, KeyInflight: 2, NodeCap: 8, NodeInflight: 1, WorkerCap: 3, WorkerInflight: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SplitShare(1, []app.ShareGroup{{ID: 1, Share: 0, Available: 1}}); err == nil {
		t.Fatal("SplitShare accepted zero total share")
	}
	rows := coverageRowsOf([]any{int64(1), 1.0, 0})
	tx := &coverageTx{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) { return rows, nil }}
	if got, err := (Store{}).pumpQueue(ctx, tx, 1, queueCap{name: "media", cap: 2}, 2, settings, &coverageJobs{}); err != nil || got != 0 {
		t.Fatalf("empty group pump=%d err=%v", got, err)
	}
	if _, err := (Store{}).pumpQueuePlan(ctx, &coverageTx{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) { return coverageRowsOf(), nil }}, 1, queueCap{name: "media"}, 1, 2, settings, &coverageJobs{}); err != nil {
		t.Fatal(err)
	}
	if math.IsNaN(float64(settings.MediaReadyCap)) {
		t.Fatal("unreachable")
	}
}
