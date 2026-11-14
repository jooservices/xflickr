package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jooservices/xflickr/internal/platform/ops"
)

type recoveryError string

func (err recoveryError) Error() string { return string(err) }

const recoveryFailure recoveryError = "injected transaction failure"

type recoveryBegin struct {
	tx  pgx.Tx
	err error
}

func (b recoveryBegin) Begin(context.Context) (pgx.Tx, error) { return b.tx, b.err }

type recoveryTx struct {
	pgx.Tx
	execPrefix string
	queryErr   error
	commitErr  error
}

func (tx recoveryTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if tx.execPrefix != "" && strings.HasPrefix(sql, tx.execPrefix) {
		return pgconn.CommandTag{}, recoveryFailure
	}
	return tx.Tx.Exec(ctx, sql, args...)
}

func (tx recoveryTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if tx.queryErr != nil {
		return nil, tx.queryErr
	}
	return tx.Tx.Query(ctx, sql, args...)
}

func (tx recoveryTx) Commit(ctx context.Context) error {
	if tx.commitErr != nil {
		return tx.commitErr
	}
	return tx.Tx.Commit(ctx)
}

func recoveryFixture() (*snapshotDB, *recoveryTx) {
	pool := &snapshotDB{estimate: 1}
	parent := &beginDB{pool: pool}
	return pool, &recoveryTx{Tx: &snapshotTx{db: pool, parent: parent}}
}

func TestSnapshotReportsTransactionFailures(t *testing.T) {
	for _, phase := range []string{"begin", "timeout setup", "query", "commit"} {
		t.Run(phase, func(t *testing.T) {
			pool, tx := recoveryFixture()
			begin := recoveryBegin{tx: tx}
			switch phase {
			case "begin":
				begin.err = recoveryFailure
			case "timeout setup":
				tx.execPrefix = "SET LOCAL"
			case "query":
				pool.failAt = 1
			case "commit":
				tx.commitErr = recoveryFailure
			}
			_, err := (Store{Pool: pool, DB: begin}).Snapshot(context.Background())
			if err == nil {
				t.Fatalf("%s failure was ignored", phase)
			}
		})
	}
}

func TestRecoveringQuerierReportsSavepointAndQueryFailures(t *testing.T) {
	pool, tx := recoveryFixture()
	q := &recoveringQuerier{tx: tx}
	if _, err := q.Exec(context.Background(), "SET LOCAL test = 1"); err != nil {
		t.Fatal(err)
	}
	tx.execPrefix = "SAVEPOINT"
	if _, err := q.Query(context.Background(), "SELECT 1"); !errors.Is(err, recoveryFailure) {
		t.Fatalf("query savepoint failure = %v", err)
	}
	if err := q.QueryRow(context.Background(), "SELECT 1").Scan(); !errors.Is(err, recoveryFailure) {
		t.Fatalf("query row savepoint failure = %v", err)
	}
	tx.execPrefix = ""
	tx.queryErr = recoveryFailure
	if _, err := q.Query(context.Background(), "SELECT 1"); !errors.Is(err, recoveryFailure) || pool.rollbacks != 1 {
		t.Fatalf("query failure = %v, rollbacks = %d", err, pool.rollbacks)
	}
}

func TestRecoveringRowsRollBackOnceOnScanFailure(t *testing.T) {
	pool, tx := recoveryFixture()
	pool.scanErr = recoveryFailure
	rows, err := (&recoveringQuerier{tx: tx}).Query(context.Background(), "SELECT state")
	if err != nil || !rows.Next() {
		t.Fatalf("query = %v", err)
	}
	if err := rows.Scan(); !errors.Is(err, recoveryFailure) {
		t.Fatalf("scan failure = %v", err)
	}
	rows.Close()
	if pool.rollbacks != 1 {
		t.Fatalf("rollbacks = %d, want one", pool.rollbacks)
	}
}

func TestCountsReportsFallbackFailure(t *testing.T) {
	pool := &snapshotDB{estimate: 0, failAt: 2}
	if _, _, err := (Store{Pool: pool}).counts(context.Background(), pool, &ops.Snapshot{}); err == nil {
		t.Fatal("fallback count failure was ignored")
	}
}

func TestGroupedAndKeysDegradeOnTimeout(t *testing.T) {
	timeout := &pgconn.PgError{Code: "57014", Message: "statement timeout"}
	for _, phase := range []string{"query", "rows"} {
		t.Run(phase, func(t *testing.T) {
			pool := &snapshotDB{}
			if phase == "query" {
				pool.queryErr = timeout
			} else {
				pool.rowsErr = timeout
			}
			snapshot := &ops.Snapshot{}
			if _, err := (Store{Pool: pool}).grouped(context.Background(), pool, snapshot, "counts", []string{"pending"}, "group"); err != nil {
				t.Fatal(err)
			}
			if _, err := (Store{Pool: pool}).keys(context.Background(), pool, snapshot); err != nil {
				t.Fatal(err)
			}
			if len(snapshot.DegradedQueries) != 2 {
				t.Fatalf("degraded queries = %v", snapshot.DegradedQueries)
			}
		})
	}
	pool := &snapshotDB{queryErr: recoveryFailure}
	if _, err := (Store{Pool: pool}).keys(context.Background(), pool, &ops.Snapshot{}); !errors.Is(err, recoveryFailure) {
		t.Fatalf("key query failure = %v", err)
	}
}

func TestTimeoutClearsOptionalTimestamp(t *testing.T) {
	value := time.Now()
	row := timeoutAwareRow{row: snapshotRow{err: &pgconn.PgError{Code: "57014"}}, snap: &ops.Snapshot{}, name: "oldest"}
	if err := row.Scan(&value); err != nil {
		t.Fatal(err)
	}
	var optional = &value
	if err := row.Scan(&optional); err != nil || optional != nil {
		t.Fatalf("optional timestamp = %v, error = %v", optional, err)
	}
}
