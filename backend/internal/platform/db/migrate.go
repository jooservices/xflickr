package db

import (
	"context"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/migrations"
	"github.com/pressly/goose/v3"
)

const (
	// MigrateUp applies all pending embedded migrations.
	MigrateUp = "up"
	// MigrateDown rolls back the most recently applied embedded migration.
	MigrateDown = "down"
	// MigrateStatus prints applied and pending embedded migrations.
	MigrateStatus = "status"
)

// Migrate runs the requested goose action using the embedded migration files.
func Migrate(ctx context.Context, cfg config.DBConfig, action string, output io.Writer) error {
	if action != MigrateUp && action != MigrateDown && action != MigrateStatus {
		return fmt.Errorf("unsupported migration action %q; expected up, down, or status", action)
	}
	if output == nil {
		output = io.Discard
	}

	connectionConfig, err := parseConnectionConfig(cfg)
	if err != nil {
		return err
	}
	database := stdlib.OpenDB(*connectionConfig)
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	provider, err := goose.NewProvider(goose.DialectPostgres, database, migrations.Files)
	if err != nil {
		_ = database.Close()
		return fmt.Errorf("create PostgreSQL migration provider: %w", err)
	}
	defer provider.Close()

	switch action {
	case MigrateUp:
		results, err := provider.Up(ctx)
		if err != nil {
			return fmt.Errorf("apply PostgreSQL migrations: %w", err)
		}
		return writeMigrationResults(output, results)
	case MigrateDown:
		result, err := provider.Down(ctx)
		if err != nil {
			return fmt.Errorf("roll back PostgreSQL migration: %w", err)
		}
		return writeMigrationResults(output, []*goose.MigrationResult{result})
	case MigrateStatus:
		statuses, err := provider.Status(ctx)
		if err != nil {
			return fmt.Errorf("read PostgreSQL migration status: %w", err)
		}
		return writeMigrationStatuses(output, statuses)
	default:
		return nil
	}
}

func writeMigrationResults(output io.Writer, results []*goose.MigrationResult) error {
	if len(results) == 0 {
		_, err := fmt.Fprintln(output, "No migrations to apply.")
		return err
	}
	for _, result := range results {
		if _, err := fmt.Fprintln(output, result); err != nil {
			return err
		}
	}
	return nil
}

func writeMigrationStatuses(output io.Writer, statuses []*goose.MigrationStatus) error {
	if len(statuses) == 0 {
		_, err := fmt.Fprintln(output, "No migrations found.")
		return err
	}
	for _, status := range statuses {
		if _, err := fmt.Fprintf(output, "%d %-8s %s\n", status.Source.Version, status.State, status.Source.Path); err != nil {
			return err
		}
	}
	return nil
}
