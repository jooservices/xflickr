package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type cycleRowFake struct {
	values []any
	err    error
}

func (r cycleRowFake) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for index, value := range r.values {
		switch target := dest[index].(type) {
		case *string:
			*target = value.(string)
		case *int:
			*target = value.(int)
		case *int64:
			*target = value.(int64)
		case *pgtype.Timestamptz:
			if value != nil {
				*target = pgtype.Timestamptz{Time: value.(time.Time), Valid: true}
			}
		default:
			return errors.New("unexpected scan target")
		}
	}
	return nil
}

type cycleTxFake struct {
	cursor      string
	due         [][5]any
	queries     int
	execs       int
	queryErrAt  int
	execErrAt   int
	commitErr   error
	rollbackErr error
	lastQuery   string
	lastArgs    []any
	lastExec    string
	execArgs    []any
}

type cycleRowsFake struct {
	values  []int64
	index   int
	scanErr error
	rowsErr error
	closed  bool
}

func (r *cycleRowsFake) Next() bool { return r.index < len(r.values) }
func (r *cycleRowsFake) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	*dest[0].(*int64) = r.values[r.index]
	r.index++
	return nil
}
func (r *cycleRowsFake) Err() error { return r.rowsErr }
func (r *cycleRowsFake) Close()     { r.closed = true }

func (tx *cycleTxFake) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	tx.execs++
	tx.lastExec, tx.execArgs = query, args
	if tx.execErrAt == tx.execs {
		return pgconn.CommandTag{}, errors.New("exec failed")
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (tx *cycleTxFake) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	tx.queries++
	tx.lastQuery = query
	tx.lastArgs = args
	if tx.queryErrAt == tx.queries {
		return cycleRowFake{err: errors.New("query failed")}
	}
	if strings.Contains(query, "cursor_person_nsid") {
		return cycleRowFake{values: []any{tx.cursor}}
	}
	index := tx.queries - 2
	if index < 0 || index >= len(tx.due) {
		return cycleRowFake{err: pgx.ErrNoRows}
	}
	if tx.due[index][0] == nil {
		return cycleRowFake{err: pgx.ErrNoRows}
	}
	return cycleRowFake{values: tx.due[index][:]}
}

func (tx *cycleTxFake) Commit(context.Context) error   { return tx.commitErr }
func (tx *cycleTxFake) Rollback(context.Context) error { return tx.rollbackErr }

func TestScheduleNextAdvancesAndPersistsCycle(t *testing.T) {
	tx := &cycleTxFake{cursor: "a", due: [][5]any{{"b", 1, int64(11), int64(22), nil}}}
	repository := &CycleRepository{begin: func(context.Context) (cycleTx, error) { return tx, nil }}
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	queued, err := repository.ScheduleNext(context.Background(), 4, now, time.Hour)
	if err != nil || !queued || tx.execs != 4 {
		t.Fatalf("ScheduleNext() = %v, %v; execs=%d", queued, err, tx.execs)
	}
	if tx.execArgs[1] != "b" {
		t.Fatalf("cycle cursor args = %#v", tx.execArgs)
	}
}

func TestScheduleNextWrapsAfterCursorAndHandlesNoDuePerson(t *testing.T) {
	tx := &cycleTxFake{cursor: "z", due: [][5]any{{nil, nil, nil, nil, nil}, {"a", 0, int64(0), int64(0), nil}}}
	repository := &CycleRepository{begin: func(context.Context) (cycleTx, error) { return tx, nil }}
	queued, err := repository.ScheduleNext(context.Background(), 4, time.Now(), time.Hour)
	if err != nil || !queued {
		t.Fatalf("wrap ScheduleNext() = %v, %v", queued, err)
	}
	if tx.execs != 5 || !strings.Contains(tx.lastExec, "INSERT INTO work_items") {
		t.Fatalf("cycle wrap did not enqueue contacts probe: execs=%d query=%s", tx.execs, tx.lastExec)
	}
	noDue := &cycleTxFake{cursor: ""}
	repository.begin = func(context.Context) (cycleTx, error) { return noDue, nil }
	queued, err = repository.ScheduleNext(context.Background(), 4, time.Now(), time.Hour)
	if err != nil || queued || noDue.execs != 1 {
		t.Fatalf("no due ScheduleNext() = %v, %v; execs=%d", queued, err, noDue.execs)
	}
	notDue := &cycleTxFake{cursor: "a", due: [][5]any{{"b", 1, int64(0), int64(0), time.Now()}}}
	repository.begin = func(context.Context) (cycleTx, error) { return notDue, nil }
	queued, err = repository.ScheduleNext(context.Background(), 4, time.Now(), time.Hour)
	if err != nil || queued || notDue.execArgs[1] != "b" {
		t.Fatalf("not due person was not visited: queued=%v err=%v cursor args=%#v", queued, err, notDue.execArgs)
	}
}

func TestScheduleNextRejectsInvalidAndCanceledCalls(t *testing.T) {
	if _, err := NewCycleRepository(nil); err == nil {
		t.Fatal("nil pool accepted")
	}
	if _, err := (*CycleRepository)(nil).ScheduleNext(context.Background(), 1, time.Now(), time.Hour); err == nil {
		t.Fatal("nil repository accepted")
	}
	repository := &CycleRepository{begin: func(context.Context) (cycleTx, error) { return &cycleTxFake{}, nil }}
	if _, err := repository.ScheduleNext(nil, 1, time.Now(), time.Hour); err == nil {
		t.Fatal("nil context accepted")
	}
	if _, err := repository.ScheduleNext(context.Background(), 0, time.Now(), time.Hour); err == nil {
		t.Fatal("invalid user accepted")
	}
	if _, err := repository.ScheduleNext(context.Background(), 1, time.Now(), 0); err == nil {
		t.Fatal("invalid interval accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repository.ScheduleNext(ctx, 1, time.Now(), time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v", err)
	}
}

func TestScheduleNextPropagatesDatabaseFailures(t *testing.T) {
	beginErr := errors.New("begin failed")
	repository := &CycleRepository{begin: func(context.Context) (cycleTx, error) { return nil, beginErr }}
	if _, err := repository.ScheduleNext(context.Background(), 1, time.Now(), time.Hour); !errors.Is(err, beginErr) {
		t.Fatalf("begin error = %v", err)
	}
	for _, tx := range []*cycleTxFake{
		{execErrAt: 1},
		{queryErrAt: 1},
		{cursor: "a", queryErrAt: 2},
		{cursor: "a", due: [][5]any{{"b", 0, int64(0), int64(0), nil}}, execErrAt: 2},
		{cursor: "a", due: [][5]any{{"b", 0, int64(0), int64(0), nil}}, execErrAt: 3},
		{cursor: "a", due: [][5]any{{"b", 0, int64(0), int64(0), nil}}, execErrAt: 4},
		{cursor: "a", due: [][5]any{{"b", 0, int64(0), int64(0), nil}}, commitErr: errors.New("commit failed")},
	} {
		repository.begin = func(context.Context) (cycleTx, error) { return tx, nil }
		if _, err := repository.ScheduleNext(context.Background(), 1, time.Now(), time.Hour); err == nil {
			t.Fatalf("database failure not returned for fake tx %+v", tx)
		}
	}
}

func TestUsersWithInterestsValidatesAndReadsRows(t *testing.T) {
	if _, err := NewCycleRepository(nil); err == nil {
		t.Fatal("nil pool accepted")
	}
	if _, err := (*CycleRepository)(nil).UsersWithInterests(context.Background()); err == nil {
		t.Fatal("nil repository accepted")
	}
	if _, err := (&CycleRepository{}).UsersWithInterests(context.Background()); err == nil {
		t.Fatal("missing query function accepted")
	}
	repository := &CycleRepository{users: func(context.Context) (cycleRows, error) {
		return &cycleRowsFake{values: []int64{3, 8}}, nil
	}}
	users, err := repository.UsersWithInterests(context.Background())
	if err != nil || len(users) != 2 || users[0] != 3 || users[1] != 8 {
		t.Fatalf("UsersWithInterests() = %v, %v", users, err)
	}
	if _, err := repository.UsersWithInterests(nil); err == nil {
		t.Fatal("nil context accepted")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repository.UsersWithInterests(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v", err)
	}
	queryErr := errors.New("query failed")
	repository.users = func(context.Context) (cycleRows, error) { return nil, queryErr }
	if _, err := repository.UsersWithInterests(context.Background()); !errors.Is(err, queryErr) {
		t.Fatalf("query error = %v", err)
	}
	scanErr := errors.New("scan failed")
	rows := &cycleRowsFake{values: []int64{1}, scanErr: scanErr}
	repository.users = func(context.Context) (cycleRows, error) { return rows, nil }
	if _, err := repository.UsersWithInterests(context.Background()); !errors.Is(err, scanErr) || !rows.closed {
		t.Fatalf("scan error = %v; rows closed=%v", err, rows.closed)
	}
	rows = &cycleRowsFake{rowsErr: errors.New("rows failed")}
	repository.users = func(context.Context) (cycleRows, error) { return rows, nil }
	if _, err := repository.UsersWithInterests(context.Background()); err == nil || !rows.closed {
		t.Fatalf("rows error = %v; rows closed=%v", err, rows.closed)
	}
}
