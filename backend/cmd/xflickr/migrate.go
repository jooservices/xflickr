package main

import (
	"context"
	"fmt"
	"io"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db"
)

type migrationRunner func(context.Context, string, io.Writer) error
type databaseConfigLoader func() (config.DBConfig, error)
type configuredMigration func(context.Context, config.DBConfig, string, io.Writer) error

func runMigrate(ctx context.Context, args []string, stdout, stderr io.Writer, run migrationRunner) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "xflickr migrate: expected exactly one action: up, down, or status")
		return 2
	}
	if args[0] != db.MigrateUp && args[0] != db.MigrateDown && args[0] != db.MigrateStatus {
		fmt.Fprintf(stderr, "xflickr migrate: unsupported action %q; expected up, down, or status\n", args[0])
		return 2
	}
	if err := run(ctx, args[0], stdout); err != nil {
		fmt.Fprintf(stderr, "xflickr migrate: %v\n", err)
		return 1
	}
	return 0
}

func runConfiguredMigrations(ctx context.Context, action string, stdout io.Writer) error {
	return runConfiguredMigrationsWith(ctx, action, stdout, config.LoadDatabase, db.Migrate)
}

func runConfiguredMigrationsWith(ctx context.Context, action string, stdout io.Writer, load databaseConfigLoader, migrate configuredMigration) error {
	if load == nil {
		load = config.LoadDatabase
	}
	dbConfig, err := load()
	if err != nil {
		return err
	}
	if migrate == nil {
		migrate = db.Migrate
	}
	return migrate(ctx, dbConfig, action, stdout)
}
