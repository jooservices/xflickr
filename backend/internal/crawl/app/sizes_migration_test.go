package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAccountScopedSizesMigrationPreservesLegacyPhotoResults(t *testing.T) {
	migration := accountScopedSizesMigration(t)
	up := strings.SplitN(migration, "-- +goose Down", 2)[0]
	if strings.Contains(strings.ToUpper(up), "UPDATE PHOTOS") {
		t.Fatal("account-scoped sizes migration must not rewrite legacy photo rows")
	}
}

func TestAccountScopedSizesMigrationBuildsIndexesWithoutLongTableLocks(t *testing.T) {
	migration := accountScopedSizesMigration(t)
	up := strings.SplitN(migration, "-- +goose Down", 2)[0]
	if !strings.HasPrefix(up, "-- +goose NO TRANSACTION\n") {
		t.Fatal("account-scoped sizes migration must run without a wrapping transaction")
	}
	for _, index := range []string{
		"CREATE UNIQUE INDEX CONCURRENTLY entity_facet_restrictions_key_scope_idx",
		"CREATE UNIQUE INDEX CONCURRENTLY entity_facet_restrictions_account_scope_idx",
	} {
		if !strings.Contains(up, index) {
			t.Errorf("migration does not build index concurrently: %s", index)
		}
	}
	for _, line := range strings.Split(strings.ToLower(up), "\n") {
		statement := strings.TrimSpace(line)
		if strings.HasPrefix(statement, "create index") &&
			(strings.Contains(statement, " on photos") || strings.Contains(statement, " on only photos")) {
			t.Errorf("account-scoped sizes migration must not create an index on photos: %s", statement)
		}
	}
	for _, statement := range []string{
		"ADD CONSTRAINT entity_facet_restrictions_account_id_fkey",
		"REFERENCES flickr_accounts (id) ON DELETE CASCADE NOT VALID",
		"VALIDATE CONSTRAINT entity_facet_restrictions_account_id_fkey",
	} {
		if !strings.Contains(up, statement) {
			t.Errorf("migration omits staged foreign key validation: %s", statement)
		}
	}
	keys := strings.Index(up, "CREATE UNIQUE INDEX CONCURRENTLY entity_facet_restrictions_key_scope_idx")
	accounts := strings.Index(up, "CREATE UNIQUE INDEX CONCURRENTLY entity_facet_restrictions_account_scope_idx")
	dropPrimary := strings.Index(up, "ALTER TABLE entity_facet_restrictions\n    DROP CONSTRAINT entity_facet_restrictions_pkey")
	if keys < 0 || accounts < 0 || dropPrimary < 0 || dropPrimary < keys || dropPrimary < accounts {
		t.Fatal("legacy primary key must remain until both replacement indexes are ready")
	}
}

func TestMigrationVersionsAreUnique(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate migration test source")
	}
	migrationDir := filepath.Join(filepath.Dir(source), "../../../migrations")
	entries, err := os.ReadDir(migrationDir)
	if err != nil {
		t.Fatalf("read migration directory: %v", err)
	}
	versions := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, _, ok := strings.Cut(entry.Name(), "_")
		if !ok || len(version) != 14 {
			t.Errorf("migration %q does not start with a 14-digit version", entry.Name())
			continue
		}
		if previous, exists := versions[version]; exists {
			t.Errorf("migration version %s is shared by %s and %s", version, previous, entry.Name())
			continue
		}
		versions[version] = entry.Name()
	}
	if len(versions) == 0 {
		t.Fatal("no migrations found")
	}
}

func accountScopedSizesMigration(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate migration test source")
	}
	migrationPath := filepath.Join(filepath.Dir(source), "../../../migrations/20261112110000_account_scoped_photo_sizes.sql")
	migration, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read account-scoped sizes migration: %v", err)
	}
	return string(migration)
}
