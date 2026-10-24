package dbtest

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db"
)

func TestOpenUsesMigratedWorktreeDatabase(t *testing.T) {
	dbConfig := worktreeDatabaseConfig(t)
	pool := Open(t, dbConfig)

	var currentDatabase string
	if err := pool.QueryRow(t.Context(), "SELECT current_database()").Scan(&currentDatabase); err != nil {
		t.Fatalf("read test database name: %v", err)
	}
	if currentDatabase != dbConfig.Name {
		t.Fatalf("connected database does not match the configured test database")
	}
}

func TestEmptyDatabaseIsCreatedAndDroppedWithoutTouchingWorktreeSchema(t *testing.T) {
	source := worktreeDatabaseConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	hadOutbox := relationExists(t, ctx, source, "public.outbox_events")

	var isolatedName string
	t.Run("create", func(t *testing.T) {
		isolated := EmptyDatabase(t, source)
		if isolated.Name == source.Name {
			t.Fatal("isolated database reused the worktree database")
		}
		isolatedName = isolated.Name

		pool, err := db.NewPool(ctx, isolated)
		if err != nil {
			t.Fatalf("open isolated database: %v", err)
		}
		defer pool.Close()

		var currentDatabase string
		if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&currentDatabase); err != nil {
			t.Fatalf("read isolated database name: %v", err)
		}
		if currentDatabase != isolated.Name {
			t.Fatal("connected database does not match the isolated test database")
		}
		var gooseTable *string
		if err := pool.QueryRow(ctx, `SELECT to_regclass('public.goose_db_version')`).Scan(&gooseTable); err != nil {
			t.Fatalf("lookup goose version table: %v", err)
		}
		if gooseTable != nil {
			t.Fatal("empty database already contains goose versions")
		}
	})

	if isolatedName == "" {
		t.Fatal("isolated database was not created")
	}
	admin, err := db.NewPool(ctx, source)
	if err != nil {
		t.Fatalf("open worktree database: %v", err)
	}
	defer admin.Close()

	var dropped bool
	if err := admin.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, isolatedName).Scan(&dropped); err != nil {
		t.Fatalf("check isolated database cleanup: %v", err)
	}
	if !dropped {
		t.Fatal("isolated database was not dropped")
	}
	var currentDatabase string
	if err := admin.QueryRow(ctx, "SELECT current_database()").Scan(&currentDatabase); err != nil {
		t.Fatalf("read worktree database name: %v", err)
	}
	if currentDatabase != source.Name {
		t.Fatal("maintenance connection left the worktree database")
	}
	if hadOutbox && !relationExists(t, ctx, source, "public.outbox_events") {
		t.Fatal("worktree outbox_events table was dropped")
	}
}

func worktreeDatabaseConfig(t *testing.T) config.DBConfig {
	t.Helper()
	if os.Getenv("XFLICKR_DBTEST_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_DBTEST_INTEGRATION_TEST=1 to use the configured XFlickr worktree database")
	}

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source file")
	}
	worktreeRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../"))
	dbConfig, err := config.LoadDatabaseWithDotEnv(filepath.Join(worktreeRoot, ".env"))
	if err != nil {
		t.Fatalf("load test PostgreSQL settings: %v", err)
	}
	if err := ValidateDatabaseName(dbConfig.Name); err != nil {
		t.Fatal(err)
	}
	return dbConfig
}

func relationExists(t *testing.T, ctx context.Context, cfg config.DBConfig, relation string) bool {
	t.Helper()
	pool, err := db.NewPool(ctx, cfg)
	if err != nil {
		t.Fatalf("open PostgreSQL database: %v", err)
	}
	defer pool.Close()

	var name *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass($1)`, relation).Scan(&name); err != nil {
		t.Fatalf("lookup relation %s: %v", relation, err)
	}
	return name != nil
}
