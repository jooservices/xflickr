package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/migrations"
)

const notificationsMigrationVersion int64 = 20261112100000

func TestNotificationsSchema(t *testing.T) {
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	suffix := time.Now().UnixNano()
	userID := insertNotificationTestUser(t, ctx, pool, fmt.Sprintf("notifications-schema-%d", suffix))
	t.Cleanup(func() { deleteNotificationTestUsers(t, pool, userID) })

	var (
		id        int64
		gotUserID int64
		kind      string
		title     string
		body      string
		level     string
		readAt    *time.Time
		createdAt time.Time
	)
	err := pool.QueryRow(ctx, `
		INSERT INTO notifications (user_id, kind, title, body, level)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, user_id, kind, title, body, level, read_at, created_at`,
		userID, "sync.failed", "Sync failed", "The upload failed.", "warning",
	).Scan(&id, &gotUserID, &kind, &title, &body, &level, &readAt, &createdAt)
	if err != nil {
		t.Fatalf("insert notification: %v", err)
	}
	if id <= 0 || gotUserID != userID || kind != "sync.failed" || title != "Sync failed" ||
		body != "The upload failed." || level != "warning" || readAt != nil || createdAt.IsZero() {
		t.Fatalf("notification fields = (%d, %d, %q, %q, %q, %q, %v, %v)",
			id, gotUserID, kind, title, body, level, readAt, createdAt)
	}

	columns := notificationColumns(t, ctx, pool)
	wantColumns := []notificationColumn{
		{name: "id", dataType: "bigint", nullable: "NO", identity: "YES", generation: "BY DEFAULT"},
		{name: "user_id", dataType: "bigint", nullable: "NO", identity: "NO"},
		{name: "kind", dataType: "text", nullable: "NO", identity: "NO"},
		{name: "title", dataType: "text", nullable: "NO", identity: "NO"},
		{name: "body", dataType: "text", nullable: "NO", identity: "NO"},
		{name: "level", dataType: "text", nullable: "NO", identity: "NO"},
		{name: "read_at", dataType: "timestamp with time zone", nullable: "YES", identity: "NO"},
		{name: "created_at", dataType: "timestamp with time zone", nullable: "NO", identity: "NO"},
	}
	if !reflect.DeepEqual(columns, wantColumns) {
		t.Fatalf("notifications columns = %+v, want %+v", columns, wantColumns)
	}

	var createdAtDefault *string
	if err := pool.QueryRow(ctx, `
		SELECT column_default
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'notifications' AND column_name = 'created_at'`,
	).Scan(&createdAtDefault); err != nil {
		t.Fatalf("read created_at default: %v", err)
	}
	if createdAtDefault == nil || *createdAtDefault != "now()" {
		t.Fatalf("created_at default = %v, want now()", createdAtDefault)
	}

	var indexDefinition string
	if err := pool.QueryRow(ctx, `SELECT pg_get_indexdef('notifications_user_created_id_idx'::regclass)`).Scan(&indexDefinition); err != nil {
		t.Fatalf("read notification ordering index: %v", err)
	}
	if !strings.Contains(indexDefinition, "(user_id, created_at DESC, id DESC)") {
		t.Fatalf("notification ordering index = %q", indexDefinition)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO notifications (user_id, kind, title, body, level)
		VALUES (-1, 'test', 'Invalid user', 'Must be rejected.', 'info')`)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "23503" {
		t.Fatalf("insert notification with missing user error = %v, want foreign-key violation", err)
	}
}

func TestNotificationsCascade(t *testing.T) {
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	suffix := time.Now().UnixNano()
	deletedUserID := insertNotificationTestUser(t, ctx, pool, fmt.Sprintf("notifications-cascade-deleted-%d", suffix))
	retainedUserID := insertNotificationTestUser(t, ctx, pool, fmt.Sprintf("notifications-cascade-retained-%d", suffix))
	t.Cleanup(func() { deleteNotificationTestUsers(t, pool, deletedUserID, retainedUserID) })

	insertTestNotification(t, ctx, pool, deletedUserID)
	insertTestNotification(t, ctx, pool, retainedUserID)
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, deletedUserID); err != nil {
		t.Fatalf("delete notification owner: %v", err)
	}

	var deletedCount, retainedCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE user_id = $1`, deletedUserID).Scan(&deletedCount); err != nil {
		t.Fatalf("count notifications for deleted user: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE user_id = $1`, retainedUserID).Scan(&retainedCount); err != nil {
		t.Fatalf("count notifications for retained user: %v", err)
	}
	if deletedCount != 0 || retainedCount != 1 {
		t.Fatalf("notification counts after user deletion = (%d, %d), want (0, 1)", deletedCount, retainedCount)
	}
}

func TestNotificationsMigrationDownUp(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cfg := notificationTestDatabaseConfig(t)
	cfg = dbtest.EmptyDatabase(t, cfg)
	if err := db.Migrate(ctx, cfg, db.MigrateUp, io.Discard); err != nil {
		t.Fatalf("prepare isolated migration database: %v", err)
	}
	pool, err := db.NewPool(ctx, cfg)
	if err != nil {
		t.Fatalf("open isolated migration database: %v", err)
	}
	t.Cleanup(pool.Close)

	var currentDatabase string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&currentDatabase); err != nil {
		t.Fatalf("read current database: %v", err)
	}
	if currentDatabase != cfg.Name {
		t.Fatal("migration database does not match the integration pool")
	}
	// Migrations added after notifications sit on top of it: roll those back first so that the next rollback is notifications.
	for range migrationsAfter(t, notificationsMigrationVersion) {
		if err := db.Migrate(ctx, cfg, db.MigrateDown, io.Discard); err != nil {
			t.Fatalf("roll back a migration newer than notifications: %v", err)
		}
	}
	var latestVersion int64
	if err := pool.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&latestVersion); err != nil {
		t.Fatalf("read latest applied migration: %v", err)
	}
	if latestVersion != notificationsMigrationVersion {
		t.Fatalf("latest applied migration = %d, want notifications version %d", latestVersion, notificationsMigrationVersion)
	}

	if err := db.Migrate(ctx, cfg, db.MigrateDown, io.Discard); err != nil {
		t.Fatalf("roll back notifications migration: %v", err)
	}
	assertNotificationRelation(t, ctx, pool, "public.notifications", false)
	assertNotificationRelation(t, ctx, pool, "public.notifications_user_created_id_idx", false)

	if err := db.Migrate(ctx, cfg, db.MigrateUp, io.Discard); err != nil {
		t.Fatalf("reapply notifications migration: %v", err)
	}
	assertNotificationRelation(t, ctx, pool, "public.notifications", true)
	assertNotificationRelation(t, ctx, pool, "public.notifications_user_created_id_idx", true)
}

// migrationsAfter returns the versions of the embedded migrations newer than the given version.
func migrationsAfter(t *testing.T, version int64) []int64 {
	t.Helper()
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	var newer []int64
	for _, entry := range entries {
		prefix, _, found := strings.Cut(entry.Name(), "_")
		if !found {
			continue
		}
		number, err := strconv.ParseInt(prefix, 10, 64)
		if err == nil && number > version {
			newer = append(newer, number)
		}
	}
	return newer
}

type notificationColumn struct {
	name       string
	dataType   string
	nullable   string
	identity   string
	generation string
}

func notificationColumns(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []notificationColumn {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT column_name, data_type, is_nullable, is_identity, COALESCE(identity_generation, '')
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'notifications'
		ORDER BY ordinal_position`)
	if err != nil {
		t.Fatalf("read notifications columns: %v", err)
	}
	defer rows.Close()

	var columns []notificationColumn
	for rows.Next() {
		var column notificationColumn
		if err := rows.Scan(&column.name, &column.dataType, &column.nullable, &column.identity, &column.generation); err != nil {
			t.Fatalf("scan notifications column: %v", err)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read notifications columns: %v", err)
	}
	return columns
}

func insertNotificationTestUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, emailPrefix string) int64 {
	t.Helper()
	var userID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, role)
		VALUES ($1, 'unused', 'user')
		RETURNING id`, emailPrefix+"@example.invalid").Scan(&userID); err != nil {
		t.Fatalf("insert notification test user: %v", err)
	}
	return userID
}

func insertTestNotification(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID int64) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO notifications (user_id, kind, title, body, level)
		VALUES ($1, 'test', 'Test', 'Test notification.', 'info')`, userID); err != nil {
		t.Fatalf("insert test notification: %v", err)
	}
}

func deleteNotificationTestUsers(t *testing.T, pool *pgxpool.Pool, userIDs ...int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, userID := range userIDs {
		if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Errorf("delete notification test user: %v", err)
		}
	}
}

func notificationTestDatabaseConfig(t *testing.T) config.DBConfig {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate notification integration test source file")
	}
	worktreeRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(worktreeRoot, ".env"))
	if err != nil {
		t.Fatalf("load test PostgreSQL settings: %v", err)
	}
	if err := dbtest.ValidateDatabaseName(cfg.Name); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func assertNotificationRelation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, relation string, want bool) {
	t.Helper()
	var name *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass($1)`, relation).Scan(&name); err != nil {
		t.Fatalf("lookup relation %s: %v", relation, err)
	}
	if got := name != nil; got != want {
		t.Fatalf("relation %s exists = %t, want %t", relation, got, want)
	}
}
