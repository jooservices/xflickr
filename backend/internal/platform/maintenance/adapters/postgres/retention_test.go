package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/jooservices/xflickr/internal/platform/maintenance/domain"
)

type fakeLockConn struct {
	locked   bool
	queryErr error
	execErr  error
	released bool
}

func (c *fakeLockConn) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakeRow{locked: c.locked, err: c.queryErr}
}

func (c *fakeLockConn) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, c.execErr
}

func (c *fakeLockConn) Release() { c.released = true }

type fakeRow struct {
	locked bool
	err    error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) == 1 {
		if ptr, ok := dest[0].(*bool); ok {
			*ptr = r.locked
		}
	}
	return nil
}

type fakeLockPool struct {
	conn *fakeLockConn
	err  error
}

func (p fakeLockPool) Acquire(context.Context) (LockConn, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.conn, nil
}

type fakeExecPool struct {
	execs []string
	err   error
	rows  []string
}

func (p *fakeExecPool) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	p.execs = append(p.execs, sql)
	return pgconn.CommandTag{}, p.err
}

func (p *fakeExecPool) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if p.err != nil {
		return nil, p.err
	}
	data := make([][]any, 0, len(p.rows))
	for _, name := range p.rows {
		data = append(data, []any{name})
	}
	return &fakeRows{data: data}, nil
}

type fakeRows struct {
	data [][]any
	i    int
	err  error
}

func (r *fakeRows) Close()                                       {}
func (r *fakeRows) Err() error                                   { return r.err }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) Next() bool {
	if r.i >= len(r.data) {
		return false
	}
	r.i++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	row := r.data[r.i-1]
	for i := range dest {
		if ptr, ok := dest[i].(*string); ok {
			*ptr = row[i].(string)
		}
	}
	return nil
}
func (r *fakeRows) Values() ([]any, error) { return nil, nil }
func (r *fakeRows) RawValues() [][]byte    { return nil }
func (r *fakeRows) Conn() *pgx.Conn        { return nil }
func (r *fakeRows) TypeMap() *pgtype.Map   { return nil }

func TestTryLockBusyAndUnlock(t *testing.T) {
	_ = NewStore(nil)
	conn := &fakeLockConn{locked: true}
	unlock, ok, err := (Store{Locks: fakeLockPool{conn: conn}, Key: 99}).TryLock(context.Background())
	if err != nil || !ok {
		t.Fatalf("acquired=%t err=%v", ok, err)
	}
	unlock()
	if !conn.released {
		t.Fatal("connection was not released")
	}

	busy := &fakeLockConn{locked: false}
	_, ok, err = (Store{Locks: fakeLockPool{conn: busy}}).TryLock(context.Background())
	if err != nil || ok {
		t.Fatalf("busy acquired=%t err=%v", ok, err)
	}
	if !busy.released {
		t.Fatal("busy connection was not released")
	}
	if _, _, err := (Store{}).TryLock(context.Background()); err == nil {
		t.Fatal("nil locks accepted")
	}
	boom := errors.New("acquire failed")
	if _, _, err := (Store{Locks: fakeLockPool{err: boom}}).TryLock(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("acquire err = %v", err)
	}
	lockFail := &fakeLockConn{queryErr: boom}
	if _, _, err := (Store{Locks: fakeLockPool{conn: lockFail}}).TryLock(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("lock query err = %v", err)
	}
	if !lockFail.released {
		t.Fatal("failed lock connection was not released")
	}
	pl := poolLock{}
	if _, err := pl.Acquire(context.Background()); err == nil {
		t.Fatal("nil pool lock accepted")
	}
}

func TestListDetachDropAndRollup(t *testing.T) {
	pool := &fakeExecPool{rows: []string{"api_call_log_20261001"}}
	store := Store{Pool: pool}
	parts, err := store.ListDailyPartitions(context.Background())
	if err != nil || len(parts) != 1 || parts[0].Name != "api_call_log_20261001" {
		t.Fatalf("parts=%v err=%v", parts, err)
	}
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if err := store.RollupDay(context.Background(), day); err != nil {
		t.Fatal(err)
	}
	if err := store.DetachConcurrently(context.Background(), "api_call_log_20261001"); err != nil {
		t.Fatal(err)
	}
	if err := store.DropTable(context.Background(), "api_call_log_20261001"); err != nil {
		t.Fatal(err)
	}
	if len(pool.execs) < 3 {
		t.Fatalf("execs=%v", pool.execs)
	}
	if err := store.DetachConcurrently(context.Background(), "bad"); err == nil {
		t.Fatal("bad detach name accepted")
	}
	if err := store.DropTable(context.Background(), "bad"); err == nil {
		t.Fatal("bad drop name accepted")
	}
	if _, err := (Store{}).ListDailyPartitions(context.Background()); err == nil {
		t.Fatal("nil pool list accepted")
	}
	if err := (Store{}).RollupDay(context.Background(), day); err == nil {
		t.Fatal("nil pool rollup accepted")
	}
	if err := (Store{}).DetachConcurrently(context.Background(), "api_call_log_20261001"); err == nil {
		t.Fatal("nil pool detach accepted")
	}
	if err := (Store{}).DropTable(context.Background(), "api_call_log_20261001"); err == nil {
		t.Fatal("nil pool drop accepted")
	}
	boom := errors.New("db down")
	down := Store{Pool: &fakeExecPool{err: boom}}
	if _, err := down.ListDailyPartitions(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("list err = %v", err)
	}
	if err := down.RollupDay(context.Background(), day); !errors.Is(err, boom) {
		t.Fatalf("rollup err = %v", err)
	}
	if err := down.DetachConcurrently(context.Background(), "api_call_log_20261001"); !errors.Is(err, boom) {
		t.Fatalf("detach err = %v", err)
	}
	if err := down.DropTable(context.Background(), "api_call_log_20261001"); !errors.Is(err, boom) {
		t.Fatalf("drop err = %v", err)
	}
}

func TestLoadRetentionSettings(t *testing.T) {
	days, dryRun := DefaultRetentionSettings()
	if days != domain.DefaultRetentionDays || dryRun {
		t.Fatalf("defaults = %d %t", days, dryRun)
	}
	settings := RetentionSettings{Querier: &settingsQuerier{values: map[string]string{
		"maintenance.api_call_log_retention_days": "9",
		"maintenance.api_call_log_dry_run":        "true",
	}}}
	gotDays, err := settings.RetentionDays(context.Background())
	if err != nil || gotDays != 9 {
		t.Fatalf("days=%d err=%v", gotDays, err)
	}
	gotDry, err := settings.DryRun(context.Background())
	if err != nil || !gotDry {
		t.Fatalf("dryRun=%t err=%v", gotDry, err)
	}
	clamped := RetentionSettings{Querier: &settingsQuerier{values: map[string]string{
		"maintenance.api_call_log_retention_days": "3",
	}}}
	gotDays, err = clamped.RetentionDays(context.Background())
	if err != nil || gotDays != domain.MinRetentionDays {
		t.Fatalf("clamped=%d err=%v", gotDays, err)
	}
	if _, err := (RetentionSettings{}).RetentionDays(context.Background()); err == nil {
		t.Fatal("nil querier accepted")
	}
	if _, err := (RetentionSettings{Querier: &settingsQuerier{err: errors.New("down")}}).RetentionDays(context.Background()); err == nil {
		t.Fatal("query error ignored")
	}
	if _, err := (RetentionSettings{Querier: &settingsQuerier{values: map[string]string{
		"maintenance.api_call_log_retention_days": "nope",
	}}}).RetentionDays(context.Background()); err == nil {
		t.Fatal("bad days accepted")
	}
	if _, err := (RetentionSettings{Querier: &settingsQuerier{values: map[string]string{
		"maintenance.api_call_log_dry_run": "nope",
	}}}).DryRun(context.Background()); err == nil {
		t.Fatal("bad dry-run accepted")
	}
}

type settingsQuerier struct {
	values map[string]string
	err    error
}

func (q *settingsQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if q.err != nil {
		return nil, q.err
	}
	data := make([][]any, 0, len(q.values))
	for key, value := range q.values {
		data = append(data, []any{key, value})
	}
	return &fakeRows{data: data}, nil
}
