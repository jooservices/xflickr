package db_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestMigrateUpDownOnWorktreeDatabase(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the configured XFlickr worktree database")
	}

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source file")
	}
	worktreeRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../"))
	dbConfig, err := config.LoadDatabaseWithDotEnv(filepath.Join(worktreeRoot, ".env"))
	if err != nil {
		t.Fatalf("load test PostgreSQL settings: %v", err)
	}
	if err := dbtest.ValidateDatabaseName(dbConfig.Name); err != nil {
		t.Fatal(err)
	}

	dbtest.Open(t, dbConfig)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var status bytes.Buffer
	if err := db.Migrate(ctx, dbConfig, db.MigrateStatus, &status); err != nil {
		t.Fatalf("read migration status after setup: %v", err)
	}
	if !strings.Contains(status.String(), "applied") {
		t.Fatalf("migration status after setup = %q, want applied migration", status.String())
	}

	var down bytes.Buffer
	if err := db.Migrate(ctx, dbConfig, db.MigrateDown, &down); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if !strings.Contains(down.String(), "20261024180000_initial.sql") {
		t.Fatalf("migrate down output = %q", down.String())
	}

	status.Reset()
	if err := db.Migrate(ctx, dbConfig, db.MigrateStatus, &status); err != nil {
		t.Fatalf("read migration status after down: %v", err)
	}
	if !strings.Contains(status.String(), "pending") {
		t.Fatalf("migration status after down = %q, want pending", status.String())
	}

	var up bytes.Buffer
	if err := db.Migrate(ctx, dbConfig, db.MigrateUp, &up); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if !strings.Contains(up.String(), "20261024180000_initial.sql") {
		t.Fatalf("migrate up output = %q", up.String())
	}
}
