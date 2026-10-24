package dbtest

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jooservices/xflickr/internal/platform/config"
)

func TestOpenUsesMigratedWorktreeDatabase(t *testing.T) {
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
	pool := Open(t, dbConfig)

	var currentDatabase string
	if err := pool.QueryRow(t.Context(), "SELECT current_database()").Scan(&currentDatabase); err != nil {
		t.Fatalf("read test database name: %v", err)
	}
	if currentDatabase != dbConfig.Name {
		t.Fatalf("connected database does not match the configured test database")
	}
}
