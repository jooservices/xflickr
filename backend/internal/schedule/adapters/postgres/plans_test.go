package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/audit"
	"github.com/jooservices/xflickr/internal/schedule/app"
	"github.com/jooservices/xflickr/internal/schedule/domain"
)

type fakeRow struct {
	values []any
	err    error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *int64:
			*d = r.values[i].(int64)
		case *int:
			*d = r.values[i].(int)
		case *string:
			*d = r.values[i].(string)
		case *float64:
			*d = r.values[i].(float64)
		case *bool:
			*d = r.values[i].(bool)
		default:
			return errors.New("unsupported scan target")
		}
	}
	return nil
}

type fakeRows struct {
	values           [][]any
	index            int
	started          bool
	scanErr, rowsErr error
	closed           bool
}

func (r *fakeRows) Next() bool {
	if !r.started {
		r.started = true
		return len(r.values) > 0
	}
	r.index++
	return r.index < len(r.values)
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	return (fakeRow{values: r.values[r.index]}).Scan(dest...)
}
func (r *fakeRows) Err() error { return r.rowsErr }
func (r *fakeRows) Close()     { r.closed = true }

type fakeTx struct {
	execErr, commitErr            error
	rollbackErr                   error
	execCount, commits, rollbacks int
	queryRow                      row
	execTag                       pgconn.CommandTag
}

func (tx *fakeTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	tx.execCount++
	if tx.execTag.String() != "" {
		return tx.execTag, tx.execErr
	}
	return pgconn.NewCommandTag("INSERT 0 1"), tx.execErr
}

func (tx *fakeTx) QueryRow(context.Context, string, ...any) row {
	if tx.queryRow != nil {
		return tx.queryRow
	}
	return fakeRow{values: []any{true}}
}
func (tx *fakeTx) Commit(context.Context) error   { tx.commits++; return tx.commitErr }
func (tx *fakeTx) Rollback(context.Context) error { tx.rollbacks++; return tx.rollbackErr }

type fakePool struct {
	beginErr  error
	tx        *fakeTx
	queryRows rows
	queryErr  error
	queryRow  row
	execTag   pgconn.CommandTag
	execErr   error
}

func (p *fakePool) Begin(context.Context) (transaction, error) {
	if p.beginErr != nil {
		return nil, p.beginErr
	}
	if p.tx == nil {
		p.tx = &fakeTx{}
	}
	return p.tx, nil
}

func (p *fakePool) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return p.execTag, p.execErr
}

func (p *fakePool) Query(context.Context, string, ...any) (rows, error) {
	return p.queryRows, p.queryErr
}
func (p *fakePool) QueryRow(context.Context, string, ...any) row { return p.queryRow }

type fakePGXPool struct {
	err    error
	result pgx.Row
}

func (p fakePGXPool) Begin(context.Context) (pgx.Tx, error) { return nil, p.err }
func (p fakePGXPool) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, p.err
}
func (p fakePGXPool) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, p.err }
func (p fakePGXPool) QueryRow(context.Context, string, ...any) pgx.Row        { return p.result }

type passthroughRow struct{}

func (passthroughRow) Scan(...any) error { return nil }

func TestPoolAdapterAndConstructor(t *testing.T) {
	if New(&pgxpool.Pool{}) == nil {
		t.Fatal("New returned nil")
	}
	errExpected := errors.New("db")
	adapter := poolAdapter{pool: fakePGXPool{err: errExpected, result: passthroughRow{}}}
	if _, err := adapter.Begin(context.Background()); !errors.Is(err, errExpected) {
		t.Fatalf("Begin error=%v", err)
	}
	if _, err := adapter.Exec(context.Background(), "query"); !errors.Is(err, errExpected) {
		t.Fatalf("Exec error=%v", err)
	}
	if _, err := adapter.Query(context.Background(), "query"); !errors.Is(err, errExpected) {
		t.Fatalf("Query error=%v", err)
	}
	if got := adapter.QueryRow(context.Background(), "query"); got == nil {
		t.Fatal("QueryRow returned nil")
	}
}

func TestScanPlan(t *testing.T) {
	values := []any{int64(2), "pro", 4, 3, 1000, 100, 72, float64(.8)}
	got, err := scanPlan(fakeRow{values: values})
	want := domain.Plan{ID: 2, Name: "pro", Weight: 4, MaxDepthCap: 3, MaxContacts: 1000, AIQuotaPerDay: 100, ZipTTLHours: 72, ResourceShare: .8}
	if err != nil || got != want {
		t.Fatalf("scanPlan=%+v %v; want %+v", got, err, want)
	}
	if _, err := scanPlan(fakeRow{err: errors.New("scan failed")}); err == nil {
		t.Fatal("scanPlan swallowed error")
	}
}

func TestListPlans(t *testing.T) {
	p := &fakePool{queryRows: &fakeRows{values: [][]any{{int64(1), "free", 1, 1, 100, 0, 24, float64(.2)}}}}
	store := &Store{pool: p}
	got, err := store.ListPlans(context.Background())
	if err != nil || len(got) != 1 || got[0].Name != "free" {
		t.Fatalf("ListPlans=%+v %v", got, err)
	}
	if !p.queryRows.(*fakeRows).closed {
		t.Fatal("ListPlans did not close rows")
	}
	p = &fakePool{queryErr: errors.New("query")}
	if _, err := (&Store{pool: p}).ListPlans(context.Background()); err == nil {
		t.Fatal("ListPlans swallowed query error")
	}
	p = &fakePool{queryRows: &fakeRows{values: [][]any{{}}, scanErr: errors.New("scan")}}
	if _, err := (&Store{pool: p}).ListPlans(context.Background()); err == nil {
		t.Fatal("ListPlans swallowed row scan error")
	}
	p = &fakePool{queryRows: &fakeRows{rowsErr: errors.New("rows")}}
	if _, err := (&Store{pool: p}).ListPlans(context.Background()); err == nil {
		t.Fatal("ListPlans swallowed rows error")
	}
}

func TestSavePlans(t *testing.T) {
	plan := domain.Plan{Name: "free", Weight: 1, MaxDepthCap: 1, MaxContacts: 100, ZipTTLHours: 24, ResourceShare: .2}
	event := audit.Event{Actor: "user:7", Action: "plans.update", Target: "plans", Detail: map[string]any{"plans": []domain.Plan{plan}}}
	tx := &fakeTx{}
	if err := (&Store{pool: &fakePool{tx: tx}}).SavePlans(context.Background(), []domain.Plan{plan}, event); err != nil {
		t.Fatal(err)
	}
	if tx.execCount != 2 || tx.commits != 1 || tx.rollbacks != 1 {
		t.Fatalf("transaction calls=%+v", tx)
	}
	tx = &fakeTx{}
	if err := (&Store{pool: &fakePool{tx: tx}}).SavePlans(context.Background(), nil, event); err != nil {
		t.Fatal(err)
	}
	if tx.execCount != 1 || tx.commits != 1 {
		t.Fatalf("empty catalog transaction=%+v", tx)
	}
	if err := (&Store{pool: &fakePool{beginErr: errors.New("begin")}}).SavePlans(context.Background(), nil, event); err == nil {
		t.Fatal("SavePlans swallowed begin error")
	}
	tx = &fakeTx{execErr: errors.New("exec")}
	if err := (&Store{pool: &fakePool{tx: tx}}).SavePlans(context.Background(), []domain.Plan{plan}, event); err == nil {
		t.Fatal("SavePlans swallowed exec error")
	}
	if tx.rollbacks != 1 {
		t.Fatalf("failed save did not roll back: %+v", tx)
	}
	tx = &fakeTx{commitErr: errors.New("commit")}
	if err := (&Store{pool: &fakePool{tx: tx}}).SavePlans(context.Background(), nil, event); err == nil {
		t.Fatal("SavePlans swallowed commit error")
	}
}

func TestPlanForUser(t *testing.T) {
	values := []any{int64(1), "free", 1, 1, 100, 0, 24, float64(.2)}
	store := &Store{pool: &fakePool{queryRow: fakeRow{values: values}}}
	if got, err := store.PlanForUser(context.Background(), 8); err != nil || got.Name != "free" {
		t.Fatalf("PlanForUser=%+v %v", got, err)
	}
	store.pool = &fakePool{queryRow: fakeRow{err: errors.New("query")}}
	if _, err := store.PlanForUser(context.Background(), 8); err == nil {
		t.Fatal("PlanForUser swallowed query error")
	}
	store.pool = &fakePool{queryRow: fakeRow{err: pgxNoRows}}
	if _, err := store.PlanForUser(context.Background(), 8); !errors.Is(err, app.ErrUserNotFound) {
		t.Fatalf("missing user error=%v", err)
	}
}

func TestAssignUserPlan(t *testing.T) {
	event := audit.Event{Actor: "user:1", Action: "user.plan.assign", Target: "user:7", Detail: map[string]any{"plan_id": int64(2)}}
	tx := &fakeTx{}
	pool := &fakePool{tx: tx}
	if err := (&Store{pool: pool}).AssignUserPlan(context.Background(), 7, 2, event); err != nil {
		t.Fatal(err)
	}
	if tx.execCount != 2 || tx.commits != 1 {
		t.Fatalf("assignment transaction=%+v", tx)
	}
	tx.queryRow = fakeRow{values: []any{false}}
	if err := (&Store{pool: pool}).AssignUserPlan(context.Background(), 7, 2, event); !errors.Is(err, app.ErrPlanNotFound) {
		t.Fatalf("missing plan error=%v", err)
	}
	tx.queryRow = fakeRow{err: errors.New("query")}
	if err := (&Store{pool: pool}).AssignUserPlan(context.Background(), 7, 2, event); err == nil {
		t.Fatal("AssignUserPlan swallowed query error")
	}
	tx.queryRow = fakeRow{values: []any{true}}
	tx.execErr = errors.New("update")
	if err := (&Store{pool: pool}).AssignUserPlan(context.Background(), 7, 2, event); err == nil {
		t.Fatal("AssignUserPlan swallowed update error")
	}
	tx.execErr = nil
	tx.execTag = pgconn.NewCommandTag("UPDATE 0")
	if err := (&Store{pool: pool}).AssignUserPlan(context.Background(), 7, 2, event); !errors.Is(err, app.ErrUserNotFound) {
		t.Fatalf("missing user error=%v", err)
	}
}

var pgxNoRows = pgx.ErrNoRows
