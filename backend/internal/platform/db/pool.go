// Package db provides shared PostgreSQL access and schema migration helpers.
package db

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jooservices/xflickr/internal/platform/config"
)

// NewPool creates and verifies a traced PostgreSQL connection pool from the
// application's typed database configuration.
func NewPool(ctx context.Context, cfg config.DBConfig) (*pgxpool.Pool, error) {
	poolConfig, err := newPoolConfig(cfg)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	return pool, nil
}

func newPoolConfig(cfg config.DBConfig) (*pgxpool.Config, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	poolConfig, err := pgxpool.ParseConfig(connectionString(cfg))
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL connection settings: %w", err)
	}
	poolConfig.MaxConns = cfg.MaxConns
	poolConfig.MinConns = cfg.MinConns
	poolConfig.MaxConnLifetime = cfg.MaxConnLifetime
	poolConfig.MaxConnIdleTime = cfg.MaxConnIdleTime
	poolConfig.HealthCheckPeriod = cfg.HealthCheckPeriod
	poolConfig.ConnConfig.Tracer = otelpgx.NewTracer()
	return poolConfig, nil
}

func connectionString(cfg config.DBConfig) string {
	connectionURL := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   net.JoinHostPort(strings.Trim(cfg.Host, "[]"), strconv.Itoa(cfg.Port)),
		Path:   "/" + cfg.Name,
	}
	query := connectionURL.Query()
	query.Set("sslmode", cfg.SSLMode)
	connectionURL.RawQuery = query.Encode()
	return connectionURL.String()
}

func parseConnectionConfig(cfg config.DBConfig) (*pgx.ConnConfig, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	connectionConfig, err := pgx.ParseConfig(connectionString(cfg))
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL connection settings: %w", err)
	}
	connectionConfig.Tracer = otelpgx.NewTracer()
	return connectionConfig, nil
}
