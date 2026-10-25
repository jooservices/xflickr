package queuepostgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestUserQueue(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*fakeDB)
		wantErr bool
	}{
		{name: "success", setup: func(db *fakeDB) {
			db.queryRows = []fakeRows{{items: [][]any{{"person", int64(4), float64(12)}, {"sizes", int64(1), float64(8)}}}}
		}},
		{name: "invalid dependencies", wantErr: true},
		{name: "query failure", setup: func(db *fakeDB) { db.queryErrors[1] = errors.New("query") }, wantErr: true},
		{name: "scan failure", setup: func(db *fakeDB) {
			db.queryRows = []fakeRows{{scanErr: true, items: [][]any{{"person", int64(1), float64(1)}}}}
		}, wantErr: true},
		{name: "iteration failure", setup: func(db *fakeDB) {
			db.queryRows = []fakeRows{{items: [][]any{{"person", int64(1), float64(1)}}, err: errors.New("rows")}}
		}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "invalid dependencies" {
				if _, err := (*Store)(nil).UserQueue(context.Background(), 1); err == nil {
					t.Fatal("nil store accepted")
				}
				if _, err := New(nil).UserQueue(context.Background(), 1); err == nil {
					t.Fatal("nil database accepted")
				}
				if _, err := (&Store{pool: &fakeDB{}}).UserQueue(nil, 1); err == nil {
					t.Fatal("nil context accepted")
				}
				if _, err := (&Store{pool: &fakeDB{}}).UserQueue(context.Background(), 0); err == nil {
					t.Fatal("invalid user accepted")
				}
				return
			}
			db := newFakeDB()
			tt.setup(db)
			got, err := (&Store{pool: db}).UserQueue(context.Background(), 1)
			if (err != nil) != tt.wantErr {
				t.Fatalf("UserQueue() error=%v", err)
			}
			if err == nil && (len(got) != 2 || got[0].BacklogCalls != 4 || got[1].EffectiveCallsPerHour != 8) {
				t.Fatalf("UserQueue()=%+v", got)
			}
		})
	}
}

func TestAdminQueue(t *testing.T) {
	tests := []struct {
		name    string
		db      *fakeDB
		wantErr bool
	}{
		{name: "success", db: adminDB()},
		{name: "invalid dependencies", wantErr: true},
		{name: "backlog query", db: func() *fakeDB { d := adminDB(); d.queryErrors[1] = errors.New("query"); return d }(), wantErr: true},
		{name: "backlog scan", db: func() *fakeDB { d := adminDB(); d.queryRows[0].scanErr = true; return d }(), wantErr: true},
		{name: "backlog iteration", db: func() *fakeDB { d := adminDB(); d.queryRows[0].err = errors.New("rows"); return d }(), wantErr: true},
		{name: "age query", db: func() *fakeDB { d := adminDB(); d.rowErrors[1] = errors.New("age"); return d }(), wantErr: true},
		{name: "ready query", db: func() *fakeDB { d := adminDB(); d.queryErrors[2] = errors.New("query"); return d }(), wantErr: true},
		{name: "ready scan", db: func() *fakeDB { d := adminDB(); d.queryRows[1].scanErr = true; return d }(), wantErr: true},
		{name: "ready iteration", db: func() *fakeDB { d := adminDB(); d.queryRows[1].err = errors.New("rows"); return d }(), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "invalid dependencies" {
				if _, err := (*Store)(nil).AdminQueue(context.Background()); err == nil {
					t.Fatal("nil store accepted")
				}
				if _, err := New(nil).AdminQueue(context.Background()); err == nil {
					t.Fatal("nil database accepted")
				}
				if _, err := (&Store{pool: &fakeDB{}}).AdminQueue(nil); err == nil {
					t.Fatal("nil context accepted")
				}
				return
			}
			got, err := (&Store{pool: tt.db}).AdminQueue(context.Background())
			if (err != nil) != tt.wantErr {
				t.Fatalf("AdminQueue() error=%v", err)
			}
			if err == nil && (len(got.BacklogByPlan) != 1 || got.OldestItemAgeSeconds != 11 || len(got.ReadyQueueSizes) != 1) {
				t.Fatalf("AdminQueue()=%+v", got)
			}
		})
	}
}

func TestPublishQueueState(t *testing.T) {
	tests := []struct {
		name    string
		db      *fakeDB
		wantErr bool
	}{
		{name: "success", db: publishDB()},
		{name: "invalid dependencies", wantErr: true},
		{name: "lane query", db: func() *fakeDB { d := publishDB(); d.queryErrors[1] = errors.New("query"); return d }(), wantErr: true},
		{name: "lane scan", db: func() *fakeDB { d := publishDB(); d.queryRows[0].scanErr = true; return d }(), wantErr: true},
		{name: "lane iteration", db: func() *fakeDB { d := publishDB(); d.queryRows[0].err = errors.New("rows"); return d }(), wantErr: true},
		{name: "ready query", db: func() *fakeDB { d := publishDB(); d.rowErrors[1] = errors.New("ready"); return d }(), wantErr: true},
		{name: "begin", db: func() *fakeDB { d := publishDB(); d.beginErr = errors.New("begin"); return d }(), wantErr: true},
		{name: "insert", db: func() *fakeDB { d := publishDB(); d.tx.execErr = errors.New("insert"); return d }(), wantErr: true},
		{name: "commit", db: func() *fakeDB { d := publishDB(); d.tx.commitErr = errors.New("commit"); return d }(), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "invalid dependencies" {
				if err := (*Store)(nil).PublishQueueState(context.Background()); err == nil {
					t.Fatal("nil store accepted")
				}
				if err := New(nil).PublishQueueState(context.Background()); err == nil {
					t.Fatal("nil database accepted")
				}
				if err := (&Store{pool: &fakeDB{}}).PublishQueueState(nil); err == nil {
					t.Fatal("nil context accepted")
				}
				return
			}
			err := (&Store{pool: tt.db}).PublishQueueState(context.Background())
			if (err != nil) != tt.wantErr {
				t.Fatalf("PublishQueueState() error=%v", err)
			}
			if err == nil && tt.db.tx.execCalls != 1 {
				t.Fatalf("outbox inserts=%d", tt.db.tx.execCalls)
			}
		})
	}
}

type fakeDB struct {
	queryCalls  int
	queryRows   []fakeRows
	queryErrors map[int]error
	rowCalls    int
	rowErrors   map[int]error
	beginErr    error
	tx          *fakeTx
}

func newFakeDB() *fakeDB {
	return &fakeDB{queryErrors: map[int]error{}, rowErrors: map[int]error{}, tx: &fakeTx{}}
}

func adminDB() *fakeDB {
	d := newFakeDB()
	d.queryRows = []fakeRows{{items: [][]any{{"pro", "person", int64(2)}}}, {items: [][]any{{"sync", int64(4)}}}}
	return d
}

func publishDB() *fakeDB {
	d := newFakeDB()
	d.queryRows = []fakeRows{{items: [][]any{{"person", int64(2)}}}}
	return d
}

func (d *fakeDB) Query(_ context.Context, _ string, _ ...any) (rows, error) {
	d.queryCalls++
	if err := d.queryErrors[d.queryCalls]; err != nil {
		return nil, err
	}
	if d.queryCalls > len(d.queryRows) {
		return &fakeRows{}, nil
	}
	return &d.queryRows[d.queryCalls-1], nil
}

func (d *fakeDB) QueryRow(context.Context, string, ...any) row {
	d.rowCalls++
	return fakeRow{value: int64(11), err: d.rowErrors[d.rowCalls]}
}

func (d *fakeDB) Begin(context.Context) (queueTx, error) {
	if d.beginErr != nil {
		return nil, d.beginErr
	}
	return d.tx, nil
}

type fakeRow struct {
	value any
	err   error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	switch target := dest[0].(type) {
	case *int64:
		*target = r.value.(int64)
	default:
		return fmt.Errorf("unsupported destination %T", dest[0])
	}
	return nil
}

type fakeRows struct {
	items   [][]any
	at      int
	scanErr bool
	err     error
}

func (r *fakeRows) Next() bool { return r.at < len(r.items) }
func (r *fakeRows) Scan(dest ...any) error {
	if r.scanErr {
		return errors.New("scan")
	}
	values := r.items[r.at]
	for i, target := range dest {
		switch ptr := target.(type) {
		case *string:
			*ptr = values[i].(string)
		case *int64:
			*ptr = values[i].(int64)
		case *float64:
			*ptr = values[i].(float64)
		default:
			return fmt.Errorf("unsupported destination %T", target)
		}
	}
	r.at++
	return nil
}
func (r *fakeRows) Err() error { return r.err }
func (*fakeRows) Close()       {}

type fakeTx struct {
	execCalls          int
	execErr, commitErr error
}

func (tx *fakeTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	tx.execCalls++
	return pgconn.CommandTag{}, tx.execErr
}
func (tx *fakeTx) Commit(context.Context) error { return tx.commitErr }
func (*fakeTx) Rollback(context.Context) error  { return nil }
