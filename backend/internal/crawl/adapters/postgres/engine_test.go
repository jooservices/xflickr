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
		case *[]byte:
			*p = r.values[i].([]byte)
		}
	}
	return nil
}

type engConn struct {
	execs    []engExec
	failExec string
	affected int64
	row      engRow
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
func (c *engConn) QueryRow(context.Context, string, ...any) pgx.Row { return c.row }
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

func TestStoreTransitions(t *testing.T) {
	conn := &engConn{affected: 1}
	e := newTestEngine(conn)
	started, err := e.Start(t.Context(), 1)
	if err != nil || !started {
		t.Fatalf("started=%v err=%v", started, err)
	}
	conn.affected = 0
	if started, _ = e.Start(t.Context(), 1); started {
		t.Fatal("zero rows must not start")
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
	conn := &engConn{failExec: "UPDATE"}
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
	conn := &engConn{}
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
		conn := &engConn{failExec: fragment}
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
}
