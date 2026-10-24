package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/jooservices/xflickr/internal/platform/config"
)

func TestRunMigrateRequiresExactlyOneSupportedAction(t *testing.T) {
	for _, args := range [][]string{nil, {"up", "down"}, {"fresh"}} {
		var stderr bytes.Buffer
		called := false
		code := runMigrate(context.Background(), args, &bytes.Buffer{}, &stderr, func(context.Context, string, io.Writer) error {
			called = true
			return nil
		})
		if code != 2 || called || stderr.Len() == 0 {
			t.Errorf("runMigrate(%v) = (%d, called=%t, stderr=%q), want rejected command", args, code, called, stderr.String())
		}
	}
}

func TestRunMigrateDelegatesActionAndReportsErrors(t *testing.T) {
	for _, action := range []string{"up", "down", "status"} {
		t.Run(action, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			called := false
			code := runMigrate(context.Background(), []string{action}, &stdout, &stderr, func(_ context.Context, got string, output io.Writer) error {
				called = true
				if got != action || output != &stdout {
					t.Fatalf("migration runner got (%q, %T), want (%q, stdout)", got, output, action)
				}
				return errors.New("migration failed")
			})
			if code != 1 || !called || !bytes.Contains(stderr.Bytes(), []byte("migration failed")) {
				t.Fatalf("runMigrate() = (%d, called=%t, stderr=%q)", code, called, stderr.String())
			}
		})
	}
}

func TestRunConfiguredMigrationsWithInjectedDependencies(t *testing.T) {
	wantConfig := config.DBConfig{Host: "db.internal", Name: "xflickr_wtk85", User: "worker"}
	var stdout bytes.Buffer
	called := false
	err := runConfiguredMigrationsWith(context.Background(), "status", &stdout,
		func() (config.DBConfig, error) { return wantConfig, nil },
		func(_ context.Context, got config.DBConfig, action string, output io.Writer) error {
			called = true
			if got != wantConfig || action != "status" || output != &stdout {
				t.Errorf("migration received (%#v, %q, %T)", got, action, output)
			}
			return nil
		},
	)
	if err != nil || !called {
		t.Fatalf("runConfiguredMigrationsWith() = (%t, %v), want migration called successfully", called, err)
	}

	wantErr := errors.New("config unavailable")
	called = false
	err = runConfiguredMigrationsWith(context.Background(), "up", &stdout,
		func() (config.DBConfig, error) { return config.DBConfig{}, wantErr },
		func(context.Context, config.DBConfig, string, io.Writer) error { called = true; return nil },
	)
	if !errors.Is(err, wantErr) || called {
		t.Fatalf("runConfiguredMigrationsWith(load failure) = (called=%t, %v)", called, err)
	}
}

func TestRunConfiguredMigrationsReturnsMissingDatabaseConfig(t *testing.T) {
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("change to empty directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDir) })
	for _, name := range []string{"DB_HOST", "DB_NAME", "DB_USER", "DB_DATABASE", "DB_USERNAME"} {
		t.Setenv(name, "")
	}
	if err := runConfiguredMigrations(context.Background(), "status", &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "DB_HOST") {
		t.Fatalf("runConfiguredMigrations() error = %v, want missing database settings", err)
	}
}
