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
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/migrations"
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

func TestCallLogDownMigrationPreservesUnassociatedRows(t *testing.T) {
	ctx, cfg, pool := prepareMigrationTest(t)
	requestID := "f4f2dc30-25f6-4e39-bc2a-933676de40cf"
	if _, err := pool.Exec(ctx, `INSERT INTO api_call_log (at, called_at, method, status, outcome, request_id)
		VALUES (now(), now(), 'flickr.test.echo', 'success', 'success', $1)`, requestID); err != nil {
		t.Fatalf("insert unassociated call: %v", err)
	}
	if err := db.Migrate(ctx, cfg, db.MigrateDown, io.Discard); err == nil || !strings.Contains(err.Error(), "unassociated call rows exist") {
		t.Fatalf("rollback with an unassociated call = %v, want a data-preserving refusal", err)
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM api_call_log WHERE request_id=$1 AND key_id IS NULL`, requestID).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("unassociated call rows after refused rollback = %d, %v; want 1", rows, err)
	}
	var keyIsNotNull bool
	if err := pool.QueryRow(ctx, `SELECT attnotnull FROM pg_attribute WHERE attrelid='api_call_log'::regclass AND attname='key_id' AND NOT attisdropped`).Scan(&keyIsNotNull); err != nil || keyIsNotNull {
		t.Fatalf("key_id not-null after refused rollback = %t, %v; want false", keyIsNotNull, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM api_call_log WHERE request_id=$1`, requestID); err != nil {
		t.Fatalf("remove isolated rollback-test row: %v", err)
	}
	if err := db.Migrate(ctx, cfg, db.MigrateDown, io.Discard); err != nil {
		t.Fatalf("rollback after explicitly removing the isolated test row: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT attnotnull FROM pg_attribute WHERE attrelid='api_call_log'::regclass AND attname='key_id' AND NOT attisdropped`).Scan(&keyIsNotNull); err != nil || !keyIsNotNull {
		t.Fatalf("key_id not-null after safe rollback = %t, %v; want true", keyIsNotNull, err)
	}
	if err := db.Migrate(ctx, cfg, db.MigrateUp, io.Discard); err != nil {
		t.Fatalf("reapply unassociated-call migration: %v", err)
	}
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

func TestMigrateAppliesLowerVersionAfterHigherVersion(t *testing.T) {
	ctx, cfg, pool := prepareEmptyMigrationTest(t)
	const (
		lowerVersion  int64 = 20261025000100
		higherVersion int64 = 20261025000200
		lowerFile           = "20261025000100_lower.sql"
		higherFile          = "20261025000200_higher.sql"
	)
	lowerSQL := "-- +goose Up\nCREATE TABLE lower_applied (id integer PRIMARY KEY);\n-- +goose Down\nDROP TABLE lower_applied;\n"
	higherSQL := "-- +goose Up\nCREATE TABLE higher_applied (id integer PRIMARY KEY);\n-- +goose Down\nDROP TABLE higher_applied;\n"
	higherOnly := fstest.MapFS{
		higherFile: {Data: []byte(higherSQL), Mode: 0o644},
	}
	if err := db.MigrateFiles(ctx, cfg, db.MigrateUp, io.Discard, higherOnly); err != nil {
		t.Fatalf("migrate higher version: %v", err)
	}
	assertAppliedVersions(t, ctx, pool, []int64{higherVersion})
	assertRelation(t, ctx, pool, "public.higher_applied", true)
	assertRelation(t, ctx, pool, "public.lower_applied", false)

	both := fstest.MapFS{
		lowerFile:  {Data: []byte(lowerSQL), Mode: 0o644},
		higherFile: {Data: []byte(higherSQL), Mode: 0o644},
	}
	var up bytes.Buffer
	if err := db.MigrateFiles(ctx, cfg, db.MigrateUp, &up, both); err != nil {
		t.Fatalf("migrate missing lower version: %v", err)
	}
	if !strings.Contains(up.String(), lowerFile) {
		t.Fatalf("migrate up output = %q, want reapplied %s", up.String(), lowerFile)
	}
	// id order records application order. tstamp is too coarse for that check.
	assertAppliedVersions(t, ctx, pool, []int64{higherVersion, lowerVersion})
	assertRelation(t, ctx, pool, "public.lower_applied", true)
	assertRelation(t, ctx, pool, "public.higher_applied", true)
}

func prepareMigrationTest(t *testing.T) (context.Context, config.DBConfig, *pgxpool.Pool) {
	t.Helper()
	ctx, cfg := prepareIsolatedDatabase(t)
	pool := dbtest.Open(t, cfg)
	assertCurrentDatabase(t, ctx, pool, cfg.Name)
	return ctx, cfg, pool
}

func prepareEmptyMigrationTest(t *testing.T) (context.Context, config.DBConfig, *pgxpool.Pool) {
	t.Helper()
	ctx, cfg := prepareIsolatedDatabase(t)
	pool, err := db.NewPool(ctx, cfg)
	if err != nil {
		t.Fatalf("open isolated database: %v", err)
	}
	t.Cleanup(pool.Close)
	assertCurrentDatabase(t, ctx, pool, cfg.Name)
	return ctx, cfg, pool
}

func prepareIsolatedDatabase(t *testing.T) (context.Context, config.DBConfig) {
	t.Helper()
	base := worktreeDatabaseConfig(t)
	hadOutbox := relationExists(t, base, "public.outbox_events")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	cfg := dbtest.EmptyDatabase(t, base)
	if cfg.Name == base.Name {
		t.Fatal("isolated database reused the worktree database")
	}
	t.Cleanup(func() {
		if hadOutbox && !relationExists(t, base, "public.outbox_events") {
			t.Error("worktree outbox_events table was dropped")
		}
	})
	return ctx, cfg
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

func relationExists(t *testing.T, cfg config.DBConfig, relation string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := db.NewPool(ctx, cfg)
	if err != nil {
		t.Fatalf("open worktree database: %v", err)
	}
	defer pool.Close()

	var name *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass($1)`, relation).Scan(&name); err != nil {
		t.Fatalf("lookup relation %s: %v", relation, err)
	}
	return name != nil
}

type embeddedMigration struct {
	version int64
	name    string
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
		sources = append(sources, embeddedMigration{version: version, name: entry.Name()})
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].version < sources[j].version })
	if len(sources) < 2 {
		t.Fatal("expected at least two embedded migrations")
	}
	return sources
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

func assertAppliedVersions(t *testing.T, ctx context.Context, pool *pgxpool.Pool, want []int64) {
	t.Helper()
	rows, err := pool.Query(ctx, fmt.Sprintf(
		`SELECT version_id FROM %s WHERE version_id > 0 AND is_applied ORDER BY id`,
		goose.DefaultTablename,
	))
	if err != nil {
		t.Fatalf("read applied migration versions: %v", err)
	}
	defer rows.Close()

	var got []int64
	for rows.Next() {
		var version int64
		if err := rows.Scan(&version); err != nil {
			t.Fatalf("scan applied migration version: %v", err)
		}
		got = append(got, version)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read applied migration versions: %v", err)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("applied versions in id order = %v, want %v", got, want)
	}
}

func assertRelation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, relation string, want bool) {
	t.Helper()
	var name *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass($1)`, relation).Scan(&name); err != nil {
		t.Fatalf("lookup relation %s: %v", relation, err)
	}
	if got := name != nil; got != want {
		t.Fatalf("relation %s exists = %t, want %t", relation, got, want)
	}
}

func assertCurrentDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool, want string) {
	t.Helper()
	var current string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&current); err != nil {
		t.Fatalf("read current database: %v", err)
	}
	if current != want {
		t.Fatal("connected database does not match the isolated test database")
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
