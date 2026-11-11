package migrations_test

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/jooservices/xflickr/migrations"
)

var migrationName = regexp.MustCompile(`^(\d{14})_[a-z0-9_]+\.sql$`)

// TestVersionsAreUnique fails when two migration files share a version. Goose
// refuses to run with a duplicate version, which broke every integration test
// the day two branches picked the same timestamp.
func TestVersionsAreUnique(t *testing.T) {
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	seen := make(map[string]string, len(entries))
	for _, entry := range entries {
		match := migrationName.FindStringSubmatch(entry.Name())
		if match == nil {
			t.Errorf("migration %q must be named <14 digit timestamp>_<snake_case>.sql", entry.Name())
			continue
		}
		if other, ok := seen[match[1]]; ok {
			t.Errorf("duplicate migration version %s: %q and %q (rename the newer one to a later timestamp)", match[1], other, entry.Name())
		}
		seen[match[1]] = entry.Name()
	}
	if len(seen) == 0 {
		t.Fatal("no migrations found")
	}
}

func TestObservationLookupMigrationBuildsPartitionIndexesConcurrently(t *testing.T) {
	const migrationName = "20261111190000_observations_record_lookup_index.sql"
	migration, err := fs.ReadFile(migrations.Files, migrationName)
	if err != nil {
		t.Fatalf("read observation lookup migration: %v", err)
	}
	if !strings.Contains(string(migration), "-- +goose NO TRANSACTION") {
		t.Fatal("observation lookup migration must run outside a transaction")
	}
	for partition := 1; partition < 16; partition++ {
		index := fmt.Sprintf("observations_p%02d_record_lookup_idx", partition)
		create := fmt.Sprintf("CREATE INDEX CONCURRENTLY IF NOT EXISTS %s ON observations_p%02d (record_id, record_kind, user_id);", index, partition)
		drop := fmt.Sprintf("DROP INDEX CONCURRENTLY IF EXISTS %s;", index)
		if !strings.Contains(string(migration), create) {
			t.Errorf("migration does not build partition lookup index %s concurrently", index)
		}
		if !strings.Contains(string(migration), drop) {
			t.Errorf("migration does not drop partition lookup index %s concurrently", index)
		}
	}
	if strings.Contains(string(migration), "observations_p00_record_lookup_idx") {
		t.Fatal("migration must reuse the existing p00 lookup index instead of creating a duplicate")
}

func TestContactSummaryIndexesCanBeRetriedAfterPartialMigration(t *testing.T) {
	sql, err := migrations.Files.ReadFile("20261111170000_contact_screen_summaries.sql")
	if err != nil {
		t.Fatal(err)
	}
	statement := string(sql)
	for _, name := range []string{"work_items_user_contact_state_idx", "work_items_contact_photo_sizes_idx"} {
		if !strings.Contains(statement, "CREATE INDEX CONCURRENTLY IF NOT EXISTS "+name) {
			t.Errorf("%s must be created concurrently and skipped when a previous run succeeded", name)
		}
	}
	if !strings.Contains(statement, "CREATE INDEX IF NOT EXISTS photos_owner_contact_id_idx ON ONLY photos") {
		t.Error("the contact ID parent index must be available to attach its partition indexes")
	}
	for _, suffix := range []string{"recent", "contact_id"} {
		for partition := 0; partition < 16; partition++ {
			name := fmt.Sprintf("photos_p%02d_owner_%s_idx", partition, suffix)
			if !strings.Contains(statement, "CREATE INDEX CONCURRENTLY IF NOT EXISTS "+name) {
				t.Errorf("%s must be created concurrently and skipped when a previous run succeeded", name)
			}
		}
	}
	if !strings.Contains(statement, "pg_inherits") || !strings.Contains(statement, "ATTACH PARTITION") {
		t.Fatal("partition indexes must be attached only when not already attached")
	}
	if !strings.Contains(statement, "ORDER BY id LIMIT 500") || !strings.Contains(statement, "ORDER BY photo_id LIMIT 500") {
		t.Error("contact summary backfill must process bounded photo and favorite ID pages")
	}
}
