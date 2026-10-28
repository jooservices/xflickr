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
