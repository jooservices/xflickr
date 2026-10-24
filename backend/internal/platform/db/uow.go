package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	// MaxTransactionAttempts includes the initial transaction attempt.
	MaxTransactionAttempts = 3

	serializationRetryBackoff = 10 * time.Millisecond
)

// Tx is a PostgreSQL transaction available to one unit of work.
type Tx interface {
	pgx.Tx
}

// Beginner starts PostgreSQL transactions. *pgxpool.Pool implements it.
type Beginner interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// UnitOfWork runs a callback in an atomic PostgreSQL transaction.
type UnitOfWork struct {
	database Beginner
}

// NewUnitOfWork creates a unit of work backed by database.
func NewUnitOfWork(database Beginner) *UnitOfWork {
	return &UnitOfWork{database: database}
}

// Do runs fn in a serializable transaction. Serialization failures retry up to
// MaxTransactionAttempts times; other failures are returned immediately.
func (uow *UnitOfWork) Do(ctx context.Context, fn func(Tx) error) error {
	if uow == nil || uow.database == nil {
		return errors.New("db: unit of work database is required")
	}
	if ctx == nil {
		return errors.New("db: unit of work context is required")
	}
	if fn == nil {
		return errors.New("db: unit of work callback is required")
	}

	var lastErr error
	for attempt := 1; attempt <= MaxTransactionAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		tx, err := uow.database.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
		if err != nil {
			return fmt.Errorf("db: begin unit of work: %w", err)
		}

		lastErr = runCallback(tx, fn)
		if lastErr == nil {
			lastErr = tx.Commit(ctx)
		} else {
			rollbackErr := tx.Rollback(context.Background())
			if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				lastErr = errors.Join(lastErr, fmt.Errorf("db: rollback unit of work: %w", rollbackErr))
			}
		}

		if !isSerializationFailure(lastErr) || attempt == MaxTransactionAttempts {
			if lastErr != nil {
				return lastErr
			}
			return nil
		}
		if err := waitForSerializationRetry(ctx, attempt); err != nil {
			return err
		}
	}
	return lastErr
}

// runCallback runs fn. A panic rolls the transaction back with a detached
// context, which releases the pooled connection, then re-panics with the
// original value.
func runCallback(tx Tx, fn func(Tx) error) error {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		_ = tx.Rollback(context.Background())
		panic(recovered)
	}()
	return fn(tx)
}

func isSerializationFailure(err error) bool {
	var stateError interface{ SQLState() string }
	return errors.As(err, &stateError) && stateError.SQLState() == "40001"
}

func waitForSerializationRetry(ctx context.Context, attempt int) error {
	delay := serializationRetryBackoff * time.Duration(attempt)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
