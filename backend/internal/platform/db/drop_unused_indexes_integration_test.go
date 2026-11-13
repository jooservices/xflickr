package db_test

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/db"
)

const dropUnusedIndexesMigration = "20261109090000_drop_unused_indexes.sql"

// TestDropUnusedIndexesDownRestoresExactDefinitions proves the Down migration
// recreates each dropped index with an identical pg_get_indexdef.
func TestDropUnusedIndexesDownRestoresExactDefinitions(t *testing.T) {
	files := migrationFilesThrough(t, dropUnusedIndexesMigration)
	ctx, cfg, pool := prepareMigrationTestWithFiles(t, files)
	assertDroppedIndexAbsent(t, ctx, pool, "photos_last_update_idx")
	assertDroppedIndexAbsent(t, ctx, pool, "photos_owner_nsid_idx")

	if err := db.MigrateFiles(ctx, cfg, db.MigrateDown, io.Discard, files); err != nil {
		t.Fatalf("roll back %s: %v", dropUnusedIndexesMigration, err)
	}
	first := indexDefinitions(t, ctx, pool, []string{
		"photos_last_update_idx",
		"photos_owner_nsid_idx",
	})

	if err := db.MigrateFiles(ctx, cfg, db.MigrateUp, io.Discard, files); err != nil {
		t.Fatalf("re-apply %s: %v", dropUnusedIndexesMigration, err)
	}
	assertDroppedIndexAbsent(t, ctx, pool, "photos_last_update_idx")
	assertDroppedIndexAbsent(t, ctx, pool, "photos_owner_nsid_idx")

	if err := db.MigrateFiles(ctx, cfg, db.MigrateDown, io.Discard, files); err != nil {
		t.Fatalf("second roll back of %s: %v", dropUnusedIndexesMigration, err)
	}
	second := indexDefinitions(t, ctx, pool, []string{
		"photos_last_update_idx",
		"photos_owner_nsid_idx",
	})
	for name, def := range first {
		if second[name] != def {
			t.Fatalf("pg_get_indexdef(%s) after Down = %q, want exact prior Down definition %q", name, second[name], def)
		}
	}

	if err := db.MigrateFiles(ctx, cfg, db.MigrateUp, io.Discard, files); err != nil {
		t.Fatalf("leave schema with %s applied: %v", dropUnusedIndexesMigration, err)
	}
}

// TestNoForeignKeyLosesItsSupportingIndex checks that every foreign key on a
// table with at least 1,000 rows still has an index whose leading column is the
// referencing column after the unused-index drop.
func TestNoForeignKeyLosesItsSupportingIndex(t *testing.T) {
	ctx, cfg, pool := prepareMigrationTest(t)
	_ = cfg
	seedLargePhotosTable(t, ctx, pool, 1000)

	rows, err := pool.Query(ctx, `
WITH large_tables AS (
    SELECT c.oid AS relid, c.relname
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace AND n.nspname = 'public'
    WHERE c.relkind IN ('r', 'p')
      AND c.reltuples >= 1000
),
fks AS (
    SELECT
        lt.relname AS table_name,
        con.conname AS constraint_name,
        (SELECT a.attname FROM pg_attribute a
         WHERE a.attrelid = con.conrelid AND a.attnum = con.conkey[1]) AS fk_column
    FROM large_tables lt
    JOIN pg_constraint con ON con.conrelid = lt.relid AND con.contype = 'f'
)
SELECT f.table_name, f.constraint_name, f.fk_column
FROM fks f
WHERE NOT EXISTS (
    SELECT 1
    FROM pg_index x
    JOIN pg_attribute a ON a.attrelid = x.indrelid AND a.attnum = x.indkey[0]
    JOIN pg_class c ON c.oid = x.indrelid
    WHERE c.relname = f.table_name
      AND a.attname = f.fk_column
      AND x.indisvalid
)
ORDER BY f.table_name, f.constraint_name`)
	if err != nil {
		t.Fatalf("query foreign keys without a supporting index: %v", err)
	}
	defer rows.Close()

	var missing []string
	for rows.Next() {
		var tableName, constraintName, fkColumn string
		if err := rows.Scan(&tableName, &constraintName, &fkColumn); err != nil {
			t.Fatalf("scan unsupported foreign key: %v", err)
		}
		missing = append(missing, fmt.Sprintf("%s.%s (%s)", tableName, constraintName, fkColumn))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read unsupported foreign keys: %v", err)
	}
	if len(missing) > 0 {
		t.Fatalf("foreign keys on tables with 1000+ rows lost their supporting index: %v", missing)
	}
}

func assertDroppedIndexAbsent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM pg_class i
		JOIN pg_namespace n ON n.oid = i.relnamespace AND n.nspname = 'public'
		WHERE i.relkind IN ('i', 'I') AND i.relname = $1
	)`, name).Scan(&exists); err != nil {
		t.Fatalf("lookup index %s: %v", name, err)
	}
	if exists {
		t.Fatalf("index %s still present after drop migration", name)
	}
}

func indexDefinitions(t *testing.T, ctx context.Context, pool *pgxpool.Pool, names []string) map[string]string {
	t.Helper()
	out := make(map[string]string, len(names))
	for _, name := range names {
		var def string
		// Partitioned parent indexes use relkind 'I'; leaf indexes use 'i'.
		err := pool.QueryRow(ctx, `SELECT pg_get_indexdef(i.oid)
			FROM pg_class i
			JOIN pg_namespace n ON n.oid = i.relnamespace AND n.nspname = 'public'
			WHERE i.relkind IN ('i', 'I') AND i.relname = $1`, name).Scan(&def)
		if err != nil {
			t.Fatalf("pg_get_indexdef(%s): %v", name, err)
		}
		out[name] = def
	}
	return out
}

func seedLargePhotosTable(t *testing.T, ctx context.Context, pool *pgxpool.Pool, n int) {
	t.Helper()
	const owner = "jooxf371-fk-owner"
	if _, err := pool.Exec(ctx, `INSERT INTO people (nsid, username) VALUES ($1, 'fk-owner')
		ON CONFLICT (nsid) DO NOTHING`, owner); err != nil {
		t.Fatalf("seed people for FK check: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO photos (id, owner_nsid, last_update, visibility, raw, content_hash)
		SELECT 371000000000000000 + g, $1, now(), 'public', '{}'::jsonb, decode(repeat('ab', 32), 'hex')
		FROM generate_series(1, $2) AS g
		ON CONFLICT (id) DO NOTHING`, owner, n); err != nil {
		t.Fatalf("seed %d photos for FK check: %v", n, err)
	}
	// Partitioned parents report reltuples from ANALYZE; refresh before the check.
	if _, err := pool.Exec(ctx, `ANALYZE photos`); err != nil {
		t.Fatalf("analyze photos: %v", err)
	}
}
