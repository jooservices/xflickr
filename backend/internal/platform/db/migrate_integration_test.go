package db_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/migrations"
	"github.com/pressly/goose/v3"
)

func TestMigrateDownToZeroAndUpUsesEmbeddedNames(t *testing.T) {
	ctx, cfg, pool := prepareMigrationTest(t)
	sources := embeddedMigrations(t)

	downOutput := migrateDownToZero(t, ctx, cfg, len(sources))
	for _, source := range sources {
		if !strings.Contains(downOutput, source.name) {
			t.Fatalf("migrate down output = %q, missing embedded migration %s", downOutput, source.name)
		}
	}
	assertOnlyZeroVersion(t, ctx, pool)

	var status bytes.Buffer
	if err := db.Migrate(ctx, cfg, db.MigrateStatus, &status); err != nil {
		t.Fatalf("read migration status after down: %v", err)
	}
	for _, source := range sources {
		assertStatusLine(t, status.String(), source.name, "pending")
	}

	var up bytes.Buffer
	if err := db.Migrate(ctx, cfg, db.MigrateUp, &up); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	for _, source := range sources {
		if !strings.Contains(up.String(), source.name) {
			t.Fatalf("migrate up output = %q, missing embedded migration %s", up.String(), source.name)
		}
	}

	status.Reset()
	if err := db.Migrate(ctx, cfg, db.MigrateStatus, &status); err != nil {
		t.Fatalf("read migration status after up: %v", err)
	}
	for _, source := range sources {
		assertStatusLine(t, status.String(), source.name, "applied")
	}
	assertEachEmbeddedVersionOnce(t, ctx, pool, sources)
}

func TestMigrateUpFromSeveralGoroutinesAppliesEachVersionOnce(t *testing.T) {
	ctx, cfg, pool := prepareMigrationTest(t)
	sources := embeddedMigrations(t)
	migrateDownToZero(t, ctx, cfg, len(sources))
	assertOnlyZeroVersion(t, ctx, pool)

	const workers = 4
	var ready, start sync.WaitGroup
	ready.Add(workers)
	start.Add(1)
	errs := make(chan error, workers)
	for range workers {
		go func() {
			ready.Done()
			start.Wait()
			errs <- db.Migrate(ctx, cfg, db.MigrateUp, io.Discard)
		}()
	}
	ready.Wait()
	start.Done()
	for range workers {
		if err := <-errs; err != nil {
			t.Errorf("concurrent migrate up: %v", err)
		}
	}

	assertEachEmbeddedVersionOnce(t, ctx, pool, sources)
}

func TestMigrateUpAppliesLowerVersionAfterHigherVersion(t *testing.T) {
	ctx, cfg, pool := prepareMigrationTest(t)
	sources := embeddedMigrations(t)
	lower, higher := outOfOrderPair(t, sources)
	assertEachEmbeddedVersionOnce(t, ctx, pool, sources)
	higherAppliedAt := versionAppliedAt(t, ctx, pool, higher.version)

	tag, err := pool.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE version_id = $1`, goose.DefaultTablename), lower.version)
	if err != nil {
		t.Fatalf("remove applied version %d: %v", lower.version, err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("remove applied version %d affected %d rows", lower.version, tag.RowsAffected())
	}
	if _, err := versionAppliedAtOptional(ctx, pool, lower.version); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("version %d after delete error = %v, want no rows", lower.version, err)
	}

	var up bytes.Buffer
	if err := db.Migrate(ctx, cfg, db.MigrateUp, &up); err != nil {
		t.Fatalf("migrate up with a missing lower version: %v", err)
	}
	if !strings.Contains(up.String(), lower.name) {
		t.Fatalf("migrate up output = %q, want reapplied %s", up.String(), lower.name)
	}

	lowerAppliedAt := versionAppliedAt(t, ctx, pool, lower.version)
	if !lowerAppliedAt.After(higherAppliedAt) {
		t.Fatalf(
			"version %d applied at %s, want after higher version %d applied at %s",
			lower.version, lowerAppliedAt, higher.version, higherAppliedAt,
		)
	}
	assertEachEmbeddedVersionOnce(t, ctx, pool, sources)
}

func prepareMigrationTest(t *testing.T) (context.Context, config.DBConfig, *pgxpool.Pool) {
	t.Helper()
	cfg := worktreeDatabaseConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	t.Cleanup(func() {
		if err := db.Migrate(ctx, cfg, db.MigrateUp, io.Discard); err != nil {
			t.Errorf("restore PostgreSQL migrations: %v", err)
		}
	})
	pool := dbtest.Open(t, cfg)
	return ctx, cfg, pool
}

func worktreeDatabaseConfig(t *testing.T) config.DBConfig {
	t.Helper()
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
	return dbConfig
}

type embeddedMigration struct {
	version int64
	name    string
	upSQL   string
}

func embeddedMigrations(t *testing.T) []embeddedMigration {
	t.Helper()
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	sources := make([]embeddedMigration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := goose.NumericComponent(entry.Name())
		if err != nil {
			t.Fatalf("parse migration version from %s: %v", entry.Name(), err)
		}
		body, err := fs.ReadFile(migrations.Files, entry.Name())
		if err != nil {
			t.Fatalf("read embedded migration %s: %v", entry.Name(), err)
		}
		sources = append(sources, embeddedMigration{
			version: version,
			name:    entry.Name(),
			upSQL:   migrationUpSQL(string(body)),
		})
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].version < sources[j].version })
	if len(sources) < 2 {
		t.Fatal("expected at least two embedded migrations")
	}
	return sources
}

func outOfOrderPair(t *testing.T, sources []embeddedMigration) (embeddedMigration, embeddedMigration) {
	t.Helper()
	lower := sources[0]
	higher := sources[len(sources)-1]
	if lower.version >= higher.version {
		t.Fatalf("embedded versions are not ordered: %d then %d", lower.version, higher.version)
	}
	// Reapplying the version row is safe only when the Up section has no SQL.
	// The initial migration is that anchor; later migrations build real tables.
	if strings.TrimSpace(lower.upSQL) != "" {
		t.Fatalf("lowest embedded migration %s is not empty and cannot be reapplied", lower.name)
	}
	return lower, higher
}

func migrationUpSQL(content string) string {
	const upMarker = "-- +goose Up"
	const downMarker = "-- +goose Down"
	_, afterUp, ok := strings.Cut(content, upMarker)
	if !ok {
		return strings.TrimSpace(content)
	}
	up, _, _ := strings.Cut(afterUp, downMarker)
	var statements strings.Builder
	for _, line := range strings.Split(up, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		statements.WriteString(trimmed)
		statements.WriteByte('\n')
	}
	return strings.TrimSpace(statements.String())
}

func migrateDownToZero(t *testing.T, ctx context.Context, cfg config.DBConfig, migrations int) string {
	t.Helper()
	var output bytes.Buffer
	for range migrations {
		var step bytes.Buffer
		if err := db.Migrate(ctx, cfg, db.MigrateDown, &step); err != nil {
			t.Fatalf("migrate down: %v", err)
		}
		output.Write(step.Bytes())
	}
	err := db.Migrate(ctx, cfg, db.MigrateDown, io.Discard)
	if !errors.Is(err, goose.ErrNoNextVersion) {
		t.Fatalf("migrate down at version zero error = %v, want no next version", err)
	}
	return output.String()
}

func assertStatusLine(t *testing.T, status, name, state string) {
	t.Helper()
	for _, line := range strings.Split(status, "\n") {
		if strings.Contains(line, name) {
			if !strings.Contains(line, state) {
				t.Fatalf("status line %q, want state %s", line, state)
			}
			return
		}
	}
	t.Fatalf("migration status = %q, missing %s", status, name)
}

func assertOnlyZeroVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	counts := versionCounts(t, ctx, pool)
	if len(counts) != 1 || counts[0] != 1 {
		t.Fatalf("version counts after down to zero = %v, want only version 0", counts)
	}
}

func assertEachEmbeddedVersionOnce(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sources []embeddedMigration) {
	t.Helper()
	counts := versionCounts(t, ctx, pool)
	if counts[0] != 1 {
		t.Fatalf("version 0 count = %d, want 1; counts=%v", counts[0], counts)
	}
	for _, source := range sources {
		if counts[source.version] != 1 {
			t.Fatalf("version %d count = %d, want 1; counts=%v", source.version, counts[source.version], counts)
		}
	}
	if len(counts) != len(sources)+1 {
		t.Fatalf("version counts = %v, want version 0 and %d embedded migrations", counts, len(sources))
	}
}

func versionCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[int64]int {
	t.Helper()
	rows, err := pool.Query(ctx, fmt.Sprintf(`SELECT version_id, count(*) FROM %s GROUP BY version_id`, goose.DefaultTablename))
	if err != nil {
		t.Fatalf("count migration versions: %v", err)
	}
	defer rows.Close()

	counts := make(map[int64]int)
	for rows.Next() {
		var version int64
		var count int
		if err := rows.Scan(&version, &count); err != nil {
			t.Fatalf("scan migration version count: %v", err)
		}
		counts[version] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read migration version counts: %v", err)
	}
	return counts
}

func versionAppliedAt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, version int64) time.Time {
	t.Helper()
	appliedAt, err := versionAppliedAtOptional(ctx, pool, version)
	if err != nil {
		t.Fatalf("read applied time for version %d: %v", version, err)
	}
	return appliedAt
}

func versionAppliedAtOptional(ctx context.Context, pool *pgxpool.Pool, version int64) (time.Time, error) {
	var appliedAt time.Time
	err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT tstamp FROM %s WHERE version_id = $1 ORDER BY id DESC LIMIT 1`, goose.DefaultTablename), version).Scan(&appliedAt)
	return appliedAt, err
}
