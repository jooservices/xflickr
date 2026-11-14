package dbtest

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jooservices/xflickr/internal/platform/config"
)

func TestAC5OpenUsesMigratedWorktreeDatabase(t *testing.T) {
	if os.Getenv("XFLICKR_DBTEST_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_DBTEST_INTEGRATION_TEST=1 to use the configured XFlickr worktree database")
	}

	dbConfig := worktreeDatabaseConfig(t)
	pool := Open(t, dbConfig)

	var currentDatabase string
	if err := pool.QueryRow(t.Context(), "SELECT current_database()").Scan(&currentDatabase); err != nil {
		t.Fatalf("read test database name: %v", err)
	}
	if currentDatabase != dbConfig.Name {
		t.Fatal("connected database does not match the configured test database")
	}
	var relation *string
	if err := pool.QueryRow(t.Context(), `SELECT to_regclass('public.key_assignments')`).Scan(&relation); err != nil {
		t.Fatalf("lookup migrated table: %v", err)
	}
	if relation == nil {
		t.Fatal("key_assignments table is missing from the migrated worktree database")
	}
}

func TestAC5IsolatedSchemaUsesTreeRole(t *testing.T) {
	if os.Getenv("XFLICKR_DBTEST_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_DBTEST_INTEGRATION_TEST=1 to use the configured XFlickr worktree database")
	}

	base := worktreeDatabaseConfig(t)
	scoped := IsolatedSchema(t, base)
	if scoped.Name != base.Name || scoped.SearchPath == "" {
		t.Fatal("isolated schema did not preserve and scope the tree database")
	}
	pool := Open(t, scoped)

	var currentDatabase, currentSchema string
	if err := pool.QueryRow(t.Context(), "SELECT current_database(), current_schema()").Scan(&currentDatabase, &currentSchema); err != nil {
		t.Fatalf("read isolated database and schema: %v", err)
	}
	if currentDatabase != base.Name || currentSchema == "public" {
		t.Fatalf("isolated test connected to database %q schema %q", currentDatabase, currentSchema)
	}
}

func worktreeDatabaseConfig(t *testing.T) config.DBConfig {
	t.Helper()
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
