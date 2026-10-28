package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type partitionDBFake struct {
	tx       *partitionTxFake
	beginErr error
	execErr  error
	queryErr error
}

func (p *partitionDBFake) Begin(context.Context) (pgx.Tx, error) {
	if p.beginErr != nil {
		return nil, p.beginErr
	}
	return p.tx, nil
}

func (p *partitionDBFake) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("DELETE 0"), p.execErr
}

func (p *partitionDBFake) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, p.queryErr
}

type partitionTxFake struct {
	pgx.Tx
	execs      []string
	queryRows  pgx.Rows
	execErrAt  int
	execErr    error
	queryErr   error
	commitErr  error
	committed  bool
	rolledBack bool
}

func (tx *partitionTxFake) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	tx.execs = append(tx.execs, sql)
	if tx.execErr != nil && len(tx.execs) == tx.execErrAt {
		return pgconn.CommandTag{}, tx.execErr
	}
	return pgconn.NewCommandTag("CREATE TABLE"), nil
}

func (tx *partitionTxFake) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return tx.queryRows, tx.queryErr
}

func (tx *partitionTxFake) Commit(context.Context) error {
	tx.committed = true
	return tx.commitErr
}

func (tx *partitionTxFake) Rollback(context.Context) error {
	tx.rolledBack = true
	return nil
}

func TestMaintainCallLogPartitionsCreatesAndDropsOnlyDailyPartitions(t *testing.T) {
	rows := &fakeRows{data: [][]any{{"api_call_log_20261026"}}}
	tx := &partitionTxFake{queryRows: rows}
	pool := &partitionDBFake{tx: tx}
	oldest := time.Date(2026, 10, 27, 12, 0, 0, 0, time.FixedZone("local", -5*60*60))
	through := oldest.AddDate(0, 0, 2)
	if err := (Partitions{Pool: pool}).MaintainCallLogPartitions(context.Background(), oldest, through); err != nil {
		t.Fatal(err)
	}
	if !tx.committed || len(tx.execs) != 4 || !strings.Contains(tx.execs[0], "api_call_log_20261027") || tx.execs[3] != `DROP TABLE "api_call_log_20261026"` {
		t.Fatalf("commit=%v execs=%q", tx.committed, tx.execs)
	}
	if !tx.rolledBack {
		t.Fatal("transaction rollback was not attempted after commit")
	}
}

func TestMaintainCallLogPartitionsRejectsInvalidConfigurationAndRange(t *testing.T) {
	day := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name string
		p    Partitions
		ctx  context.Context
		from time.Time
		to   time.Time
	}{
		{name: "nil pool", ctx: context.Background(), from: day, to: day},
		{name: "nil context", p: Partitions{Pool: &partitionDBFake{}}, from: day, to: day},
		{name: "reversed", p: Partitions{Pool: &partitionDBFake{}}, ctx: context.Background(), from: day.AddDate(0, 0, 1), to: day},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.p.MaintainCallLogPartitions(test.ctx, test.from, test.to); err == nil {
				t.Fatal("invalid call was accepted")
			}
		})
	}
}

func TestMaintainCallLogPartitionsReportsTransactionErrors(t *testing.T) {
	day := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	boom := errors.New("database failed")
	tests := []struct {
		name string
		pool *partitionDBFake
	}{
		{name: "begin", pool: &partitionDBFake{beginErr: boom}},
		{name: "create", pool: &partitionDBFake{tx: &partitionTxFake{execErrAt: 1, execErr: boom}}},
		{name: "query", pool: &partitionDBFake{tx: &partitionTxFake{queryErr: boom}}},
		{name: "scan", pool: &partitionDBFake{tx: &partitionTxFake{queryRows: &fakeRows{data: [][]any{{int64(4)}}}}}},
		{name: "rows", pool: &partitionDBFake{tx: &partitionTxFake{queryRows: &fakeRows{err: boom}}}},
		{name: "drop", pool: &partitionDBFake{tx: &partitionTxFake{queryRows: &fakeRows{data: [][]any{{"api_call_log_20261020"}}}, execErrAt: 2, execErr: boom}}},
		{name: "commit", pool: &partitionDBFake{tx: &partitionTxFake{queryRows: &fakeRows{}, commitErr: boom}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := (Partitions{Pool: test.pool}).MaintainCallLogPartitions(context.Background(), day, day); err == nil {
				t.Fatal("database error was ignored")
			}
		})
	}
}

func TestRollupCallsAndPruneCallRollups(t *testing.T) {
	pool := &partitionDBFake{}
	from := time.Date(2026, 10, 28, 0, 0, 0, 0, time.FixedZone("local", -5*60*60))
	until := from.Add(time.Hour)
	if err := (Partitions{Pool: pool}).RollupCalls(context.Background(), from, until); err != nil {
		t.Fatal(err)
	}
	if err := (Partitions{Pool: pool}).PruneCallRollups(context.Background(), from); err != nil {
		t.Fatal(err)
	}
	if err := (Partitions{Pool: pool}).RollupCalls(nil, from, until); err == nil {
		t.Fatal("nil context accepted for rollup")
	}
	if err := (Partitions{Pool: pool}).RollupCalls(context.Background(), until, from); err == nil {
		t.Fatal("empty range accepted for rollup")
	}
	if err := (Partitions{Pool: &partitionDBFake{execErr: errors.New("down")}}).RollupCalls(context.Background(), from, until); err == nil {
		t.Fatal("rollup database error ignored")
	}
	if err := (Partitions{Pool: &partitionDBFake{execErr: errors.New("down")}}).PruneCallRollups(context.Background(), from); err == nil {
		t.Fatal("prune database error ignored")
	}
	if err := (Partitions{}).PruneCallRollups(context.Background(), from); err == nil {
		t.Fatal("nil pool accepted for prune")
	}
	if err := (Partitions{Pool: pool}).PruneCallRollups(nil, from); err == nil {
		t.Fatal("nil context accepted for prune")
	}
}
