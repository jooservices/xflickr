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
	return execute(ctx, os.Args[1:], os.Getenv("XFLICKR_ROLES"), roles.NewBuiltinRegistry(version), os.Stderr)
}

func execute(ctx context.Context, args []string, envRoles string, registry *roles.Registry, stderr io.Writer) int {
	selected, err := parseRoles(args, envRoles, registry)
	if err != nil {
		fmt.Fprintf(stderr, "xflickr: %v\n", err)
		return 2
	}
	if err := roles.Run(ctx, selected); err != nil {
		fmt.Fprintf(stderr, "xflickr: %v\n", err)
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
