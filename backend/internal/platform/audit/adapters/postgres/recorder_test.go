package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

type execStub struct {
	query string
	args  []any
	err   error
}

func (s *execStub) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	s.query = query
	s.args = args
	return pgconn.NewCommandTag("INSERT 0 1"), s.err
}

func TestRecorderRecord(t *testing.T) {
	stub := &execStub{}
	rec := Recorder{pool: stub}
	if err := rec.Record(context.Background(), "user:1", "plans.update", "plans", map[string]any{"count": 2}); err != nil {
		t.Fatal(err)
	}
	if stub.query == "" || len(stub.args) != 4 || stub.args[0] != "user:1" || stub.args[1] != "plans.update" || stub.args[2] != "plans" || stub.args[3] != "{\"count\":2}" {
		t.Fatalf("exec = %q %#v", stub.query, stub.args)
	}
	if err := rec.Record(context.Background(), "a", "b", "c", map[string]any{"invalid": make(chan int)}); err == nil {
		t.Fatal("Record accepted unmarshalable details")
	}
	stub.err = errors.New("db")
	if err := rec.Record(context.Background(), "a", "b", "c", nil); err == nil {
		t.Fatal("Record swallowed database error")
	}
}
