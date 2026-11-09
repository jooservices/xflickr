package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jooservices/xflickr/internal/platform/ops"
)

func TestExclusiveStoreRejectsMissingDependencies(t *testing.T) {
	if _, err := (ExclusiveStore{}).Snapshot(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if _, err := (ExclusiveStore{Inner: fakeInner{}, Pool: nil}).Snapshot(context.Background()); err == nil {
		t.Fatal("expected error for missing pool")
	}
	if _, err := (ExclusiveStore{Inner: fakeInner{}}).Snapshot(nil); err == nil {
		t.Fatal("expected error for missing context")
	}
}

func TestExclusiveStoreSkipsWhenLockHeld(t *testing.T) {
	pool := &fakeLockPool{conn: &fakeLockConn{locked: false}}
	_, err := (ExclusiveStore{Inner: fakeInner{}, Pool: pool}).Snapshot(context.Background())
	if !errors.Is(err, ops.ErrSnapshotBusy) {
		t.Fatalf("err=%v", err)
	}
	if !pool.conn.released {
		t.Fatal("connection must be released when lock is busy")
	}
}

func TestExclusiveStoreRunsWhenLockAcquired(t *testing.T) {
	pool := &fakeLockPool{conn: &fakeLockConn{locked: true}}
	snap, err := (ExclusiveStore{Inner: fakeInner{}, Pool: pool, Key: 42}).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !snap.CountsEstimated || !pool.conn.unlocked || !pool.conn.released {
		t.Fatalf("snap=%+v unlocked=%v released=%v", snap, pool.conn.unlocked, pool.conn.released)
	}
}

func TestExclusiveStoreAcquireAndLockErrors(t *testing.T) {
	boom := errors.New("acquire down")
	if _, err := (ExclusiveStore{Inner: fakeInner{}, Pool: &fakeLockPool{err: boom}}).Snapshot(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("acquire err=%v", err)
	}
	lockErr := errors.New("lock query down")
	if _, err := (ExclusiveStore{Inner: fakeInner{}, Pool: &fakeLockPool{conn: &fakeLockConn{queryErr: lockErr}}}).Snapshot(context.Background()); !errors.Is(err, lockErr) {
		t.Fatalf("lock err=%v", err)
	}
}

func TestPoolAdapterRequiresPool(t *testing.T) {
	if _, err := (PoolAdapter{}).Acquire(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadHealthSettingsDefaults(t *testing.T) {
	got, err := LoadHealthSettings(context.Background(), &snapshotDB{})
	if err != nil {
		t.Fatal(err)
	}
	want := DefaultHealthSettings()
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
	if _, err := LoadHealthSettings(nil, &snapshotDB{}); err == nil {
		t.Fatal("expected nil context error")
	}
	if clampDuration(0, 10*time.Second, time.Minute) != 10*time.Second {
		t.Fatal("clamp min")
	}
	if clampDuration(2*time.Minute, 10*time.Second, time.Minute) != time.Minute {
		t.Fatal("clamp max")
	}
}

type fakeInner struct{ err error }

func (f fakeInner) Snapshot(context.Context) (ops.Snapshot, error) {
	if f.err != nil {
		return ops.Snapshot{}, f.err
	}
	return ops.Snapshot{CountsEstimated: true}, nil
}

type fakeLockPool struct {
	conn *fakeLockConn
	err  error
}

func (p *fakeLockPool) Acquire(context.Context) (LockConn, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.conn, nil
}

type fakeLockConn struct {
	locked   bool
	queryErr error
	unlocked bool
	released bool
}

func (c *fakeLockConn) QueryRow(context.Context, string, ...any) pgx.Row {
	if c.queryErr != nil {
		return snapshotRow{err: c.queryErr}
	}
	return snapshotRow{values: []any{c.locked}}
}

func (c *fakeLockConn) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	c.unlocked = true
	return pgconn.CommandTag{}, nil
}

func (c *fakeLockConn) Release() { c.released = true }
