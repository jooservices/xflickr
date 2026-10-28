package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/jooservices/xflickr/internal/platform/roles"
)

var version = "dev"

func main() {
	os.Exit(mainExit())
}

func mainExit() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return executeWithHealth(ctx, os.Args[1:], os.Getenv("XFLICKR_ROLES"), roles.NewBuiltinRegistry(version), os.Stderr)
}

func execute(ctx context.Context, args []string, envRoles string, registry *roles.Registry, stderr io.Writer) int {
	return executeWithCommands(ctx, args, envRoles, registry, os.Stdout, stderr, runConfiguredMigrations, runConfiguredRotation)
}

func executeWithHealth(ctx context.Context, args []string, envRoles string, registry *roles.Registry, stderr io.Writer) int {
	return executeWithCommandsAndHealth(ctx, args, envRoles, registry, os.Stdout, stderr, runConfiguredMigrations, runConfiguredRotation, true)
}

func executeWithMigration(ctx context.Context, args []string, envRoles string, registry *roles.Registry, stdout, stderr io.Writer, run migrationRunner) int {
	return executeWithCommands(ctx, args, envRoles, registry, stdout, stderr, run, runConfiguredRotation)
}

func executeWithCommands(ctx context.Context, args []string, envRoles string, registry *roles.Registry, stdout, stderr io.Writer, runMigrations migrationRunner, runRotation rotationRunner) int {
	return executeWithCommandsAndHealth(ctx, args, envRoles, registry, stdout, stderr, runMigrations, runRotation, false)
}

func executeWithCommandsAndHealth(ctx context.Context, args []string, envRoles string, registry *roles.Registry, stdout, stderr io.Writer, runMigrations migrationRunner, runRotation rotationRunner, withHealth bool) int {
	if len(args) > 0 && args[0] == "migrate" {
		return runMigrate(ctx, args[1:], stdout, stderr, runMigrations)
	}
	if len(args) > 0 && args[0] == "requeue" {
		return runRequeue(ctx, args[1:], stdout, stderr, runConfiguredRequeue)
	}
	if len(args) > 0 && args[0] == "crypto" {
		return runCrypto(ctx, args[1:], stdout, stderr, runRotation)
	}

	selected, err := parseRoles(args, envRoles, registry)
	if err != nil {
		fmt.Fprintf(stderr, "xflickr: %v\n", err)
		return 2
	}
	var runErr error
	if withHealth {
		runErr = roles.RunWithHealth(ctx, selected, version)
	} else {
		runErr = roles.Run(ctx, selected)
	}
	if runErr != nil {
		fmt.Fprintf(stderr, "xflickr: %v\n", runErr)
		return 1
	}
	return 0
}

func parseRoles(args []string, envRoles string, registry *roles.Registry) ([]roles.Role, error) {
	flags := flag.NewFlagSet("xflickr", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	roleNames := flags.String("role", envRoles, "comma-separated roles to run")
	if err := flags.Parse(args); err != nil {
		return nil, fmt.Errorf("%w; valid roles: %s", err, registry.ValidRoles())
	}
	if flags.NArg() > 0 {
		return nil, fmt.Errorf("unexpected arguments %q; valid roles: %s", flags.Args(), registry.ValidRoles())
	}
	return roles.ParseRoleList(*roleNames, registry)
}
