package db

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/jooservices/xflickr/internal/platform/config"
)

func TestMigrateRejectsUnknownActionBeforeConnecting(t *testing.T) {
	err := Migrate(t.Context(), config.DBConfig{}, "fresh", nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported migration action") {
		t.Fatalf("Migrate() error = %v, want unsupported action", err)
	}
}

func TestMigrateRejectsInvalidConfigurationAndUnavailableDatabase(t *testing.T) {
	if err := Migrate(context.Background(), config.DBConfig{}, MigrateStatus, nil); err == nil || !strings.Contains(err.Error(), "DB_HOST") {
		t.Fatalf("Migrate() with invalid config error = %v", err)
	}

	cfg := config.DBConfig{
		Host:              "127.0.0.1",
		Port:              1,
		Name:              "xflickr_test",
		User:              "postgres",
		SSLMode:           "disable",
		MaxConns:          20,
		MinConns:          2,
		MaxConnLifetime:   time.Hour,
		MaxConnIdleTime:   30 * time.Minute,
		HealthCheckPeriod: time.Minute,
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, action := range []string{MigrateUp, MigrateDown, MigrateStatus} {
		err := Migrate(ctx, cfg, action, nil)
		if err == nil || !strings.Contains(err.Error(), "PostgreSQL") {
			t.Errorf("Migrate(%q) error = %v, want a PostgreSQL connection error", action, err)
		}
	}
}

func TestWriteMigrationResults(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		var output bytes.Buffer
		if err := writeMigrationResults(&output, nil); err != nil {
			t.Fatalf("writeMigrationResults() error = %v", err)
		}
		if got := output.String(); got != "No migrations to apply.\n" {
			t.Fatalf("output = %q", got)
		}
	})

	t.Run("one migration", func(t *testing.T) {
		var output bytes.Buffer
		result := &goose.MigrationResult{Source: &goose.Source{Version: 20261024180000, Path: "20261024180000_initial.sql"}, Direction: "applied"}
		if err := writeMigrationResults(&output, []*goose.MigrationResult{result}); err != nil {
			t.Fatalf("writeMigrationResults() error = %v", err)
		}
		if !strings.Contains(output.String(), "20261024180000_initial.sql") {
			t.Fatalf("output = %q", output.String())
		}
	})

	t.Run("writer error", func(t *testing.T) {
		if err := writeMigrationResults(errorWriter{}, []*goose.MigrationResult{{Source: &goose.Source{Version: 1, Path: "001.sql"}}}); err == nil {
			t.Fatal("writeMigrationResults() succeeded with a failing writer")
		}
	})
}

func TestWriteMigrationStatuses(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		var output bytes.Buffer
		if err := writeMigrationStatuses(&output, nil); err != nil {
			t.Fatalf("writeMigrationStatuses() error = %v", err)
		}
		if got := output.String(); got != "No migrations found.\n" {
			t.Fatalf("output = %q", got)
		}
	})

	t.Run("pending migration", func(t *testing.T) {
		var output bytes.Buffer
		status := &goose.MigrationStatus{
			Source: &goose.Source{Version: 20261024180000, Path: "20261024180000_initial.sql"},
			State:  goose.StatePending,
		}
		if err := writeMigrationStatuses(&output, []*goose.MigrationStatus{status}); err != nil {
			t.Fatalf("writeMigrationStatuses() error = %v", err)
		}
		if got := output.String(); !strings.Contains(got, "pending") || !strings.Contains(got, "20261024180000_initial.sql") {
			t.Fatalf("output = %q", got)
		}
	})

	t.Run("writer error", func(t *testing.T) {
		if err := writeMigrationStatuses(errorWriter{}, []*goose.MigrationStatus{{Source: &goose.Source{Version: 1, Path: "001.sql"}, State: goose.StateApplied}}); err == nil {
			t.Fatal("writeMigrationStatuses() succeeded with a failing writer")
		}
	})
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }
