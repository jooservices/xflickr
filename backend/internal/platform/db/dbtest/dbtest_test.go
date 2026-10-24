package dbtest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jooservices/xflickr/internal/platform/config"
)

func TestValidateDatabaseName(t *testing.T) {
	tests := []struct {
		name    string
		allowed bool
	}{
		{name: "xflickr_test", allowed: true},
		{name: "xflickr_wtk80", allowed: true},
		{name: "xflickr_wtfeature1", allowed: true},
		{name: "xflickr", allowed: false},
		{name: "other_test", allowed: false},
		{name: "xflickr_wt", allowed: false},
		{name: "xflickr_wtwith-dash", allowed: false},
		{name: "", allowed: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateDatabaseName(test.name)
			if test.allowed && err != nil {
				t.Fatalf("ValidateDatabaseName(%q) error = %v", test.name, err)
			}
			if !test.allowed && err == nil {
				t.Fatalf("ValidateDatabaseName(%q) succeeded, want error", test.name)
			}
		})
	}
}

func TestOpenPoolMigratesOnlyAnIsolatedDatabase(t *testing.T) {
	cfg := config.DBConfig{Name: "xflickr_wtk80"}
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

func TestOpenPoolRejectsUnsafeDatabaseBeforeConnecting(t *testing.T) {
	created := false
	_, err := openPool(context.Background(), config.DBConfig{Name: "xflickr"}, func(context.Context, config.DBConfig) (*pgxpool.Pool, error) {
		created = true
		return nil, nil
	}, func(context.Context, config.DBConfig) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "refusing database") || created {
		t.Fatalf("openPool() error = %v, create called=%t", err, created)
	}
}

func TestOpenPoolWrapsPoolAndMigrationFailures(t *testing.T) {
	t.Run("pool factory", func(t *testing.T) {
		wantErr := errors.New("pool unavailable")
		_, err := openPool(context.Background(), config.DBConfig{Name: "xflickr_test"}, func(context.Context, config.DBConfig) (*pgxpool.Pool, error) {
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
		wantErr := errors.New("migration failed")
		_, err = openPool(context.Background(), config.DBConfig{Name: "xflickr_test"}, func(context.Context, config.DBConfig) (*pgxpool.Pool, error) {
			return pool, nil
		}, func(context.Context, config.DBConfig) error { return wantErr })
		if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "migrate test PostgreSQL database") {
			t.Fatalf("openPool() error = %v", err)
		}
	})
}

func newUnconnectedPool(t *testing.T) (*pgxpool.Pool, error) {
	t.Helper()
	poolConfig, err := pgxpool.ParseConfig("postgres://localhost:5432/xflickr_test?sslmode=disable")
	if err != nil {
		return nil, err
	}
	return pgxpool.NewWithConfig(context.Background(), poolConfig)
}
