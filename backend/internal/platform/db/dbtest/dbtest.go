// Package dbtest provides guarded access to XFlickr's isolated test databases.
package dbtest

import (
	"context"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db"
)

var worktreeDatabaseName = regexp.MustCompile(`^xflickr_wt[a-z0-9]{1,12}$`)

// Open returns a migrated pool after refusing live or unrelated databases.
// Worktree tests use their own xflickr_wt<id> database; shared test runs may
// use xflickr_test.
func Open(t testing.TB, cfg config.DBConfig) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := openPool(ctx, cfg, db.NewPool, func(ctx context.Context, cfg config.DBConfig) error {
		return db.Migrate(ctx, cfg, db.MigrateUp, nil)
	})
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
