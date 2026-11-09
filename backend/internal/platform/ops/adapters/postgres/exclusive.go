package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/ops"
)

// HealthLockKey is the fixed session advisory lock for the crawl-health monitor.
// Only one process in the cluster runs a snapshot tick at a time.
const HealthLockKey int64 = 0x78666c636b683369 // "xflckh3i"

// LockConn is one PostgreSQL session held for the advisory lock lifetime.
type LockConn interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Release()
}

// ConnPool acquires a dedicated connection so the advisory lock and unlock
// share one PostgreSQL session.
type ConnPool interface {
	Acquire(context.Context) (LockConn, error)
}

// PoolAdapter adapts *pgxpool.Pool to ConnPool.
type PoolAdapter struct {
	Pool *pgxpool.Pool
}

// Acquire returns a session connection for the health advisory lock.
func (a PoolAdapter) Acquire(ctx context.Context) (LockConn, error) {
	if a.Pool == nil {
		return nil, errors.New("ops: health lock pool is required")
	}
	conn, err := a.Pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("ops: acquire health lock connection: %w", err)
	}
	return conn, nil
}

// ExclusiveStore serialises Snapshot behind HealthLockKey. When the lock is
// already held it returns ops.ErrSnapshotBusy so the monitor can skip the tick.
type ExclusiveStore struct {
	Inner ops.Store
	Pool  ConnPool
	Key   int64
}

var _ ops.Store = ExclusiveStore{}

// Snapshot tries the advisory lock on a held connection, then delegates.
func (s ExclusiveStore) Snapshot(ctx context.Context) (ops.Snapshot, error) {
	if ctx == nil || s.Inner == nil || s.Pool == nil {
		return ops.Snapshot{}, errors.New("ops: exclusive store requires context, inner store and pool")
	}
	key := s.Key
	if key == 0 {
		key = HealthLockKey
	}
	conn, err := s.Pool.Acquire(ctx)
	if err != nil {
		return ops.Snapshot{}, fmt.Errorf("ops: acquire health lock connection: %w", err)
	}
	defer conn.Release()
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&locked); err != nil {
		return ops.Snapshot{}, fmt.Errorf("ops: try health advisory lock: %w", err)
	}
	if !locked {
		return ops.Snapshot{}, ops.ErrSnapshotBusy
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, key)
	}()
	return s.Inner.Snapshot(ctx)
}
