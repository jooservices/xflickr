package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jooservices/xflickr/internal/quota/app"
)

type fakePool struct {
	row         pgx.Row
	rows        pgx.Rows
	execErr     error
	copyErr     error
	qErr        error
	args        []any
	copyTable   pgx.Identifier
	copyColumns []string
	copyData    [][]any
}

func (p *fakePool) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	p.args = args
	return pgconn.CommandTag{}, p.execErr
}

func (p *fakePool) Query(context.Context, string, ...any) (pgx.Rows, error) { return p.rows, p.qErr }
func (p *fakePool) QueryRow(context.Context, string, ...any) pgx.Row        { return p.row }
func (p *fakePool) CopyFrom(_ context.Context, table pgx.Identifier, columns []string, source pgx.CopyFromSource) (int64, error) {
	p.copyTable = table
	p.copyColumns = append([]string(nil), columns...)
	for source.Next() {
		values, err := source.Values()
		if err != nil {
			return int64(len(p.copyData)), err
		}
		p.copyData = append(p.copyData, values)
	}
	if err := source.Err(); err != nil {
		return int64(len(p.copyData)), err
	}
	return int64(len(p.copyData)), p.copyErr
}

type fakeRows struct {
	pgx.Rows
	data [][]any
	i    int
	err  error
}

func (r *fakeRows) Close()     {}
func (r *fakeRows) Err() error { return r.err }
func (r *fakeRows) Next() bool { r.i++; return r.i <= len(r.data) }
func (r *fakeRows) Scan(dest ...any) error {
	return valRow{values: r.data[r.i-1]}.Scan(dest...)
}

type valRow struct {
	values []any
	err    error
}

func (r valRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, value := range r.values {
		switch target := dest[i].(type) {
		case *int64:
			v, ok := value.(int64)
			if !ok {
				return errors.New("type mismatch")
			}
			*target = v
		case *string:
			v, ok := value.(string)
			if !ok {
				return errors.New("type mismatch")
			}
			*target = v
		case *bool:
			v, ok := value.(bool)
			if !ok {
				return errors.New("type mismatch")
			}
			*target = v
		case *time.Time:
			v, ok := value.(time.Time)
			if !ok {
				return errors.New("type mismatch")
			}
			*target = v
		case *[]byte:
			*target = append((*target)[:0], value.([]byte)...)
		default:
			return errors.New("unexpected scan target")
		}
	}
	return nil
}

var at = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

func TestSaveValidatesAndWrites(t *testing.T) {
	pool := &fakePool{}
	u := Usage{Pool: pool}
	if err := u.Save(context.Background(), app.Usage{KeyID: 1, WindowStart: at, Used: 3, ByClass: map[string]int64{"person": 3}}); err != nil {
		t.Fatal(err)
	}
	if string(pool.args[3].([]byte)) != `{"person":3}` {
		t.Fatalf("args = %v", pool.args)
	}
	if err := u.Save(context.Background(), app.Usage{KeyID: 1, WindowStart: at}); err != nil || string(pool.args[3].([]byte)) != "{}" {
		t.Fatalf("nil by_class = %v %v", err, pool.args)
	}
	for _, bad := range []app.Usage{{WindowStart: at}, {KeyID: 1}, {KeyID: 1, WindowStart: at, Used: -1}} {
		if err := u.Save(context.Background(), bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if err := (Usage{}).Save(context.Background(), app.Usage{KeyID: 1, WindowStart: at}); err == nil {
		t.Fatal("nil pool accepted")
	}
	pool.execErr = errors.New("down")
	if err := u.Save(context.Background(), app.Usage{KeyID: 1, WindowStart: at}); err == nil {
		t.Fatal("exec error ignored")
	}
}

func TestLatest(t *testing.T) {
	pool := &fakePool{row: valRow{values: []any{at, int64(4), []byte(`{"photo":4}`)}}}
	got, ok, err := (Usage{Pool: pool}).Latest(context.Background(), 2, at.Add(-time.Hour))
	if err != nil || !ok || got.KeyID != 2 || got.Used != 4 || got.ByClass["photo"] != 4 || !got.WindowStart.Equal(at) {
		t.Fatalf("Latest = %+v %v %v", got, ok, err)
	}
	pool.row = valRow{err: pgx.ErrNoRows}
	if _, ok, err := (Usage{Pool: pool}).Latest(context.Background(), 2, at); ok || err != nil {
		t.Fatalf("no rows = %v %v", ok, err)
	}
	pool.row = valRow{err: errors.New("down")}
	if _, _, err := (Usage{Pool: pool}).Latest(context.Background(), 2, at); err == nil {
		t.Fatal("query error ignored")
	}
	pool.row = valRow{values: []any{at, int64(4), []byte(`bad`)}}
	if _, _, err := (Usage{Pool: pool}).Latest(context.Background(), 2, at); err == nil {
		t.Fatal("bad json accepted")
	}
	if _, _, err := (Usage{}).Latest(context.Background(), 2, at); err == nil {
		t.Fatal("nil pool accepted")
	}
}

func TestKeyIDs(t *testing.T) {
	pool := &fakePool{rows: &fakeRows{data: [][]any{{int64(1)}, {int64(5)}}}}
	ids, err := (Usage{Pool: pool}).KeyIDs(context.Background())
	if err != nil || len(ids) != 2 || ids[1] != 5 {
		t.Fatalf("KeyIDs = %v %v", ids, err)
	}
	pool.qErr = errors.New("down")
	if _, err := (Usage{Pool: pool}).KeyIDs(context.Background()); err == nil {
		t.Fatal("query error ignored")
	}
	pool.qErr = nil
	pool.rows = &fakeRows{data: [][]any{{"x"}}}
	if _, err := (Usage{Pool: pool}).KeyIDs(context.Background()); err == nil {
		t.Fatal("scan error ignored")
	}
	if _, err := (Usage{}).KeyIDs(context.Background()); err == nil {
		t.Fatal("nil pool accepted")
	}
}

func TestCallLog(t *testing.T) {
	early := at.Add(-30 * time.Minute)
	pool := &fakePool{rows: &fakeRows{data: [][]any{
		{"person", int64(3), at},
		{"photo", int64(2), early},
	}}}
	got, err := (CallLog{Pool: pool}).Usage(context.Background(), 1, at.Add(-time.Hour))
	if err != nil || got.ByClass["person"] != 3 || got.ByClass["photo"] != 2 || !got.First.Equal(early) {
		t.Fatalf("Usage = %+v %v", got, err)
	}
	pool.qErr = errors.New("down")
	if _, err := (CallLog{Pool: pool}).Usage(context.Background(), 1, at); err == nil {
		t.Fatal("query error ignored")
	}
	pool.qErr = nil
	pool.rows = &fakeRows{data: [][]any{{"person", "x", at}}}
	if _, err := (CallLog{Pool: pool}).Usage(context.Background(), 1, at); err == nil {
		t.Fatal("scan error ignored")
	}
	if _, err := (CallLog{}).Usage(context.Background(), 1, at); err == nil {
		t.Fatal("nil pool accepted")
	}
}

func TestCallLogRecordAndPrune(t *testing.T) {
	pool := &fakePool{}
	if err := (CallLog{Pool: pool}).Record(context.Background(), 4, "person", at); err != nil || pool.args[0] != int64(4) || pool.args[1] != "person" {
		t.Fatalf("Record = %v args %v", err, pool.args)
	}
	if err := (CallLog{Pool: pool}).Prune(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	pool.execErr = errors.New("down")
	if err := (CallLog{Pool: pool}).Record(context.Background(), 4, "person", at); err == nil {
		t.Fatal("record error ignored")
	}
	if err := (CallLog{Pool: pool}).Prune(context.Background(), at); err == nil {
		t.Fatal("prune error ignored")
	}
	if (CallLog{}).Record(context.Background(), 4, "person", at) == nil || (CallLog{Pool: pool}).Record(context.Background(), 0, "person", at) == nil || (CallLog{Pool: pool}).Record(context.Background(), 1, "", at) == nil {
		t.Fatal("invalid record accepted")
	}
	if (CallLog{}).Prune(context.Background(), at) == nil {
		t.Fatal("nil pool accepted")
	}
}

func TestCallLogInsertCalls(t *testing.T) {
	userID, accountID, errorCode := int64(2), int64(3), int64(105)
	pool := &fakePool{}
	record := app.CallRecord{
		At: at, KeyID: 4, UserID: &userID, AccountID: &accountID, Node: "node-1",
		Method: "flickr.people.getInfo", Status: "fail", ErrorCode: &errorCode,
		LatencyMS: 75, Bytes: 42, Lane: "person",
	}
	if err := (CallLog{Pool: pool}).InsertCalls(context.Background(), []app.CallRecord{record}); err != nil {
		t.Fatal(err)
	}
	if len(pool.copyData) != 1 || len(pool.copyColumns) != 12 || len(pool.copyTable) != 1 || pool.copyTable[0] != "api_call_log" {
		t.Fatalf("copy target=%v columns=%v rows=%v", pool.copyTable, pool.copyColumns, pool.copyData)
	}
	row := pool.copyData[0]
	if !row[0].(time.Time).Equal(at) || !row[1].(time.Time).Equal(at) || row[2] != int64(4) || row[3] != userID || row[4] != accountID || row[5] != "node-1" || row[6] != record.Method || row[7] != "fail" || row[8] != errorCode || row[9] != int64(75) || row[10] != int64(42) || row[11] != "person" {
		t.Fatalf("copied row = %#v", row)
	}
	if err := (CallLog{Pool: pool}).InsertCalls(context.Background(), nil); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
	for _, invalid := range []app.CallRecord{{KeyID: 4, Method: "m", Status: "ok"}, {At: at, Method: "m", Status: "ok"}} {
		if err := (CallLog{Pool: pool}).InsertCalls(context.Background(), []app.CallRecord{invalid}); err == nil {
			t.Fatalf("accepted invalid record: %+v", invalid)
		}
	}
	pool.copyErr = errors.New("copy failed")
	if err := (CallLog{Pool: pool}).InsertCalls(context.Background(), []app.CallRecord{record}); err == nil {
		t.Fatal("copy error ignored")
	}
	if err := (CallLog{}).InsertCalls(context.Background(), []app.CallRecord{record}); err == nil {
		t.Fatal("nil pool accepted")
	}
}
