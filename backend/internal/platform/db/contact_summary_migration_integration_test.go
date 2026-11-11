package db_test

import (
	"context"
	"io"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/db"
	"github.com/jooservices/xflickr/migrations"
)

func TestContactSummaryMigrationBackfillsExistingData(t *testing.T) {
	ctx, cfg, pool := prepareEmptyMigrationTest(t)
	predecessors := migrationsBeforeContactSummary(t)
	if err := db.MigrateFiles(ctx, cfg, db.MigrateUp, io.Discard, predecessors); err != nil {
		t.Fatalf("migrate predecessors: %v", err)
	}
	seedContactSummaryMigrationData(t, ctx, pool)

	contactMigration := fstest.MapFS{}
	sql, err := migrations.Files.ReadFile("20261111170000_contact_screen_summaries.sql")
	if err != nil {
		t.Fatal(err)
	}
	contactMigration["20261111170000_contact_screen_summaries.sql"] = &fstest.MapFile{
		Data: sql,
		Mode: 0o644,
	}
	if err := db.MigrateFiles(ctx, cfg, db.MigrateUp, io.Discard, contactMigration); err != nil {
		t.Fatalf("migrate contact summaries: %v", err)
	}

	var photosCrawled, favoritesCount, sizesCompleted, tombstonesCount int64
	err = pool.QueryRow(ctx, `SELECT photos_crawled, favorites_count, sizes_completed, tombstones_count
		FROM crawl_interests WHERE user_id=42 AND person_nsid='contact-nsid'`).Scan(
		&photosCrawled, &favoritesCount, &sizesCompleted, &tombstonesCount,
	)
	if err != nil {
		t.Fatalf("read backfilled contact summaries: %v", err)
	}
	if photosCrawled != 3 || favoritesCount != 1 || sizesCompleted != 1 || tombstonesCount != 1 {
		t.Fatalf("backfilled summaries = photos %d, favorites %d, sizes %d, tombstones %d; want 3, 1, 1, 1",
			photosCrawled, favoritesCount, sizesCompleted, tombstonesCount)
	}

	var attachedIndexes, validIndexes int
	err = pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE child.indisvalid)
		FROM pg_inherits inherited
		JOIN pg_index parent ON parent.indexrelid=inherited.inhparent
		JOIN pg_index child ON child.indexrelid=inherited.inhrelid
		WHERE parent.indexrelid IN ('photos_owner_recent_idx'::regclass, 'photos_owner_contact_id_idx'::regclass)`).Scan(&attachedIndexes, &validIndexes)
	if err != nil {
		t.Fatalf("read attached contact photo indexes: %v", err)
	}
	if attachedIndexes != 32 || validIndexes != 32 {
		t.Fatalf("attached photo indexes = %d (%d valid); want 32 attached and valid", attachedIndexes, validIndexes)
	}
}

func migrationsBeforeContactSummary(t *testing.T) fstest.MapFS {
	t.Helper()
	const contactMigration = "20261111170000_contact_screen_summaries.sql"
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	files := fstest.MapFS{}
	found := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		if entry.Name() == contactMigration {
			found = true
			continue
		}
		if entry.Name() > contactMigration {
			continue
		}
		data, err := migrations.Files.ReadFile(entry.Name())
		if err != nil {
			t.Fatalf("read migration %s: %v", entry.Name(), err)
		}
		files[entry.Name()] = &fstest.MapFile{Data: data, Mode: 0o644}
	}
	if !found {
		t.Fatalf("embedded migration %s was not found", contactMigration)
	}
	return files
}

func seedContactSummaryMigrationData(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	statements := []string{
		`INSERT INTO users (id, email, password_hash, role) VALUES (42, 'contact-summary@example.invalid', 'hash', 'user')`,
		`INSERT INTO api_keys (id, user_id, label, key_public, secret_sealed, per_hour_limit, per_second_limit, burst)
			VALUES (42, 42, 'test', 'contact-summary-key', '{}'::jsonb, 1000, 10, 10)`,
		`INSERT INTO flickr_accounts (id, user_id, key_id, nsid, username, token_sealed, secret_sealed)
			VALUES (42, 42, 42, 'account-nsid', 'test', '{}'::jsonb, '{}'::jsonb)`,
		`INSERT INTO people (nsid, username) VALUES ('contact-nsid', 'contact')`,
		`INSERT INTO crawl_interests (user_id, person_nsid, depth, via) VALUES (42, 'contact-nsid', 0, 'manual')`,
		`INSERT INTO photos (id, owner_nsid, last_update, visibility, sizes_status, sizes_checked_at, tombstoned_at, raw, content_hash)
			VALUES
				(101, 'contact-nsid', '2025-01-01', 'public', 'confirmed', '2025-02-01', NULL, '{}'::jsonb, decode(repeat('00', 32), 'hex')),
				(102, 'contact-nsid', '2025-01-01', 'public', 'confirmed', '2024-12-01', NULL, '{}'::jsonb, decode(repeat('00', 32), 'hex')),
				(103, 'contact-nsid', '2025-01-01', 'public', 'none', NULL, '2026-01-01', '{}'::jsonb, decode(repeat('00', 32), 'hex')),
				(104, 'contact-nsid', '2025-01-01', 'public', 'confirmed', '2025-02-01', NULL, '{}'::jsonb, decode(repeat('00', 32), 'hex'))`,
		`INSERT INTO photos (id, owner_nsid, last_update, visibility, raw, content_hash)
			SELECT photo_id, 'contact-nsid', '2025-01-01', 'public', '{}'::jsonb, decode(repeat('00', 32), 'hex')
			FROM generate_series(105, 1204) AS photo_id`,
		`INSERT INTO observations (user_id, flickr_account_id, record_kind, record_id)
			VALUES (42, 42, 'photo', '101'), (42, 42, 'photo', '102'), (42, 42, 'photo', '103')`,
		`INSERT INTO favorites (person_nsid, photo_id, fave_date)
			VALUES ('contact-nsid', 101, '2025-02-01'), ('contact-nsid', 104, '2025-02-01')`,
		`INSERT INTO favorites (person_nsid, photo_id, fave_date)
			SELECT 'contact-nsid', photo_id, '2025-02-01' FROM generate_series(105, 1204) AS photo_id`,
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("seed contact summary migration data: %v", err)
		}
	}
}
