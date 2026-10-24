// Package dbtest provides guarded access to XFlickr's isolated test databases.
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db"
)

const isolatedDatabaseAttempts = 5

var worktreeDatabaseName = regexp.MustCompile(`^xflickr_wt[a-z0-9]{1,12}$`)

// Open returns a migrated pool after refusing live or unrelated databases.
// Worktree tests use their own xflickr_wt<id> database; shared test runs may
// use xflickr_test.
func Open(t testing.TB, cfg config.DBConfig) *pgxpool.Pool {
	t.Helper()
	return openMigrated(t, cfg, db.NewPool, func(ctx context.Context, cfg config.DBConfig) error {
		return db.Migrate(ctx, cfg, db.MigrateUp, nil)
	})
}

func openMigrated(t testing.TB, cfg config.DBConfig, createPool poolFactory, migrate migrationFunc) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := openPool(ctx, cfg, createPool, migrate)
	if err != nil {
		t.Fatalf("open migrated test PostgreSQL database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type poolFactory func(context.Context, config.DBConfig) (*pgxpool.Pool, error)
type migrationFunc func(context.Context, config.DBConfig) error

func openPool(ctx context.Context, cfg config.DBConfig, createPool poolFactory, migrate migrationFunc) (*pgxpool.Pool, error) {
	if err := ValidateDatabaseName(cfg.Name); err != nil {
		return nil, err
	}

	pool, err := createPool(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open test PostgreSQL pool: %w", err)
	}
	if err := migrate(ctx, cfg); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate test PostgreSQL database: %w", err)
	}
	return pool, nil
}

// ValidateDatabaseName permits only XFlickr's shared test database or a
// worktree-specific database, preventing tests from connecting to xflickr.
func ValidateDatabaseName(name string) error {
	if name == "xflickr_test" || worktreeDatabaseName.MatchString(name) {
		return nil
	}
	return fmt.Errorf("refusing database %q: tests require xflickr_test or xflickr_wt<id>", name)
}

// EmptyDatabase creates an empty database on the same PostgreSQL server as cfg
// and drops it when the test ends. Tests that roll schema back to version zero
// use it so they do not drop tables other packages still need in the shared
// worktree database.
func EmptyDatabase(t testing.TB, cfg config.DBConfig) config.DBConfig {
	t.Helper()
	return emptyDatabase(t, cfg, openDatabaseAdmin, randomWorktreeDatabaseName)
}

func emptyDatabase(t testing.TB, cfg config.DBConfig, open adminOpener, nextName databaseNamer) config.DBConfig {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	isolated, cleanup, err := provisionEmptyDatabase(ctx, cfg, open, nextName)
	if err != nil {
		t.Fatalf("create isolated test database: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer dropCancel()
		if err := cleanup(dropCtx); err != nil {
			t.Errorf("drop isolated test database: %v", err)
		}
	})
	return isolated
}

type databaseAdmin interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Close()
}

type adminOpener func(context.Context, config.DBConfig) (databaseAdmin, error)
type databaseNamer func(excluded string) (string, error)

func openDatabaseAdmin(ctx context.Context, cfg config.DBConfig) (databaseAdmin, error) {
	pool, err := db.NewPool(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return pool, nil
}

func provisionEmptyDatabase(ctx context.Context, cfg config.DBConfig, open adminOpener, nextName databaseNamer) (config.DBConfig, func(context.Context) error, error) {
	if err := ValidateDatabaseName(cfg.Name); err != nil {
		return config.DBConfig{}, nil, err
	}

	admin, err := open(ctx, cfg)
	if err != nil {
		return config.DBConfig{}, nil, fmt.Errorf("open database maintenance connection: %w", err)
	}
	defer admin.Close()

	var created string
	var createErr error
	for range isolatedDatabaseAttempts {
		name, err := nextName(cfg.Name)
		if err != nil {
			return config.DBConfig{}, nil, err
		}
		if err := ValidateDatabaseName(name); err != nil {
			return config.DBConfig{}, nil, fmt.Errorf("isolated test database name: %w", err)
		}
		if name == cfg.Name {
			return config.DBConfig{}, nil, errors.New("isolated test database name matches the source database")
		}

		_, createErr = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
		if createErr == nil {
			created = name
			break
		}
		if !isDuplicateDatabase(createErr) {
			return config.DBConfig{}, nil, fmt.Errorf("create isolated test database: %w", createErr)
		}
	}
	if created == "" {
		return config.DBConfig{}, nil, fmt.Errorf("create isolated test database: %w", createErr)
	}

	isolated := cfg
	isolated.Name = created
	cleanup := func(ctx context.Context) error {
		admin, err := open(ctx, cfg)
		if err != nil {
			return fmt.Errorf("open database maintenance connection: %w", err)
		}
		defer admin.Close()
		_, err = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{created}.Sanitize()+" WITH (FORCE)")
		if err != nil {
			return fmt.Errorf("drop isolated test database: %w", err)
		}
		return nil
	}
	return isolated, cleanup, nil
}

func randomWorktreeDatabaseName(excluded string) (string, error) {
	return newRandomDatabaseName(excluded, rand.Reader)
}

func newRandomDatabaseName(excluded string, reader io.Reader) (string, error) {
	for range isolatedDatabaseAttempts {
		var raw [4]byte
		if _, err := io.ReadFull(reader, raw[:]); err != nil {
			return "", fmt.Errorf("generate isolated test database name: %w", err)
		}
		// "t" marks a per-test database. The suffix stays inside the 12
		// character worktree-name limit enforced by ValidateDatabaseName.
		name := "xflickr_wtt" + hex.EncodeToString(raw[:])
		if name != excluded {
			return name, nil
		}
	}
	return "", errors.New("generate isolated test database name: exhausted attempts")
}

func isDuplicateDatabase(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42P04"
}
