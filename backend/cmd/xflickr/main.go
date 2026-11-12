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

// commandOptions are the dependencies of the command dispatcher.
type commandOptions struct {
	envRoles      string
	registry      *roles.Registry
	stdout        io.Writer
	stderr        io.Writer
	runMigrations migrationRunner
	runRotation   rotationRunner
}

func main() {
	os.Exit(mainExit())
}

func mainExit() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return executeWithHealth(ctx, os.Args[1:], os.Getenv("XFLICKR_ROLES"), roles.NewBuiltinRegistry(version), os.Stderr)
}

func execute(ctx context.Context, args []string, envRoles string, registry *roles.Registry, stderr io.Writer) int {
	return executeWithCommands(ctx, args, commandOptions{envRoles: envRoles, registry: registry, stdout: os.Stdout, stderr: stderr, runMigrations: runConfiguredMigrations, runRotation: runConfiguredRotation})
}

func executeWithHealth(ctx context.Context, args []string, envRoles string, registry *roles.Registry, stderr io.Writer) int {
	return executeWithCommandsAndHealth(ctx, args, commandOptions{envRoles: envRoles, registry: registry, stdout: os.Stdout, stderr: stderr, runMigrations: runConfiguredMigrations, runRotation: runConfiguredRotation}, true)
}

func executeWithMigration(ctx context.Context, args []string, envRoles string, registry *roles.Registry, stdout, stderr io.Writer, run migrationRunner) int {
	return executeWithCommands(ctx, args, commandOptions{envRoles: envRoles, registry: registry, stdout: stdout, stderr: stderr, runMigrations: run, runRotation: runConfiguredRotation})
}

func executeWithCommands(ctx context.Context, args []string, opts commandOptions) int {
	return executeWithCommandsAndHealth(ctx, args, opts, false)
}

func executeWithCommandsAndHealth(ctx context.Context, args []string, opts commandOptions, withHealth bool) int {
	if code, handled := runSubcommand(ctx, args, opts); handled {
		return code
	}
	selected, err := parseRoles(args, opts.envRoles, opts.registry)
	if err != nil {
		fmt.Fprintf(opts.stderr, "xflickr: %v\n", err)
		return 2
	}
	return runRoles(ctx, selected, opts.stderr, withHealth)
}

// runSubcommand runs the maintenance command named by args[0]. It reports
// false when args does not start with a maintenance command.
func runSubcommand(ctx context.Context, args []string, opts commandOptions) (int, bool) {
	if len(args) == 0 {
		return 0, false
	}
	switch args[0] {
	case "backfill-observations":
		return runObservationBackfill(ctx, args[1:], opts.stdout, opts.stderr, roles.BackfillObservations), true
	case "repair-checkpoints":
		return runCheckpointRepair(ctx, args[1:], opts.stdout, opts.stderr, roles.RepairZeroCheckpoints), true
	case "migrate":
		return runMigrate(ctx, args[1:], opts.stdout, opts.stderr, opts.runMigrations), true
	case "requeue":
		return runRequeue(ctx, args[1:], opts.stdout, opts.stderr, runConfiguredRequeue), true
	case "crypto":
		return runCrypto(ctx, args[1:], opts.stdout, opts.stderr, opts.runRotation), true
	default:
		return 0, false
	}
}

func runRoles(ctx context.Context, selected []roles.Role, stderr io.Writer, withHealth bool) int {
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
