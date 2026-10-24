package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestUnitOfWorkDoCommitsSerializableTransaction(t *testing.T) {
	tx := &fakeUnitOfWorkTx{}
	database := &fakeUnitOfWorkDB{transactions: []*fakeUnitOfWorkTx{tx}}
	uow := NewUnitOfWork(database)

	called := false
	err := uow.Do(t.Context(), func(got Tx) error {
		called = true
		if got != tx {
			t.Fatal("callback received a different transaction")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if !called || tx.commits != 1 || tx.rollbacks != 0 {
		t.Fatalf("callback/commit/rollback = %v/%d/%d", called, tx.commits, tx.rollbacks)
	}
	if database.options[0].IsoLevel != pgx.Serializable {
		t.Fatalf("transaction isolation = %v, want serializable", database.options[0].IsoLevel)
	}
}

func TestUnitOfWorkRetriesSerializationFailureUpToThreeAttempts(t *testing.T) {
	first := &fakeUnitOfWorkTx{}
	second := &fakeUnitOfWorkTx{}
	third := &fakeUnitOfWorkTx{}
	database := &fakeUnitOfWorkDB{transactions: []*fakeUnitOfWorkTx{first, second, third}}
	uow := NewUnitOfWork(database)
	callbacks := 0

	err := uow.Do(t.Context(), func(Tx) error {
		callbacks++
		if callbacks < MaxTransactionAttempts {
			return &pgconn.PgError{Code: "40001", Message: "serialization failure"}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if callbacks != MaxTransactionAttempts || first.rollbacks != 1 || second.rollbacks != 1 || third.commits != 1 {
		t.Fatalf("attempts=%d first=%+v second=%+v third=%+v", callbacks, first, second, third)
	}
}

func TestUnitOfWorkRetriesCommitSerializationFailure(t *testing.T) {
	first := &fakeUnitOfWorkTx{commitErr: &pgconn.PgError{Code: "40001"}}
	second := &fakeUnitOfWorkTx{}
	database := &fakeUnitOfWorkDB{transactions: []*fakeUnitOfWorkTx{first, second}}
	callbacks := 0

	err := NewUnitOfWork(database).Do(t.Context(), func(Tx) error {
		callbacks++
		return nil
	})
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if callbacks != 2 || first.commits != 1 || second.commits != 1 {
		t.Fatalf("callbacks=%d first=%+v second=%+v", callbacks, first, second)
	}
}

func TestUnitOfWorkReturnsNonSerializationErrorWithoutRetry(t *testing.T) {
	wantErr := errors.New("callback failed")
	tx := &fakeUnitOfWorkTx{}
	database := &fakeUnitOfWorkDB{transactions: []*fakeUnitOfWorkTx{tx, {}}}
	err := NewUnitOfWork(database).Do(t.Context(), func(Tx) error { return wantErr })
	if !errors.Is(err, wantErr) {
		t.Fatalf("Do() error = %v, want callback error", err)
	}
	if database.begins != 1 || tx.rollbacks != 1 || tx.commits != 0 {
		t.Fatalf("begins=%d tx=%+v", database.begins, tx)
	}
}

func TestUnitOfWorkReturnsFinalSerializationFailure(t *testing.T) {
	wantErr := &pgconn.PgError{Code: "40001", Message: "serialization failure"}
	transactions := []*fakeUnitOfWorkTx{
		{}, {}, {commitErr: wantErr},
	}
	database := &fakeUnitOfWorkDB{transactions: transactions}
	callbacks := 0
	err := NewUnitOfWork(database).Do(t.Context(), func(Tx) error {
		callbacks++
		if callbacks < MaxTransactionAttempts {
			return &pgconn.PgError{Code: "40001", Message: "serialization failure"}
		}
		return nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Do() error = %v, want final commit error", err)
	}
	if database.begins != MaxTransactionAttempts || callbacks != MaxTransactionAttempts {
		t.Fatalf("begins/callbacks = %d/%d, want %d", database.begins, callbacks, MaxTransactionAttempts)
	}
}

func TestUnitOfWorkReturnsBeginError(t *testing.T) {
	database := &fakeUnitOfWorkDB{beginErr: errors.New("database unavailable")}
	err := NewUnitOfWork(database).Do(t.Context(), func(Tx) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "begin unit of work") {
		t.Fatalf("Do() error = %v", err)
	}
}

func TestUnitOfWorkValidationAndCanceledRetry(t *testing.T) {
	if err := (*UnitOfWork)(nil).Do(t.Context(), func(Tx) error { return nil }); err == nil {
		t.Fatal("Do() with nil unit of work succeeded")
	}
	if err := NewUnitOfWork(nil).Do(t.Context(), func(Tx) error { return nil }); err == nil {
		t.Fatal("Do() with nil database succeeded")
	}
	if err := NewUnitOfWork(&fakeUnitOfWorkDB{}).Do(nil, func(Tx) error { return nil }); err == nil {
		t.Fatal("Do() with nil context succeeded")
	}
	if err := NewUnitOfWork(&fakeUnitOfWorkDB{}).Do(t.Context(), nil); err == nil {
		t.Fatal("Do() with nil callback succeeded")
	}

	ctx, cancel := context.WithCancel(t.Context())
	tx := &fakeUnitOfWorkTx{}
	database := &fakeUnitOfWorkDB{transactions: []*fakeUnitOfWorkTx{tx, {}}}
	err := NewUnitOfWork(database).Do(ctx, func(Tx) error {
		cancel()
		return &pgconn.PgError{Code: "40001"}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Do() error = %v, want context canceled", err)
	}
	if database.begins != 1 || tx.rollbacks != 1 {
		t.Fatalf("begins=%d tx=%+v", database.begins, tx)
	}
}

func TestUnitOfWorkPanicRollsBackAndRepanics(t *testing.T) {
	const panicValue = "unit of work callback panicked"
	tx := &fakeUnitOfWorkTx{}
	database := &fakeUnitOfWorkDB{transactions: []*fakeUnitOfWorkTx{tx, {}}}

	defer func() {
		recovered := recover()
		if recovered != panicValue {
			t.Fatalf("recovered = %#v, want %#v", recovered, panicValue)
		}
		if database.begins != 1 || tx.rollbacks != 1 || tx.commits != 0 {
			t.Fatalf("begins=%d commits=%d rollbacks=%d", database.begins, tx.commits, tx.rollbacks)
		}
		if tx.rollbackCtx != context.Background() {
			t.Fatalf("rollback context = %v, want context.Background()", tx.rollbackCtx)
		}
	}()

	err := NewUnitOfWork(database).Do(t.Context(), func(Tx) error {
		panic(panicValue)
	})
	t.Fatalf("Do() returned %v, want panic", err)
}

func TestUnitOfWorkCombinesRollbackError(t *testing.T) {
	callbackErr := &pgconn.PgError{Code: "40001"}
	rollbackErr := errors.New("rollback failed")
	first := &fakeUnitOfWorkTx{rollbackErr: rollbackErr}
	second := &fakeUnitOfWorkTx{}
	database := &fakeUnitOfWorkDB{transactions: []*fakeUnitOfWorkTx{first, second}}
	calls := 0
	err := NewUnitOfWork(database).Do(t.Context(), func(Tx) error {
		calls++
		if calls == 1 {
			return callbackErr
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if calls != 2 || first.rollbacks != 1 {
		t.Fatalf("calls=%d first=%+v", calls, first)
	}
}

type fakeUnitOfWorkDB struct {
	transactions []*fakeUnitOfWorkTx
	options      []pgx.TxOptions
	beginErr     error
	begins       int
}

func (database *fakeUnitOfWorkDB) BeginTx(_ context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	database.begins++
	database.options = append(database.options, options)
	if database.beginErr != nil {
		return nil, database.beginErr
	}
	if database.begins > len(database.transactions) {
		return nil, errors.New("unexpected transaction attempt")
	}
	return database.transactions[database.begins-1], nil
}

type fakeUnitOfWorkTx struct {
	pgx.Tx
	commits     int
	rollbacks   int
	commitErr   error
	rollbackErr error
	rollbackCtx context.Context
}

func (tx *fakeUnitOfWorkTx) Commit(context.Context) error {
	tx.commits++
	return tx.commitErr
}

func (tx *fakeUnitOfWorkTx) Rollback(ctx context.Context) error {
	tx.rollbacks++
	tx.rollbackCtx = ctx
	return tx.rollbackErr
}
