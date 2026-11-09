package migrations_test

import (
	"io/fs"
	"regexp"
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
