package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/crawl/app"
	"github.com/jooservices/xflickr/internal/crawl/domain"
)

type engExec struct {
	sql  string
	args []any
}

type engRow struct {
	values []any
	err    error
}

func (r engRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, d := range dest {
		switch p := d.(type) {
		case *int64:
			*p = r.values[i].(int64)
		case *int:
			*p = r.values[i].(int)
		case *string:
			*p = r.values[i].(string)
		case *bool:
			*p = r.values[i].(bool)
		case *[]byte:
			*p = r.values[i].([]byte)
		}
	}
	return nil
}

type engConn struct {
	execs    []engExec
	queries  []engExec
	failExec string
	affected int64
	row      engRow
	rows     []engRow
	rowErr   error
}

func (c *engConn) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	c.execs = append(c.execs, engExec{sql, args})
	if c.failExec != "" && strings.Contains(sql, c.failExec) {
		return pgconn.CommandTag{}, errors.New("exec failed")
	}
	return pgconn.NewCommandTag("UPDATE " + string(rune('0'+c.affected))), nil
}

func (c *engConn) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unused")
}

func (c *engConn) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	c.queries = append(c.queries, engExec{sql, args})
	if c.rowErr != nil {
		return engRow{err: c.rowErr}
	}
	if c.failExec != "" && strings.Contains(sql, c.failExec) {
		return engRow{err: errors.New("query failed")}
	}
	if len(c.rows) > 0 {
		next := c.rows[0]
		c.rows = c.rows[1:]
		return next
	}
	return c.row
}

func (c *engConn) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}

type engCatalog struct{ app.CatalogWriter }

func newTestEngine(conn *engConn) *Engine {
	return &Engine{
		conn: conn,
		run:  func(_ context.Context, fn func(Conn) error) error { return fn(conn) },
		catalog: func(Conn) app.CatalogWriter {
			return engCatalog{}
		},
	}
}

func TestLoadParsesCursorAndMapsMissingRows(t *testing.T) {
	conn := &engConn{row: engRow{values: []any{int64(5), "photos", "1@N01", int64(0), 2, "ready", 1, []byte(`{"page":3,"method":"m"}`)}}}
	item, err := newTestEngine(conn).Load(t.Context(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if item.ID != 5 || item.Kind != "photos" || item.Depth != 2 || item.Cursor.Page != 3 || item.Cursor.Method != "m" {
		t.Fatalf("item = %+v", item)
	}
	conn.row = engRow{err: pgx.ErrNoRows}
	if _, err := newTestEngine(conn).Load(t.Context(), 5); !errors.Is(err, app.ErrItemNotFound) {
		t.Fatalf("err = %v", err)
	}
	conn.row = engRow{err: errors.New("db")}
	if _, err := newTestEngine(conn).Load(t.Context(), 5); err == nil || errors.Is(err, app.ErrItemNotFound) {
		t.Fatalf("err = %v", err)
	}
	conn.row = engRow{values: []any{int64(5), "photos", "1@N01", int64(0), 2, "ready", 1, []byte(`{}`)}}
	if _, err := newTestEngine(conn).Load(t.Context(), 5); !errors.Is(err, domain.ErrInvalidCursor) {
		t.Fatalf("err = %v", err)
	}
}

func TestStampScanStart(t *testing.T) {
	conn := &engConn{}
	e := newTestEngine(conn)
	expected := domain.Cursor{Page: 3, Continuation: "legacy", Method: "flickr.people.getPhotos", Params: map[string]string{"user_id": "person"}}
	if err := e.StampScanStart(t.Context(), 9, 0, expected); err != nil {
		t.Fatal(err)
	}
	if len(conn.execs) != 0 {
		t.Fatal("zero checkpoint must not write")
	}
	if err := e.StampScanStart(t.Context(), 9, 1700000000, expected); err != nil {
		t.Fatal(err)
	}
	if len(conn.execs) != 1 || !strings.Contains(conn.execs[0].sql, "checkpoint_at") ||
		!strings.Contains(conn.execs[0].sql, "continuation") || conn.execs[0].args[1] != int64(1700000000) ||
		conn.execs[0].args[2] != expected.Method || conn.execs[0].args[3] != expected.Page {
		t.Fatalf("stamp exec = %#v", conn.execs)
	}
	conn.failExec = "checkpoint_at"
	if err := e.StampScanStart(t.Context(), 9, 1, expected); err == nil {
		t.Fatal("stamp error must surface")
	}
}

func TestWorkFinishStoresNextCheckpoint(t *testing.T) {
	conn := &engConn{rows: []engRow{
		{values: []any{"photos", "person", int64(7), int64(1700000000)}},
		{values: []any{int64(0)}},
	}}
	err := (workTx{tx: conn}).Finish(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(conn.queries) == 0 || !strings.Contains(conn.queries[0].sql, "scan_completed_at") {
		t.Fatalf("successful page finish lacks a distinct scan completion marker: %#v", conn.queries)
	}
	if len(conn.execs) != 1 || !strings.Contains(conn.execs[0].sql, "last_seen_at") {
		t.Fatalf("photos finish execs = %#v", conn.execs)
	}
	conn = &engConn{rows: []engRow{
		{values: []any{"favorites", "person", int64(7), int64(1700000001)}},
		{values: []any{int64(1600000000)}},
	}}
	if err := (workTx{tx: conn}).Finish(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if len(conn.execs) != 1 || !strings.Contains(conn.execs[0].sql, "last_fave_at") {
		t.Fatalf("favorites finish execs = %#v", conn.execs)
	}
	conn = &engConn{rows: []engRow{
		{values: []any{"photos", "person", int64(7), int64(0)}},
		{values: []any{int64(1600000000)}},
	}}
	if err := (workTx{tx: conn}).Finish(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if len(conn.execs) != 0 {
		t.Fatalf("invalid scan start must keep old checkpoint, execs=%#v", conn.execs)
	}
	conn = &engConn{row: engRow{err: pgx.ErrNoRows}}
	if err := (workTx{tx: conn}).Finish(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	conn = &engConn{rows: []engRow{
		{values: []any{"albums", "person", int64(7), int64(1)}},
	}}
	if err := (workTx{tx: conn}).Finish(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	conn = &engConn{rows: []engRow{
		{values: []any{"photos", "person", int64(7), int64(1700000000)}},
		{err: pgx.ErrNoRows},
	}}
	if err := (workTx{tx: conn}).Finish(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	conn = &engConn{rows: []engRow{
		{values: []any{"photos", "person", int64(7), int64(1700000000)}},
		{values: []any{int64(0)}},
	}, failExec: "last_seen_at"}
	if err := (workTx{tx: conn}).Finish(t.Context(), 1); err == nil {
		t.Fatal("store checkpoint error must surface")
	}
	conn = &engConn{rows: []engRow{
		{values: []any{"favorites", "person", int64(7), int64(1700000001)}},
		{err: errors.New("interest read")},
	}}
	if err := (workTx{tx: conn}).Finish(t.Context(), 1); err == nil {
		t.Fatal("interest read error must surface")
	}
}

func TestStoreTransitions(t *testing.T) {
	conn := &engConn{row: engRow{values: []any{true}}}
	e := newTestEngine(conn)
	started, err := e.Start(t.Context(), 1)
	if err != nil || !started {
		t.Fatalf("started=%v err=%v", started, err)
	}
	conn.row = engRow{values: []any{false}}
	if started, _ = e.Start(t.Context(), 1); started {
		t.Fatal("false result must not start")
	}
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := e.Defer(t.Context(), 1, app.Deferral{NotBefore: when, ConsumeAttempt: true, Reason: "r"}); err != nil {
		t.Fatal(err)
	}
	if got := conn.execs[len(conn.execs)-1].args; got[1] != when || got[2] != 1 || got[3] != "r" {
		t.Fatalf("defer args = %v", got)
	}
	if err := e.Defer(t.Context(), 1, app.Deferral{NotBefore: when}); err != nil {
		t.Fatal(err)
	}
	if got := conn.execs[len(conn.execs)-1].args; got[2] != 0 {
		t.Fatalf("quota wait must not consume an attempt: %v", got)
	}
	if err := e.Finish(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if err := e.Fail(t.Context(), 1, "bad"); err != nil {
		t.Fatal(err)
	}
}

func TestStoreErrorsAreWrapped(t *testing.T) {
	conn := &engConn{failExec: "work_items"}
	e := newTestEngine(conn)
	if _, err := e.Start(t.Context(), 1); err == nil {
		t.Fatal("start")
	}
	if err := e.Defer(t.Context(), 1, app.Deferral{}); err == nil {
		t.Fatal("defer")
	}
	if err := e.Finish(t.Context(), 1); err == nil {
		t.Fatal("finish")
	}
	if err := e.Fail(t.Context(), 1, "x"); err == nil {
		t.Fatal("fail")
	}
	if err := e.Stop(t.Context(), app.StopRequest{ItemID: 1, UserID: 2, Reason: app.StopTokenRevoked}); err == nil {
		t.Fatal("stop")
	}
}

func TestStartReturnsRunnableAndWrapsError(t *testing.T) {
	conn := &engConn{rows: []engRow{
		{values: []any{true}},
		{values: []any{false}},
	}}
	e := newTestEngine(conn)

	for index, test := range []struct {
		id      int64
		started bool
	}{
		{id: 42, started: true},
		{id: 43, started: false},
	} {
		started, err := e.Start(t.Context(), test.id)
		if err != nil || started != test.started {
			t.Fatalf("Start(%d) = %v, %v; want %v, nil", test.id, started, err, test.started)
		}
		if got := conn.queries[index].args; len(got) != 1 || got[0] != test.id {
			t.Fatalf("Start(%d) query args = %v", test.id, got)
		}
	}

	databaseErr := errors.New("database unavailable")
	conn.rowErr = databaseErr
	if _, err := e.Start(t.Context(), 44); !errors.Is(err, databaseErr) || !strings.Contains(err.Error(), "crawl: start work item") {
		t.Fatalf("Start error = %v, want wrapped database error", err)
	}
}

func TestStopFailsOwnerItemsAndPublishes(t *testing.T) {
	conn := &engConn{affected: 3}
	e := newTestEngine(conn)
	if err := e.Stop(t.Context(), app.StopRequest{ItemID: 1, UserID: 2, KeyID: 9, Reason: app.StopTokenRevoked}); err != nil {
		t.Fatal(err)
	}
	if len(conn.execs) != 2 || !strings.Contains(conn.execs[0].sql, "user_id = $1") || conn.execs[0].args[0] != int64(2) || !strings.Contains(conn.execs[1].sql, "outbox_events") {
		t.Fatalf("execs = %+v", conn.execs)
	}
	conn.execs = nil
	if err := e.Stop(t.Context(), app.StopRequest{ItemID: 1, UserID: 2, KeyID: 9, Reason: app.StopInvalidKey}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(conn.execs[0].sql, "key_id = $1") || conn.execs[0].args[0] != int64(9) {
		t.Fatalf("execs = %+v", conn.execs)
	}
	conn.execs = nil
	if err := e.Stop(t.Context(), app.StopRequest{ItemID: 1, Reason: app.StopInvalidKey}); err != nil {
		t.Fatal(err)
	}
	if len(conn.execs) != 1 || !strings.Contains(conn.execs[0].sql, "'failed'") {
		t.Fatalf("an ownerless stop fails just the item: %+v", conn.execs)
	}
	conn.failExec = "outbox_events"
	if err := e.Stop(t.Context(), app.StopRequest{ItemID: 1, UserID: 2, Reason: app.StopTokenRevoked}); err == nil {
		t.Fatal("publish failure must roll back the stop")
	}
}

func TestDoWiresTransactionWriters(t *testing.T) {
	conn := &engConn{row: engRow{err: pgx.ErrNoRows}}
	e := newTestEngine(conn)
	err := e.Do(t.Context(), func(w app.Writers) error {
		if w.Catalog == nil || w.Work == nil {
			t.Fatal("writers missing")
		}
		if err := w.Work.Enqueue(t.Context(), app.NewWork{Kind: "photos", Subject: "s", UserID: 4, Depth: 1, Cursor: domain.Cursor{Page: 1, Method: "m"}}); err != nil {
			return err
		}
		if err := w.Work.Enqueue(t.Context(), app.NewWork{Kind: "photos", Subject: "s", Cursor: domain.Cursor{Page: 1, Method: "m"}}); err != nil {
			return err
		}
		if err := w.Work.Advance(t.Context(), 1, domain.Cursor{Page: 2, Method: "m"}); err != nil {
			return err
		}
		if err := w.Work.Finish(t.Context(), 1); err != nil {
			return err
		}
		if err := w.Interests.Add(t.Context(), domain.Interest{UserID: 4, PersonNSID: " 5@N01 ", Depth: 2, Via: " contact "}); err != nil {
			return err
		}
		return w.Observations.Record(t.Context(), 1, 2, []domain.Observation{{Kind: "k", Subject: "s"}, {Kind: "k2", Subject: "s", Data: json.RawMessage(`{"a":1}`)}})
	})
	if err != nil {
		t.Fatal(err)
	}
	if conn.execs[0].args[2] != int64(4) || conn.execs[1].args[2] != nil {
		t.Fatalf("user id arg = %v / %v", conn.execs[0].args[2], conn.execs[1].args[2])
	}
	var interest *engExec
	for i := range conn.execs {
		if strings.Contains(conn.execs[i].sql, "crawl_interests") {
			interest = &conn.execs[i]
		}
	}
	if interest == nil || interest.args[1] != "5@N01" || interest.args[3] != "contact" {
		t.Fatalf("interest = %+v", interest)
	}
	observed := conn.execs[len(conn.execs)-2]
	if string(observed.args[4].(json.RawMessage)) != `{}` {
		t.Fatalf("observation default data = %v", observed.args[4])
	}
}

func TestEnqueueDefaultsToDoNothingButSupportsExplicitReopen(t *testing.T) {
	conn := &engConn{}
	err := newTestEngine(conn).Do(t.Context(), func(w app.Writers) error {
		if err := w.Work.Enqueue(t.Context(), app.NewWork{Kind: "photos", Subject: "person", Cursor: domain.Cursor{Page: 1, Method: "m"}}); err != nil {
			return err
		}
		return w.Work.Enqueue(t.Context(), app.NewWork{Kind: "photos", Subject: "person", Reopen: true, Cursor: domain.Cursor{Page: 1, Method: "m"}})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(conn.execs[0].sql, "DO NOTHING") || strings.Contains(conn.execs[0].sql, "DO UPDATE") {
		t.Fatalf("ordinary enqueue query = %s", conn.execs[0].sql)
	}
	if !strings.Contains(conn.execs[1].sql, "DO UPDATE") || !strings.Contains(conn.execs[1].sql, "WHERE work_items.state IN ('done','failed')") {
		t.Fatalf("explicit reopen query = %s", conn.execs[1].sql)
	}
}

func TestEnqueueCheckCarriesCheckpointsAndMarksQueued(t *testing.T) {
	for _, test := range []struct {
		name   string
		params map[string]string
	}{
		{name: "creates params"},
		{name: "updates existing params", params: map[string]string{"keep": "value"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn := &engConn{row: engRow{values: []any{int64(123), int64(456)}}}
			work := app.NewWork{
				Kind: "check", Subject: "person", UserID: 7, Depth: 2,
				Cursor: domain.Cursor{Page: 1, Method: "flickr.people.getPhotos", Params: test.params},
			}
			if err := (workTx{tx: conn}).Enqueue(t.Context(), work); err != nil {
				t.Fatalf("Enqueue() = %v", err)
			}
			if len(conn.execs) != 2 || !strings.Contains(conn.execs[0].sql, "work_items_static_score") || !strings.Contains(conn.execs[0].sql, "ci.starred") || !strings.Contains(conn.execs[1].sql, "SET last_checked_at=now()") {
				t.Fatalf("check enqueue statements = %+v", conn.execs)
			}
			var cursor domain.Cursor
			if err := json.Unmarshal(conn.execs[0].args[4].([]byte), &cursor); err != nil {
				t.Fatal(err)
			}
			if cursor.Params["user_id"] != "person" || cursor.Params["min_upload_date"] != "123" || cursor.Params["min_fave_date"] != "456" || cursor.Params["per_page"] != "1" {
				t.Fatalf("check cursor params = %v", cursor.Params)
			}
			if test.params != nil && cursor.Params["keep"] != "value" {
				t.Fatalf("existing cursor params were discarded: %v", cursor.Params)
			}
		})
	}
}

func TestEnqueueCheckReturnsCheckpointAndMarkErrors(t *testing.T) {
	t.Run("checkpoint read", func(t *testing.T) {
		conn := &engConn{row: engRow{err: errors.New("query failed")}}
		err := (workTx{tx: conn}).Enqueue(t.Context(), app.NewWork{Kind: "check", Subject: "person", UserID: 7})
		if err == nil || len(conn.execs) != 0 {
			t.Fatalf("Enqueue() error=%v statements=%d; want read failure before insert", err, len(conn.execs))
		}
	})
	t.Run("mark queued", func(t *testing.T) {
		conn := &engConn{row: engRow{values: []any{int64(123), int64(456)}}, failExec: "SET last_checked_at"}
		err := (workTx{tx: conn}).Enqueue(t.Context(), app.NewWork{Kind: "check", Subject: "person", UserID: 7})
		if err == nil || len(conn.execs) != 2 {
			t.Fatalf("Enqueue() error=%v statements=%d; want error after enqueue and checkpoint write", err, len(conn.execs))
		}
	})
}

func TestEnqueueDropsCheckForUnknownOwner(t *testing.T) {
	conn := &engConn{row: engRow{err: pgx.ErrNoRows}}
	err := (workTx{tx: conn}).Enqueue(t.Context(), app.NewWork{
		Kind: "check", Subject: "outside-scope", UserID: 7,
		Cursor: domain.Cursor{Page: 1, Method: "flickr.people.getPhotos", Params: map[string]string{"min_upload_date": "0", "min_fave_date": "0"}},
	})
	if err != nil {
		t.Fatalf("Enqueue() = %v", err)
	}
	if len(conn.execs) != 0 {
		t.Fatalf("unknown owner enqueued work: %#v", conn.execs)
	}
}

func TestWritersWrapFailures(t *testing.T) {
	for _, fragment := range []string{"INSERT INTO work_items", "SET cursor", "SET state = 'done'", "crawl_interests", "crawl_observations"} {
		conn := &engConn{
			failExec: fragment,
			// Finish reads the completed work item then the interest checkpoint.
			rows: []engRow{
				{values: []any{"photos", "person", int64(7), int64(1700000000)}},
				{values: []any{int64(0)}},
			},
		}
		err := newTestEngine(conn).Do(t.Context(), func(w app.Writers) error {
			steps := []func() error{
				func() error {
					return w.Work.Enqueue(t.Context(), app.NewWork{Kind: "k", Subject: "s", Cursor: domain.Cursor{Page: 1, Method: "m"}})
				},
				func() error { return w.Work.Advance(t.Context(), 1, domain.Cursor{Page: 2, Method: "m"}) },
				func() error { return w.Work.Finish(t.Context(), 1) },
				func() error {
					return w.Interests.Add(t.Context(), domain.Interest{UserID: 1, PersonNSID: "n", Via: "v"})
				},
				func() error {
					return w.Observations.Record(t.Context(), 1, 1, []domain.Observation{{Kind: "k"}})
				},
			}
			for _, step := range steps {
				if err := step(); err != nil {
					return err
				}
			}
			return nil
		})
		if err == nil {
			t.Fatalf("%q failure must surface", fragment)
		}
	}
	err := newTestEngine(&engConn{}).Do(t.Context(), func(w app.Writers) error {
		return w.Interests.Add(t.Context(), domain.Interest{})
	})
	if !errors.Is(err, domain.ErrInvalidInterest) {
		t.Fatalf("err = %v", err)
	}
}

func TestReconcileTxCompleteValidatesAndSkipsRemovedInterest(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	valid := struct {
		userID int64
		person string
	}{userID: 7, person: "person"}
	invalid := []struct {
		name    string
		userID  int64
		person  string
		scanned time.Time
		done    time.Time
	}{
		{name: "user", userID: 0, person: valid.person, scanned: now, done: now},
		{name: "person", userID: valid.userID, person: "", scanned: now, done: now},
		{name: "scan time", userID: valid.userID, person: valid.person, done: now},
		{name: "completion time", userID: valid.userID, person: valid.person, scanned: now},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			conn := &engConn{}
			err := (reconcileTx{tx: conn}).Complete(t.Context(), tt.userID, tt.person, tt.scanned, tt.done)
			if err == nil || len(conn.execs) != 0 {
				t.Fatalf("Complete() = %v, execs=%d; want validation error before writes", err, len(conn.execs))
			}
		})
	}

	conn := &engConn{row: engRow{err: pgx.ErrNoRows}}
	if err := (reconcileTx{tx: conn}).Complete(t.Context(), valid.userID, valid.person, now, now); err != nil {
		t.Fatalf("removed interest Complete() = %v; want no-op", err)
	}
	if len(conn.execs) != 0 {
		t.Fatalf("removed interest wrote %d queries", len(conn.execs))
	}

	conn.row = engRow{err: errors.New("query failed")}
	if err := (reconcileTx{tx: conn}).Complete(t.Context(), valid.userID, valid.person, now, now); err == nil || !strings.Contains(err.Error(), "lock reconciliation interest") {
		t.Fatalf("interest lock error = %v", err)
	}
}

func TestReconcileTxCompleteAppliesDiffAndWrapsWriteFailures(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	completed := now.Add(time.Minute)
	newConn := func() *engConn { return &engConn{row: engRow{values: []any{1}}} }
	conn := newConn()
	if err := (reconcileTx{tx: conn}).Complete(t.Context(), 7, "person", now, completed); err != nil {
		t.Fatal(err)
	}
	if len(conn.execs) != 3 {
		t.Fatalf("Complete() issued %d writes; want photo, favorite, and completion updates", len(conn.execs))
	}
	for i, want := range []string{"UPDATE photos", "UPDATE favorites"} {
		if !strings.Contains(conn.execs[i].sql, want) || conn.execs[i].args[0] != "person" || conn.execs[i].args[1] != now || conn.execs[i].args[2] != completed || conn.execs[i].args[3] != int64(7) {
			t.Fatalf("write %d = %+v; want %q and reconciliation arguments", i, conn.execs[i], want)
		}
	}
	if !strings.Contains(conn.execs[2].sql, "UPDATE crawl_interests") || conn.execs[2].args[0] != int64(7) || conn.execs[2].args[1] != "person" || conn.execs[2].args[2] != completed {
		t.Fatalf("completion write = %+v; want user, person, and completion time", conn.execs[2])
	}
	if !strings.Contains(conn.execs[0].sql, "p.visibility='public'") || !strings.Contains(conn.execs[1].sql, "flickr_accounts") {
		t.Fatalf("missing-row safety predicates absent: photo=%s favorite=%s", conn.execs[0].sql, conn.execs[1].sql)
	}

	for _, tt := range []struct {
		fragment string
		want     string
	}{
		{fragment: "UPDATE photos", want: "tombstone photos"},
		{fragment: "UPDATE favorites", want: "mark favorites"},
		{fragment: "UPDATE crawl_interests", want: "record completed reconciliation"},
	} {
		t.Run(tt.want, func(t *testing.T) {
			conn := newConn()
			conn.failExec = tt.fragment
			err := (reconcileTx{tx: conn}).Complete(t.Context(), 7, "person", now, completed)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Complete() error = %v; want %q", err, tt.want)
			}
		})
	}
}

func TestLockReadsItemForUpdate(t *testing.T) {
	conn := &engConn{row: engRow{values: []any{int64(5), "photos", "1@N01", int64(3), 0, "running", 0, []byte(`{"page":1,"method":"m"}`)}}}
	err := newTestEngine(conn).Do(t.Context(), func(w app.Writers) error {
		item, err := w.Work.Lock(t.Context(), 5)
		if item.UserID != 3 || item.State != "running" {
			t.Fatalf("item = %+v", item)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNewEngineValidatesArguments(t *testing.T) {
	if _, err := NewEngine(nil, func(Conn) app.CatalogWriter { return nil }); err == nil {
		t.Fatal("pool required")
	}
	if _, err := NewEngine(&pgxpool.Pool{}, nil); err == nil {
		t.Fatal("catalog factory required")
	}
	if engine, err := NewEngine(&pgxpool.Pool{}, func(Conn) app.CatalogWriter { return engCatalog{} }); err != nil || engine == nil {
		t.Fatalf("NewEngine() = %v, %v; want initialized engine", engine, err)
	}
}
