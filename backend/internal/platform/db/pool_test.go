package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/config"
)

func TestNewPoolConfigUsesDatabaseAndPoolSettings(t *testing.T) {
	cfg := validDatabaseConfig()
	cfg.Host = "db.internal"
	cfg.Port = 5544
	cfg.Name = "worktree_db"
	cfg.User = "worker"
	cfg.Password = "p@ss:/word"
	cfg.SSLMode = "require"
	cfg.SearchPath = "test_jooxf522,public"
	cfg.MaxConns = 32
	cfg.MinConns = 3
	cfg.MaxConnLifetime = 45 * time.Minute
	cfg.MaxConnIdleTime = 12 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second

	poolConfig, err := newPoolConfig(cfg)
	if err != nil {
		t.Fatalf("newPoolConfig() error = %v", err)
	}
	if poolConfig.ConnConfig.Host != "db.internal" || poolConfig.ConnConfig.Port != 5544 || poolConfig.ConnConfig.Database != "worktree_db" || poolConfig.ConnConfig.User != "worker" || poolConfig.ConnConfig.Password != "p@ss:/word" {
		t.Errorf("connection settings were not loaded from config")
	}
	if poolConfig.ConnConfig.TLSConfig == nil {
		t.Error("SSL mode require did not configure TLS")
	}
	if poolConfig.ConnConfig.RuntimeParams["search_path"] != cfg.SearchPath {
		t.Errorf("PostgreSQL search_path = %q, want %q", poolConfig.ConnConfig.RuntimeParams["search_path"], cfg.SearchPath)
	}
	if poolConfig.MaxConns != 32 || poolConfig.MinConns != 3 || poolConfig.MaxConnLifetime != 45*time.Minute || poolConfig.MaxConnIdleTime != 12*time.Minute || poolConfig.HealthCheckPeriod != 30*time.Second {
		t.Errorf("pool settings = %#v", poolConfig)
	}
	if poolConfig.ConnConfig.Tracer == nil {
		t.Error("connection config has no OpenTelemetry tracer")
	}
}

func TestNewPoolConfigRejectsInvalidDatabaseAndPoolSettings(t *testing.T) {
	tests := []struct {
		name string
		edit func(*config.DBConfig)
		want string
	}{
		{name: "missing host", edit: func(cfg *config.DBConfig) { cfg.Host = "" }, want: "DB_HOST"},
		{name: "zero maximum", edit: func(cfg *config.DBConfig) { cfg.MaxConns = 0 }, want: "DB_MAX_CONNS"},
		{name: "minimum exceeds maximum", edit: func(cfg *config.DBConfig) { cfg.MinConns = cfg.MaxConns + 1 }, want: "DB_MIN_CONNS"},
		{name: "no connection lifetime", edit: func(cfg *config.DBConfig) { cfg.MaxConnLifetime = 0 }, want: "DB_MAX_CONN_LIFETIME"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := validDatabaseConfig()
			test.edit(&cfg)
			if _, err := newPoolConfig(cfg); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("newPoolConfig() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestNewPoolConfigRejectsInvalidSSLMode(t *testing.T) {
	cfg := validDatabaseConfig()
	cfg.SSLMode = "not-a-ssl-mode"
	if _, err := newPoolConfig(cfg); err == nil || !strings.Contains(err.Error(), "parse PostgreSQL connection settings") {
		t.Fatalf("newPoolConfig() error = %v, want connection parsing error", err)
	}
}

func TestParseConnectionConfigRejectsInvalidSSLMode(t *testing.T) {
	cfg := validDatabaseConfig()
	cfg.SSLMode = "not-a-ssl-mode"
	if _, err := parseConnectionConfig(cfg); err == nil || !strings.Contains(err.Error(), "parse PostgreSQL connection settings") {
		t.Fatalf("parseConnectionConfig() error = %v, want connection parsing error", err)
	}
}

func TestNewPoolConfigOmitsEmptySearchPath(t *testing.T) {
	cfg := validDatabaseConfig()
	poolConfig, err := newPoolConfig(cfg)
	if err != nil {
		t.Fatalf("newPoolConfig() error = %v", err)
	}
	if _, exists := poolConfig.ConnConfig.RuntimeParams["search_path"]; exists {
		t.Errorf("unexpected search_path runtime parameter: %q", poolConfig.ConnConfig.RuntimeParams["search_path"])
	}
}

func TestNewPoolUsesPoolFactoryAndVerifiesConnection(t *testing.T) {
	wantErr := errors.New("unavailable")
	cfg := validDatabaseConfig()
	cfg.MinConns = 0 // the injected factory must not start background connections
	var pool *pgxpool.Pool
	createPool := func(ctx context.Context, config *pgxpool.Config) (*pgxpool.Pool, error) {
		var err error
		pool, err = pgxpool.NewWithConfig(ctx, config)
		return pool, err
	}
	t.Run("invalid database settings", func(t *testing.T) {
		invalid := cfg
		invalid.Host = ""
		if _, err := newPool(t.Context(), invalid, nil, nil); err == nil || !strings.Contains(err.Error(), "DB_HOST") {
			t.Fatalf("newPool() error = %v, want DB_HOST validation error", err)
		}
	})
	t.Run("pool factory failure", func(t *testing.T) {
		_, err := newPool(t.Context(), cfg, func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) {
			return nil, wantErr
		}, func(context.Context, *pgxpool.Pool) error { return nil })
		if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "create PostgreSQL pool") {
			t.Fatalf("newPool() error = %v, want wrapped factory error", err)
		}
	})
	t.Run("connection failure closes pool", func(t *testing.T) {
		_, err := newPool(t.Context(), cfg, createPool, func(context.Context, *pgxpool.Pool) error { return wantErr })
		if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "connect to PostgreSQL") {
			t.Fatalf("newPool() error = %v, want wrapped ping error", err)
		}
		if _, err := pool.Acquire(t.Context()); err == nil {
			t.Fatal("pool remained open after connection failure")
		}
	})
	t.Run("connected pool", func(t *testing.T) {
		got, err := newPool(t.Context(), cfg, createPool, func(context.Context, *pgxpool.Pool) error { return nil })
		if err != nil || got != pool {
			t.Fatalf("newPool() = (%p, %v), want (%p, nil)", got, err, pool)
		}
		got.Close()
	})
}

func TestNewPoolReturnsConnectionFailure(t *testing.T) {
	cfg := validDatabaseConfig()
	cfg.Host = "127.0.0.1"
	cfg.Port = 1
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if pool, err := NewPool(ctx, cfg); err == nil {
		pool.Close()
		t.Fatal("NewPool() succeeded without a PostgreSQL server")
	}
}

func validDatabaseConfig() config.DBConfig {
	return config.DBConfig{
		Host:              "127.0.0.1",
		Port:              5432,
		Name:              "xflickr_test",
		User:              "postgres",
		SSLMode:           "disable",
		MaxConns:          20,
		MinConns:          2,
		MaxConnLifetime:   time.Hour,
		MaxConnIdleTime:   30 * time.Minute,
		HealthCheckPeriod: time.Minute,
	}
}
