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
}

func TestContactsKeysetIndexMigrationBuildsConcurrentlyAndRepairsOnRetry(t *testing.T) {
	sql, err := fs.ReadFile(migrations.Files, "20261111110000_contacts_keyset_index.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, down, found := strings.Cut(string(sql), "-- +goose Down")
	if !found || !strings.HasPrefix(string(sql), "-- +goose NO TRANSACTION\n") {
		t.Fatalf("contacts index migration must disable goose transactions: %q", sql)
	}
	drop := strings.Index(up, "DROP INDEX CONCURRENTLY IF EXISTS contacts_contact_nsid_person_nsid_idx")
	create := strings.Index(up, "CREATE INDEX CONCURRENTLY contacts_contact_nsid_person_nsid_idx")
	if drop < 0 || create < 0 || drop > create ||
		!strings.Contains(down, "DROP INDEX CONCURRENTLY IF EXISTS contacts_contact_nsid_person_nsid_idx") {
		t.Fatalf("contacts index migration must remove stale indexes before concurrent creation and drop concurrently: %q", sql)
	}
}
