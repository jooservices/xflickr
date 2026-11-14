// Package dbtest provides guarded access to XFlickr's isolated test databases.
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

var worktreeDatabaseName = regexp.MustCompile(`^wt_[a-z0-9]{1,24}$`)

// Open returns a migrated pool after refusing live or unrelated databases.
func Open(t testing.TB, cfg config.DBConfig) *pgxpool.Pool {
	t.Helper()
	return openMigrated(t, cfg, db.NewPool, migrateTestDatabase)
}

func migrateTestDatabase(ctx context.Context, cfg config.DBConfig) error {
	return db.Migrate(ctx, cfg, db.MigrateUp, nil)
}

// IsolatedSchema creates a unique schema inside the worktree database and
// returns a config scoped to it. This keeps integration tests isolated without
// requiring the tree's database role to create databases.
func IsolatedSchema(t testing.TB, cfg config.DBConfig) config.DBConfig {
	t.Helper()
	return isolatedSchema(t, cfg, rand.Reader, databaseSchemaPool)
}

func isolatedSchema(t testing.TB, cfg config.DBConfig, random io.Reader, createPool schemaPoolFactory) config.DBConfig {
	t.Helper()
	scoped, cleanup, err := createIsolatedSchema(cfg, random, createPool)
	if err != nil {
		t.Fatal(err)
	}
	registerSchemaCleanup(t, cleanup)
	return scoped
}

func registerSchemaCleanup(t testing.TB, cleanup func() error) {
	t.Cleanup(func() {
		if err := cleanup(); err != nil {
			t.Error(err)
		}
	})
}

func databaseSchemaPool(ctx context.Context, cfg config.DBConfig) (schemaPool, error) {
	return db.NewPool(ctx, cfg)
}

type (
	schemaPool interface {
		Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
		Close()
	}
	schemaPoolFactory func(context.Context, config.DBConfig) (schemaPool, error)
)

func createIsolatedSchema(cfg config.DBConfig, random io.Reader, createPool schemaPoolFactory) (config.DBConfig, func() error, error) {
	if err := ValidateDatabaseName(cfg.Name); err != nil {
		return config.DBConfig{}, nil, err
	}
	databaseConfig := cfg
	var suffix [8]byte
	if _, err := io.ReadFull(random, suffix[:]); err != nil {
		return config.DBConfig{}, nil, fmt.Errorf("generate isolated PostgreSQL schema name: %w", err)
	}
	schema := "test_" + hex.EncodeToString(suffix[:])

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := createPool(ctx, cfg)
	if err != nil {
		return config.DBConfig{}, nil, fmt.Errorf("open worktree database for isolated schema: %w", err)
	}
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		pool.Close()
		return config.DBConfig{}, nil, fmt.Errorf("create isolated PostgreSQL schema: %w", err)
	}
	pool.Close()

	cleanup := func() error {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		cleanupPool, err := createPool(cleanupCtx, databaseConfig)
		if err != nil {
			return fmt.Errorf("open worktree database to remove isolated schema: %w", err)
		}
		defer cleanupPool.Close()
		if _, err := cleanupPool.Exec(cleanupCtx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			return fmt.Errorf("remove isolated PostgreSQL schema: %w", err)
		}
		return nil
	}

	cfg.SearchPath = schema + ",public"
	return cfg, cleanup, nil
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

type (
	poolFactory   func(context.Context, config.DBConfig) (*pgxpool.Pool, error)
	migrationFunc func(context.Context, config.DBConfig) error
)

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

// ValidateDatabaseName permits a database created for an isolated test tree.
func ValidateDatabaseName(name string) error {
	if worktreeDatabaseName.MatchString(name) {
		return nil
	}
	return fmt.Errorf("refusing database %q: tests require a wt_<ticket-id> database", name)
}
