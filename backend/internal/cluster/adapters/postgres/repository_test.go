package postgres

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/jooservices/xflickr/internal/cluster/app"
	"github.com/jooservices/xflickr/internal/cluster/domain"
)

func TestRepositoryCreate(t *testing.T) {
	want := sampleNode()
	pool := &fakeNodePool{row: fakeNodeRow{node: want}}
	repository := Repository{pool: pool}
	hash := []byte("stored-hash")
	got, err := repository.Create(t.Context(), domain.Node{Name: want.Name, Roles: want.Roles}, hash)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != want.Name || got.EgressIP != want.EgressIP || len(pool.args) != 4 || pool.args[0] != want.Name || !bytes.Equal(pool.args[3].([]byte), hash) {
		t.Fatalf("created node = %+v, args = %#v", got, pool.args)
	}
}

func TestRepositoryCreateErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want error
	}{
		{name: "duplicate", err: &pgconn.PgError{Code: "23505"}, want: app.ErrNodeExists},
		{name: "storage", err: errors.New("database unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := Repository{pool: &fakeNodePool{row: fakeNodeRow{err: test.err}}}
			_, err := repository.Create(t.Context(), domain.Node{Name: "node-1"}, nil)
			if test.want != nil {
				if !errors.Is(err, test.want) {
					t.Fatalf("Create() error = %v, want %v", err, test.want)
				}
			} else if err == nil || !strings.Contains(err.Error(), "database unavailable") {
				t.Fatalf("Create() error = %v", err)
			}
		})
	}
}

func TestRepositoryAuthenticateToken(t *testing.T) {
	want := sampleNode()
	for _, test := range []struct {
		name string
		err  error
		want error
	}{
		{name: "authenticated"},
		{name: "unknown token", err: pgx.ErrNoRows, want: app.ErrInvalidToken},
		{name: "storage", err: errors.New("database unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			pool := &fakeNodePool{row: fakeNodeRow{node: want, err: test.err}}
			repository := Repository{pool: pool}
			got, err := repository.AuthenticateToken(t.Context(), []byte("hash"))
			if test.want != nil {
				if !errors.Is(err, test.want) {
					t.Fatalf("AuthenticateToken() error = %v, want %v", err, test.want)
				}
				return
			}
			if test.err != nil {
				if err == nil || !strings.Contains(err.Error(), "database unavailable") {
					t.Fatalf("AuthenticateToken() error = %v", err)
				}
				return
			}
			if err != nil || got.Name != want.Name || len(pool.args) != 1 || string(pool.args[0].([]byte)) != "hash" {
				t.Fatalf("AuthenticateToken() = %+v, %v; args = %#v", got, err, pool.args)
			}
		})
	}
}

func TestRepositoryHeartbeat(t *testing.T) {
	want := sampleNode()
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	want.LastHeartbeatAt = &at
	input := app.HeartbeatInput{Version: "v1", EgressIP: "192.0.2.5", Metrics: []byte(`{"load":0.25}`)}
	pool := &fakeNodePool{row: fakeNodeRow{node: want}}
	repository := Repository{pool: pool}
	got, err := repository.Heartbeat(t.Context(), want.Name, input, at)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != want.Name || got.LastHeartbeatAt == nil || !got.LastHeartbeatAt.Equal(at) || len(pool.args) != 5 || pool.args[0] != want.Name || pool.args[1] != input.Version || pool.args[2] != input.EgressIP || pool.args[3] != string(input.Metrics) {
		t.Fatalf("Heartbeat() = %+v, args = %#v", got, pool.args)
	}
	for _, test := range []struct {
		name string
		err  error
		want error
	}{
		{name: "node missing", err: pgx.ErrNoRows, want: app.ErrInvalidToken},
		{name: "storage", err: errors.New("database unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := Repository{pool: &fakeNodePool{row: fakeNodeRow{err: test.err}}}
			_, err := repository.Heartbeat(t.Context(), want.Name, input, at)
			if test.want != nil {
				if !errors.Is(err, test.want) {
					t.Fatalf("Heartbeat() error = %v, want %v", err, test.want)
				}
			} else if err == nil || !strings.Contains(err.Error(), "database unavailable") {
				t.Fatalf("Heartbeat() error = %v", err)
			}
		})
	}
}

func TestScanNodePropagatesScanError(t *testing.T) {
	want := errors.New("bad row")
	if _, err := scanNode(fakeNodeRow{err: want}); !errors.Is(err, want) {
		t.Fatalf("scanNode() error = %v", err)
	}
}

func TestAssignmentRepositoryInitial(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 123456000, time.UTC)
	want := app.Assignment{KeyID: 7, Node: "node-1", Since: now, Reason: "initial"}

	t.Run("stores and reads assignment in one transaction", func(t *testing.T) {
		tx := &fakeAssignmentTx{row: fakeAssignmentRow{assignment: want}}
		pool := &fakeAssignmentPool{tx: tx}
		got, err := (&Repository{pool: pool}).AssignInitial(t.Context(), want.KeyID, want.Node, now)
		if err != nil {
			t.Fatal(err)
		}
		if got != want || !tx.committed || tx.rolledBack || len(tx.args) != 4 {
			t.Fatalf("AssignInitial() = %+v, transaction = %+v", got, tx)
		}
		if tx.args[0] != want.KeyID || tx.args[1] != want.Node || tx.args[2] != now || !tx.args[3].(time.Time).Equal(now.Add(-domain.WarningAfter)) {
			t.Fatalf("insert arguments = %#v", tx.args)
		}
	})

	t.Run("begin error", func(t *testing.T) {
		wantErr := errors.New("begin failed")
		_, err := (&Repository{pool: &fakeAssignmentPool{beginErr: wantErr}}).AssignInitial(t.Context(), want.KeyID, want.Node, now)
		if !errors.Is(err, wantErr) {
			t.Fatalf("AssignInitial() error = %v", err)
		}
	})

	t.Run("unsupported transaction pool", func(t *testing.T) {
		_, err := (&Repository{pool: &fakeNodePool{}}).AssignInitial(t.Context(), want.KeyID, want.Node, now)
		if err == nil || !strings.Contains(err.Error(), "cluster: assignment transaction is unavailable") {
			t.Fatalf("AssignInitial() error = %v", err)
		}
	})

	t.Run("insert error rolls back", func(t *testing.T) {
		wantErr := errors.New("insert failed")
		tx := &fakeAssignmentTx{execErr: wantErr}
		_, err := (&Repository{pool: &fakeAssignmentPool{tx: tx}}).AssignInitial(t.Context(), want.KeyID, want.Node, now)
		if !errors.Is(err, wantErr) || !tx.rolledBack || tx.committed {
			t.Fatalf("AssignInitial() error = %v, transaction = %+v", err, tx)
		}
	})

	t.Run("unavailable row rolls back", func(t *testing.T) {
		tx := &fakeAssignmentTx{row: fakeAssignmentRow{err: pgx.ErrNoRows}}
		_, err := (&Repository{pool: &fakeAssignmentPool{tx: tx}}).AssignInitial(t.Context(), want.KeyID, want.Node, now)
		if !errors.Is(err, app.ErrAssignmentUnavailable) || !tx.rolledBack {
			t.Fatalf("AssignInitial() error = %v, transaction = %+v", err, tx)
		}
	})

	t.Run("read error rolls back", func(t *testing.T) {
		wantErr := errors.New("read failed")
		tx := &fakeAssignmentTx{row: fakeAssignmentRow{err: wantErr}}
		_, err := (&Repository{pool: &fakeAssignmentPool{tx: tx}}).AssignInitial(t.Context(), want.KeyID, want.Node, now)
		if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "cluster: read key assignment") || !tx.rolledBack {
			t.Fatalf("AssignInitial() error = %v, transaction = %+v", err, tx)
		}
	})

	t.Run("rollback error is preserved", func(t *testing.T) {
		readErr := errors.New("read failed")
		rollbackErr := errors.New("rollback failed")
		tx := &fakeAssignmentTx{row: fakeAssignmentRow{err: readErr}, rollbackErr: rollbackErr}
		_, err := (&Repository{pool: &fakeAssignmentPool{tx: tx}}).AssignInitial(t.Context(), want.KeyID, want.Node, now)
		if !errors.Is(err, readErr) || !errors.Is(err, rollbackErr) {
			t.Fatalf("AssignInitial() error = %v", err)
		}
	})

	t.Run("commit error", func(t *testing.T) {
		wantErr := errors.New("commit failed")
		tx := &fakeAssignmentTx{row: fakeAssignmentRow{assignment: want}, commitErr: wantErr}
		_, err := (&Repository{pool: &fakeAssignmentPool{tx: tx}}).AssignInitial(t.Context(), want.KeyID, want.Node, now)
		if !errors.Is(err, wantErr) {
			t.Fatalf("AssignInitial() error = %v", err)
		}
	})
}

func TestAssignmentRepositoryGet(t *testing.T) {
	want := app.Assignment{KeyID: 7, Node: "node-1", Since: time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC), Reason: "initial"}
	for _, test := range []struct {
		name string
		row  pgx.Row
		want error
	}{
		{name: "found", row: fakeAssignmentRow{assignment: want}},
		{name: "missing", row: fakeAssignmentRow{err: pgx.ErrNoRows}, want: app.ErrAssignmentUnavailable},
		{name: "scan error", row: fakeAssignmentRow{err: errors.New("bad row")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			pool := &fakeNodePool{row: test.row}
			got, err := (&Repository{pool: pool}).GetAssignment(t.Context(), want.KeyID)
			if test.want != nil {
				if !errors.Is(err, test.want) {
					t.Fatalf("GetAssignment() error = %v, want %v", err, test.want)
				}
				return
			}
			if test.name == "found" && (err != nil || got != want) {
				t.Fatalf("GetAssignment() = %+v, %v", got, err)
			}
			if test.name == "scan error" && (err == nil || !strings.Contains(err.Error(), "cluster: read key assignment")) {
				t.Fatalf("GetAssignment() error = %v", err)
			}
		})
	}
}

type fakeAssignmentPool struct {
	*fakeNodePool
	tx       *fakeAssignmentTx
	beginErr error
}

func (pool *fakeAssignmentPool) Begin(context.Context) (pgx.Tx, error) {
	return pool.tx, pool.beginErr
}

type fakeAssignmentTx struct {
	pgx.Tx
	row         pgx.Row
	execErr     error
	commitErr   error
	rollbackErr error
	args        []any
	committed   bool
	rolledBack  bool
}

func (tx *fakeAssignmentTx) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	tx.args = args
	return pgconn.CommandTag{}, tx.execErr
}

func (tx *fakeAssignmentTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return tx.row
}

func (tx *fakeAssignmentTx) Commit(context.Context) error {
	tx.committed = true
	return tx.commitErr
}

func (tx *fakeAssignmentTx) Rollback(context.Context) error {
	tx.rolledBack = true
	return tx.rollbackErr
}

type fakeAssignmentRow struct {
	assignment app.Assignment
	err        error
}

func (row fakeAssignmentRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(dest) != 4 {
		return errors.New("unexpected assignment scan shape")
	}
	*dest[0].(*int64) = row.assignment.KeyID
	*dest[1].(*string) = row.assignment.Node
	*dest[2].(*time.Time) = row.assignment.Since
	*dest[3].(*string) = row.assignment.Reason
	return nil
}

type fakeNodePool struct {
	row  pgx.Row
	args []any
}

func (pool *fakeNodePool) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (pool *fakeNodePool) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	pool.args = args
	return pool.row
}

type fakeNodeRow struct {
	node domain.Node
	err  error
}

func (row fakeNodeRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(dest) != 9 {
		return errors.New("unexpected scan shape")
	}
	*dest[0].(*string) = row.node.Name
	*dest[1].(*[]string) = append([]string(nil), row.node.Roles...)
	*dest[2].(*string) = row.node.Region
	*dest[3].(*string) = row.node.Version
	*dest[4].(*string) = row.node.EgressIP
	*dest[5].(*domain.State) = row.node.State
	if row.node.CoolingUntil != nil {
		*dest[6].(*pgtype.Timestamptz) = pgtype.Timestamptz{Time: *row.node.CoolingUntil, Valid: true}
	}
	if row.node.LastHeartbeatAt != nil {
		*dest[7].(*pgtype.Timestamptz) = pgtype.Timestamptz{Time: *row.node.LastHeartbeatAt, Valid: true}
	}
	*dest[8].(*string) = string(row.node.Metrics)
	return nil
}

func sampleNode() domain.Node {
	at := time.Date(2026, 10, 28, 11, 59, 0, 0, time.UTC)
	return domain.Node{Name: "node-1", Roles: []string{"crawl"}, Region: "west", Version: "v1", EgressIP: "192.0.2.5", State: domain.StateOnline, LastHeartbeatAt: &at, Metrics: []byte(`{"load":0.25}`)}
}
