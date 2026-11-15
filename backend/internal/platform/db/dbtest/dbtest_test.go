package dbtest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/config"
)

func TestOpenMigrationCallbackReportsInvalidConfiguration(t *testing.T) {
	cfg := config.DBConfig{Name: "wt_jooxf522", SSLMode: "not-a-ssl-mode"}
	if err := migrateTestDatabase(context.Background(), cfg); err == nil {
		t.Fatal("migrateTestDatabase() succeeded with invalid SSL mode")
	}
}

func TestIsolatedSchemaRegistersCleanup(t *testing.T) {
	tb := &recordingTB{}
	first := &fakeSchemaPool{}
	second := &fakeSchemaPool{}
	pools := []*fakeSchemaPool{first, second}
	opened := 0
	scoped := isolatedSchema(tb, config.DBConfig{Name: "wt_jooxf522"}, bytes.NewReader(make([]byte, 8)), func(context.Context, config.DBConfig) (schemaPool, error) {
		pool := pools[opened]
		opened++
		return pool, nil
	})
	if scoped.SearchPath != "test_0000000000000000,public" || len(tb.cleanups) != 1 {
		t.Fatalf("IsolatedSchema() = %#v, registered cleanups = %d", scoped, len(tb.cleanups))
	}
	tb.cleanups[0]()
	if !second.closed || len(tb.errors) != 0 {
		t.Fatalf("cleanup pool closed = %t, errors = %v", second.closed, tb.errors)
	}
}

func TestIsolatedSchemaReportsCleanupFailureToTestingTB(t *testing.T) {
	wantErr := errors.New("cleanup failed")
	tb := &recordingTB{}
	registerSchemaCleanup(tb, func() error { return wantErr })
	if len(tb.cleanups) != 1 {
		t.Fatalf("registered cleanups = %d, want 1", len(tb.cleanups))
	}
	tb.cleanups[0]()
	if len(tb.errors) != 1 || !strings.Contains(tb.errors[0], wantErr.Error()) {
		t.Fatalf("cleanup errors = %v, want %q", tb.errors, wantErr)
	}
}

func TestDatabaseSchemaPoolReturnsConfigurationError(t *testing.T) {
	_, err := databaseSchemaPool(context.Background(), config.DBConfig{Name: "wt_jooxf522"})
	if err == nil || !strings.Contains(err.Error(), "DB_HOST") {
		t.Fatalf("databaseSchemaPool() error = %v, want DB_HOST validation error", err)
	}
}

func TestOpenPoolMigratesOnlyAnIsolatedDatabase(t *testing.T) {
	cfg := config.DBConfig{Name: "wt_jooxf522"}
	var created, migrated bool
	pool, err := openPool(context.Background(), cfg, func(_ context.Context, got config.DBConfig) (*pgxpool.Pool, error) {
		created = true
		if got.Name != cfg.Name {
			t.Fatalf("pool factory got database %q", got.Name)
		}
		return newUnconnectedPool(t)
	}, func(_ context.Context, got config.DBConfig) error {
		migrated = true
		if got.Name != cfg.Name {
			t.Fatalf("migrator got database %q", got.Name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("openPool() error = %v", err)
	}
	if !created || !migrated {
		t.Fatalf("openPool() called create=%t migrate=%t", created, migrated)
	}
	pool.Close()
}

func TestOpenPoolWrapsPoolAndMigrationFailures(t *testing.T) {
	wantErr := errors.New("unavailable")
	t.Run("pool factory", func(t *testing.T) {
		_, err := openPool(context.Background(), config.DBConfig{Name: "wt_jooxf522"}, func(context.Context, config.DBConfig) (*pgxpool.Pool, error) {
			return nil, wantErr
		}, func(context.Context, config.DBConfig) error { return nil })
		if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "open test PostgreSQL pool") {
			t.Fatalf("openPool() error = %v", err)
		}
	})

	t.Run("migration", func(t *testing.T) {
		pool, err := newUnconnectedPool(t)
		if err != nil {
			t.Fatalf("newUnconnectedPool() error = %v", err)
		}
		_, err = openPool(context.Background(), config.DBConfig{Name: "wt_jooxf522"}, func(context.Context, config.DBConfig) (*pgxpool.Pool, error) {
			return pool, nil
		}, func(context.Context, config.DBConfig) error { return wantErr })
		if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "migrate test PostgreSQL database") {
			t.Fatalf("openPool() error = %v", err)
		}
	})
}

func TestOpenMigratedClosesPoolWhenSubtestEnds(t *testing.T) {
	var pool *pgxpool.Pool
	t.Run("helper", func(t *testing.T) {
		pool = openMigrated(t, config.DBConfig{Name: "wt_jooxf522"}, func(context.Context, config.DBConfig) (*pgxpool.Pool, error) {
			return newUnconnectedPool(t)
		}, func(context.Context, config.DBConfig) error { return nil })
	})
	if pool == nil {
		t.Fatal("openMigrated() returned a nil pool")
	}
}

func TestCreateIsolatedSchemaCreatesScopedSchemaAndCleanup(t *testing.T) {
	first := &fakeSchemaPool{}
	second := &fakeSchemaPool{}
	pools := []*fakeSchemaPool{first, second}
	opened := 0
	cfg, cleanup, err := createIsolatedSchema(
		config.DBConfig{Name: "wt_jooxf522"},
		bytes.NewReader([]byte{1, 2, 3, 4, 5, 6, 7, 8}),
		func(_ context.Context, got config.DBConfig) (schemaPool, error) {
			if got.Name != "wt_jooxf522" || got.SearchPath != "" {
				t.Fatalf("schema pool factory got config %+v", got)
			}
			pool := pools[opened]
			opened++
			return pool, nil
		},
	)
	if err != nil {
		t.Fatalf("createIsolatedSchema() error = %v", err)
	}
	if cfg.SearchPath != "test_0102030405060708,public" {
		t.Fatalf("SearchPath = %q", cfg.SearchPath)
	}
	if len(first.statements) != 1 || first.statements[0] != `CREATE SCHEMA "test_0102030405060708"` || !first.closed {
		t.Fatalf("create pool statements = %v, closed = %t", first.statements, first.closed)
	}
	if err := cleanup(); err != nil {
		t.Fatalf("cleanup() error = %v", err)
	}
	if len(second.statements) != 1 || second.statements[0] != `DROP SCHEMA IF EXISTS "test_0102030405060708" CASCADE` || !second.closed {
		t.Fatalf("cleanup pool statements = %v, closed = %t", second.statements, second.closed)
	}
}

func TestCreateIsolatedSchemaReportsRandomAndOpenFailures(t *testing.T) {
	wantErr := errors.New("unavailable")
	t.Run("random reader", func(t *testing.T) {
		_, _, err := createIsolatedSchema(config.DBConfig{Name: "wt_jooxf522"}, bytes.NewReader(nil), func(context.Context, config.DBConfig) (schemaPool, error) {
			t.Fatal("pool factory called after random reader failed")
			return nil, nil
		})
		if err == nil || !strings.Contains(err.Error(), "generate isolated PostgreSQL schema name") {
			t.Fatalf("createIsolatedSchema() error = %v", err)
		}
	})
	t.Run("database pool", func(t *testing.T) {
		_, _, err := createIsolatedSchema(config.DBConfig{Name: "wt_jooxf522"}, bytes.NewReader(make([]byte, 8)), func(context.Context, config.DBConfig) (schemaPool, error) {
			return nil, wantErr
		})
		if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "open worktree database") {
			t.Fatalf("createIsolatedSchema() error = %v", err)
		}
	})
}

func TestCreateIsolatedSchemaClosesPoolAfterCreateFailure(t *testing.T) {
	wantErr := errors.New("create failed")
	pool := &fakeSchemaPool{execErr: wantErr}
	_, _, err := createIsolatedSchema(config.DBConfig{Name: "wt_jooxf522"}, bytes.NewReader(make([]byte, 8)), func(context.Context, config.DBConfig) (schemaPool, error) {
		return pool, nil
	})
	if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "create isolated PostgreSQL schema") || !pool.closed {
		t.Fatalf("createIsolatedSchema() error = %v, pool closed = %t", err, pool.closed)
	}
}

func TestIsolatedSchemaCleanupReportsOpenAndDropFailures(t *testing.T) {
	wantErr := errors.New("cleanup failed")
	t.Run("database pool", func(t *testing.T) {
		opened := 0
		_, cleanup, err := createIsolatedSchema(config.DBConfig{Name: "wt_jooxf522"}, bytes.NewReader(make([]byte, 8)), func(context.Context, config.DBConfig) (schemaPool, error) {
			opened++
			if opened == 2 {
				return nil, wantErr
			}
			return &fakeSchemaPool{}, nil
		})
		if err != nil {
			t.Fatalf("createIsolatedSchema() error = %v", err)
		}
		if err := cleanup(); !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "open worktree database to remove isolated schema") {
			t.Fatalf("cleanup() error = %v", err)
		}
	})
	t.Run("drop schema", func(t *testing.T) {
		opened := 0
		cleanupPool := &fakeSchemaPool{execErr: wantErr}
		_, cleanup, err := createIsolatedSchema(config.DBConfig{Name: "wt_jooxf522"}, bytes.NewReader(make([]byte, 8)), func(context.Context, config.DBConfig) (schemaPool, error) {
			opened++
			if opened == 2 {
				return cleanupPool, nil
			}
			return &fakeSchemaPool{}, nil
		})
		if err != nil {
			t.Fatalf("createIsolatedSchema() error = %v", err)
		}
		if err := cleanup(); !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "remove isolated PostgreSQL schema") || !cleanupPool.closed {
			t.Fatalf("cleanup() error = %v, pool closed = %t", err, cleanupPool.closed)
		}
	})
}

type fakeSchemaPool struct {
	statements []string
	execErr    error
	closed     bool
}

func (pool *fakeSchemaPool) Exec(_ context.Context, statement string, _ ...any) (pgconn.CommandTag, error) {
	pool.statements = append(pool.statements, statement)
	if pool.execErr != nil {
		return pgconn.CommandTag{}, pool.execErr
	}
	return pgconn.NewCommandTag("OK"), nil
}

func (pool *fakeSchemaPool) Close() {
	pool.closed = true
}

func newUnconnectedPool(t *testing.T) (*pgxpool.Pool, error) {
	t.Helper()
	config, err := pgxpool.ParseConfig("postgres://user:password@127.0.0.1:1/wt_test?sslmode=disable&connect_timeout=1")
	if err != nil {
		return nil, err
	}
	return pgxpool.NewWithConfig(t.Context(), config)
}

type recordingTB struct {
	testing.TB
	cleanups []func()
	errors   []string
}

func (tb *recordingTB) Helper() {}

func (tb *recordingTB) Fatal(...any) {
	panic(expectedFatal{})
}

func (tb *recordingTB) Fatalf(string, ...any) {
	panic(expectedFatal{})
}

func (tb *recordingTB) Cleanup(cleanup func()) {
	tb.cleanups = append(tb.cleanups, cleanup)
}

func (tb *recordingTB) Error(args ...any) {
	tb.errors = append(tb.errors, fmt.Sprint(args...))
}

type expectedFatal struct{}

func assertFatal(t *testing.T, action func()) {
	t.Helper()
	defer func() {
		if _, ok := recover().(expectedFatal); !ok {
			t.Fatal("expected testing.TB.Fatal or Fatalf")
		}
	}()
	action()
}

func TestOpenFailsTheTestWhenTheConfiguredDatabaseCannotBeUsed(t *testing.T) {
	assertFatal(t, func() {
		Open(&recordingTB{}, config.DBConfig{SSLMode: "not-a-ssl-mode"})
	})
}

func TestIsolatedSchemaFailsTheTestWhenTheConfiguredDatabaseCannotBeUsed(t *testing.T) {
	assertFatal(t, func() {
		IsolatedSchema(&recordingTB{}, config.DBConfig{SSLMode: "not-a-ssl-mode"})
	})
}
