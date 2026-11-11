package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAccountScopedSizesMigrationPreservesLegacyPhotoResults(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate migration test source")
	}
	migrationPath := filepath.Join(filepath.Dir(source), "../../../migrations/20261111110000_account_scoped_photo_sizes.sql")
	migration, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read account-scoped sizes migration: %v", err)
	}
	up := strings.SplitN(string(migration), "-- +goose Down", 2)[0]
	if strings.Contains(strings.ToUpper(up), "UPDATE PHOTOS") {
		t.Fatal("account-scoped sizes migration must not rewrite legacy photo rows")
	}
}
