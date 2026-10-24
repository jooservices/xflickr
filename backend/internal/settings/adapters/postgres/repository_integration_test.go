package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/settings/adapters/postgres"
	"github.com/jooservices/xflickr/internal/settings/app"
	"github.com/jooservices/xflickr/internal/settings/domain"
)

const (
	integrationUserA = int64(860001)
	integrationUserB = int64(860002)
)

func TestSettingsTablesSeedPersistenceAndChangeEvent(t *testing.T) {
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	deleteUsers(t, pool)
	var startID int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(MAX(id), 0) FROM outbox_events`).Scan(&startID); err != nil {
		t.Fatalf("read outbox cursor: %v", err)
	}
	t.Cleanup(func() {
		restoreApplicationSettings(t, pool)
		deleteUsers(t, pool)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM outbox_events WHERE topic = $1 AND id > $2`, domain.ChangedTopic, startID); err != nil {
			t.Errorf("delete settings events: %v", err)
		}
	})

	t.Run("tables and seed", func(t *testing.T) {
		assertTable(t, pool, "app_settings", []column{
			{"key", "text", "NO"},
			{"value", "jsonb", "NO"},
			{"user_overridable", "boolean", "NO"},
			{"min", "double precision", "YES"},
			{"max", "double precision", "YES"},
		}, []string{"key"})
		assertTable(t, pool, "user_settings", []column{
			{"user_id", "bigint", "NO"},
			{"key", "text", "NO"},
			{"value", "jsonb", "NO"},
		}, []string{"user_id", "key"})
		assertForeignKey(t, pool)
		assertSeed(t, pool)
	})

	t.Run("override outside cap", func(t *testing.T) {
		repository := newRepository(t, pool)
		service := app.New(repository)
		err := service.SetUser(ctx, integrationUserA, domain.MaxDepth, json.RawMessage("5"))
		var validation *domain.ValidationError
		if !errors.As(err, &validation) || validation.Key != domain.MaxDepth || validation.Cap != "4" {
			t.Fatalf("SetUser() error = %v, want max_depth cap 4", err)
		}
		if !containsAll(err.Error(), "max_depth", "4") {
			t.Fatalf("SetUser() error = %q, want the key and cap", err)
		}
		assertUserRows(t, pool, integrationUserA, 0)
		assertEventCount(t, pool, startID, 0)
	})

	t.Run("persisted override and change event", func(t *testing.T) {
		repository := newRepository(t, pool)
		reader := app.New(repository)
		if _, err := reader.Effective(ctx, integrationUserA); err != nil {
			t.Fatalf("reader Effective() error = %v", err)
		}
		writer := app.New(repository)
		if err := writer.SetUser(ctx, integrationUserA, domain.MaxDepth, json.RawMessage("4")); err != nil {
			t.Fatalf("SetUser() error = %v", err)
		}
		updated, err := writer.Effective(ctx, integrationUserA)
		if err != nil {
			t.Fatalf("writer Effective() error = %v", err)
		}
		if updated.MaxDepth != 4 || updated.ChangeCheckInterval != 24*time.Hour || updated.DailyCallLimit != 0 {
			t.Fatalf("writer effective = %+v", updated)
		}
		stale, err := reader.Effective(ctx, integrationUserA)
		if err != nil {
			t.Fatalf("stale Effective() error = %v", err)
		}
		if stale.MaxDepth != 2 {
			t.Fatalf("cached reader depth = %d, want the value from before the write", stale.MaxDepth)
		}
		fresh, err := app.New(repository).Effective(ctx, integrationUserA)
		if err != nil {
			t.Fatalf("fresh Effective() error = %v", err)
		}
		if fresh.MaxDepth != 4 || fresh.QuotaReserve != 100 || fresh.GroupPoolsEnabled {
			t.Fatalf("fresh effective = %+v", fresh)
		}
		other, err := app.New(repository).Effective(ctx, integrationUserB)
		if err != nil {
			t.Fatalf("other Effective() error = %v", err)
		}
		if other.MaxDepth != 2 {
			t.Fatalf("other user depth = %d, want the application default", other.MaxDepth)
		}

		if err := writer.SetUser(ctx, integrationUserA, domain.MaxDepth, json.RawMessage("3")); err != nil {
			t.Fatalf("upsert SetUser() error = %v", err)
		}
		assertUserValue(t, pool, integrationUserA, domain.MaxDepth, "3")
		events := eventsSince(t, pool, startID)
		if len(events) != 2 {
			t.Fatalf("events = %d, want the insert and the upsert", len(events))
		}
		assertUserChange(t, events[0], integrationUserA, "4")
		assertUserChange(t, events[1], integrationUserA, "3")
	})

	t.Run("invalid write rolls back", func(t *testing.T) {
		repository := newRepository(t, pool)
		before := eventCount(t, pool, startID)
		err := repository.SetUserOverride(ctx, integrationUserB, domain.MaxDepth, json.RawMessage("not-json"))
		var validation *domain.ValidationError
		if !errors.As(err, &validation) || validation.Key != domain.MaxDepth || !containsAll(err.Error(), "valid JSON") {
			t.Fatalf("invalid JSON error = %v, want a validation error", err)
		}
		_, err = pool.Exec(ctx, `INSERT INTO user_settings (user_id, key, value) VALUES ($1, 'max_depth', 'not-json'::jsonb)`, integrationUserB)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "22P02" {
			t.Fatalf("invalid JSON error = %v, want SQLSTATE 22P02", err)
		}
		err = repository.SetUserOverride(ctx, integrationUserB, "not_a_setting", json.RawMessage("1"))
		if !errors.As(err, &validation) || validation.Key != "not_a_setting" || !containsAll(err.Error(), "not a registered setting key") {
			t.Fatalf("unknown key error = %v, want a validation error", err)
		}
		_, err = pool.Exec(ctx, `INSERT INTO user_settings (user_id, key, value) VALUES ($1, 'not_a_setting', '1'::jsonb)`, integrationUserB)
		if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
			t.Fatalf("unknown key error = %v, want SQLSTATE 23503", err)
		}
		_, err = pool.Exec(ctx, `
			INSERT INTO app_settings (key, value, user_overridable, min, max)
			VALUES ('cap_order_probe', '1'::jsonb, TRUE, 5, 1)`)
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("unordered caps error = %v, want SQLSTATE 23514", err)
		}
		assertUserRows(t, pool, integrationUserB, 0)
		var probes int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM app_settings WHERE key = 'cap_order_probe'`).Scan(&probes); err != nil {
			t.Fatalf("count cap probe: %v", err)
		}
		if probes != 0 || eventCount(t, pool, startID) != before {
			t.Fatalf("probes/events = %d/%d, want no partial write", probes, eventCount(t, pool, startID))
		}
	})

	t.Run("application change event", func(t *testing.T) {
		repository := newRepository(t, pool)
		service := app.New(repository)
		loaded, err := repository.Load(ctx, integrationUserA)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		setting := loaded.Settings[domain.QuotaReserve]
		setting.Value = json.RawMessage("250")
		minimum, maximum := 0.0, 1000.0
		setting.Min = &minimum
		setting.Max = &maximum
		if err := service.SetApplicationSetting(ctx, setting); err != nil {
			t.Fatalf("SetApplicationSetting() error = %v", err)
		}
		got, err := app.New(repository).Effective(ctx, integrationUserA)
		if err != nil {
			t.Fatalf("Effective() error = %v", err)
		}
		if got.QuotaReserve != 250 || got.MaxDepth != 3 {
			t.Fatalf("effective after application change = %+v", got)
		}
		var payload []byte
		var eventKey string
		if err := pool.QueryRow(ctx, `
			SELECT key, payload
			FROM outbox_events
			WHERE topic = $1 AND id > $2 AND key = 'application:quota_reserve'
			ORDER BY id DESC
			LIMIT 1`, domain.ChangedTopic, startID).Scan(&eventKey, &payload); err != nil {
			t.Fatalf("read application event: %v", err)
		}
		var change domain.Change
		if err := json.Unmarshal(payload, &change); err != nil {
			t.Fatalf("unmarshal application event: %v", err)
		}
		if eventKey != "application:quota_reserve" || change.Scope != domain.ScopeApplication || change.Key != domain.QuotaReserve || change.UserID != nil || string(change.Value) != "250" || change.Min == nil || *change.Min != 0 || change.Max == nil || *change.Max != 1000 {
			t.Fatalf("application change key/payload = %s/%+v", eventKey, change)
		}
	})

	t.Run("setting that is not user overridable", func(t *testing.T) {
		repository := newRepository(t, pool)
		service := app.New(repository)
		loaded, err := repository.Load(ctx, integrationUserA)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		setting := loaded.Settings[domain.GroupPoolsEnabled]
		setting.UserOverridable = false
		if err := service.SetApplicationSetting(ctx, setting); err != nil {
			t.Fatalf("SetApplicationSetting() error = %v", err)
		}
		err = service.SetUser(ctx, integrationUserA, domain.GroupPoolsEnabled, json.RawMessage("true"))
		var validation *domain.ValidationError
		if !errors.As(err, &validation) || validation.Key != domain.GroupPoolsEnabled || !containsAll(err.Error(), "group_pools_enabled", "not user-overridable") {
			t.Fatalf("SetUser() error = %v", err)
		}
		var rows int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM user_settings WHERE user_id = $1 AND key = 'group_pools_enabled'`, integrationUserA).Scan(&rows); err != nil {
			t.Fatalf("count locked override: %v", err)
		}
		if rows != 0 {
			t.Fatalf("locked override rows = %d", rows)
		}
	})

	t.Run("stale override after a tighter cap", func(t *testing.T) {
		repository := newRepository(t, pool)
		service := app.New(repository)
		if err := service.SetUser(ctx, integrationUserA, domain.MaxDepth, json.RawMessage("4")); err != nil {
			t.Fatalf("SetUser(4) error = %v", err)
		}
		loaded, err := repository.Load(ctx, integrationUserA)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		setting := loaded.Settings[domain.MaxDepth]
		maximum := 3.0
		setting.Max = &maximum
		setting.Value = json.RawMessage("2")
		if err := service.SetApplicationSetting(ctx, setting); err != nil {
			t.Fatalf("SetApplicationSetting() error = %v", err)
		}
		t.Cleanup(func() { restoreApplicationSettings(t, pool) })

		assertUserValue(t, pool, integrationUserA, domain.MaxDepth, "4")
		got, err := app.New(repository).Effective(ctx, integrationUserA)
		if err != nil {
			t.Fatalf("Effective() error = %v, want the application default for a stale override", err)
		}
		if got.MaxDepth != 2 {
			t.Fatalf("depth = %d, want the application default after the cap dropped below the override", got.MaxDepth)
		}
		err = service.SetUser(ctx, integrationUserA, domain.MaxDepth, json.RawMessage("4"))
		var validation *domain.ValidationError
		if !errors.As(err, &validation) || validation.Key != domain.MaxDepth || validation.Cap != "3" {
			t.Fatalf("SetUser() error = %v, want max_depth cap 3", err)
		}
		assertUserValue(t, pool, integrationUserA, domain.MaxDepth, "4")
	})

	t.Run("concurrent cap change cannot persist an override outside the cap", func(t *testing.T) {
		restoreApplicationSettings(t, pool)
		repository := newRepository(t, pool)
		service := app.New(repository)
		lockCtx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
		defer cancel()

		conn, err := pgx.ConnectConfig(lockCtx, pool.Config().ConnConfig.Copy())
		if err != nil {
			t.Fatalf("open lock connection: %v", err)
		}
		defer conn.Close(context.Background())
		tx, err := conn.Begin(lockCtx)
		if err != nil {
			t.Fatalf("begin lock: %v", err)
		}
		defer tx.Rollback(lockCtx)
		if _, err := tx.Exec(lockCtx, `SELECT key FROM app_settings WHERE key = $1 FOR UPDATE`, string(domain.MaxDepth)); err != nil {
			t.Fatalf("lock max_depth: %v", err)
		}

		done := make(chan error, 1)
		go func() {
			done <- service.SetUser(lockCtx, integrationUserB, domain.MaxDepth, json.RawMessage("4"))
		}()
		if !waitForAppSettingLock(t, pool) {
			cancel()
			<-done
			t.Fatal("user override write did not wait on the application setting row")
		}
		if _, err := tx.Exec(lockCtx, `UPDATE app_settings SET value = '2'::jsonb, max = 3 WHERE key = $1`, string(domain.MaxDepth)); err != nil {
			t.Fatalf("tighten cap: %v", err)
		}
		if err := tx.Commit(lockCtx); err != nil {
			t.Fatalf("commit cap: %v", err)
		}

		err = <-done
		var validation *domain.ValidationError
		if !errors.As(err, &validation) || validation.Key != domain.MaxDepth || validation.Cap != "3" {
			t.Fatalf("SetUser() error = %v, want max_depth cap 3", err)
		}
		assertUserRows(t, pool, integrationUserB, 0)
		restoreApplicationSettings(t, pool)
	})
}

func waitForAppSettingLock(t *testing.T, pool *pgxpool.Pool) bool {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		err := pool.QueryRow(t.Context(), `
			SELECT count(*) FROM (
				SELECT l.pid
				FROM pg_locks l
				WHERE l.relation = 'public.app_settings'::regclass
				  AND NOT l.granted
				UNION
				SELECT a.pid
				FROM pg_stat_activity a
				WHERE a.wait_event_type = 'Lock'
				  AND a.query LIKE '%FOR SHARE%'
				  AND a.pid <> pg_backend_pid()
			) waits`).Scan(&waiting)
		if err != nil {
			t.Fatalf("read lock waits: %v", err)
		}
		if waiting > 0 {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source file")
	}
	worktreeRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../"))
	dbConfig, err := config.LoadDatabaseWithDotEnv(filepath.Join(worktreeRoot, ".env"))
	if err != nil {
		t.Fatalf("load test PostgreSQL settings: %v", err)
	}
	if err := dbtest.ValidateDatabaseName(dbConfig.Name); err != nil {
		t.Fatal(err)
	}
	return dbtest.Open(t, dbConfig)
}

func newRepository(t *testing.T, pool *pgxpool.Pool) *postgres.Repository {
	t.Helper()
	repository, err := postgres.New(pool)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return repository
}

type column struct {
	name     string
	dataType string
	nullable string
}

func assertTable(t *testing.T, pool *pgxpool.Pool, table string, want []column, primaryKey []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	rows, err := pool.Query(ctx, `
		SELECT column_name, data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1
		ORDER BY ordinal_position`, table)
	if err != nil {
		t.Fatalf("read %s columns: %v", table, err)
	}
	defer rows.Close()
	var got []column
	for rows.Next() {
		var item column
		if err := rows.Scan(&item.name, &item.dataType, &item.nullable); err != nil {
			t.Fatalf("scan %s column: %v", table, err)
		}
		got = append(got, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s columns: %v", table, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s columns = %+v, want %+v", table, got, want)
	}

	keyRows, err := pool.Query(ctx, `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY (i.indkey)
		WHERE i.indrelid = $1::regclass AND i.indisprimary
		ORDER BY array_position(i.indkey, a.attnum)`, "public."+table)
	if err != nil {
		t.Fatalf("read %s primary key: %v", table, err)
	}
	defer keyRows.Close()
	var gotKey []string
	for keyRows.Next() {
		var name string
		if err := keyRows.Scan(&name); err != nil {
			t.Fatalf("scan %s primary key: %v", table, err)
		}
		gotKey = append(gotKey, name)
	}
	if err := keyRows.Err(); err != nil {
		t.Fatalf("iterate %s primary key: %v", table, err)
	}
	if !reflect.DeepEqual(gotKey, primaryKey) {
		t.Fatalf("%s primary key = %v, want %v", table, gotKey, primaryKey)
	}
}

func assertForeignKey(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var definition string
	if err := pool.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conrelid = 'public.user_settings'::regclass AND contype = 'f'`).Scan(&definition); err != nil {
		t.Fatalf("read user_settings foreign key: %v", err)
	}
	if !containsAll(definition, "app_settings", "ON DELETE CASCADE") {
		t.Fatalf("foreign key = %s", definition)
	}
}

func assertSeed(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	rows, err := pool.Query(ctx, `SELECT key, value, user_overridable, min, max FROM app_settings ORDER BY key`)
	if err != nil {
		t.Fatalf("read application settings: %v", err)
	}
	defer rows.Close()
	type stored struct {
		value       []byte
		overridable bool
		minimum     *float64
		maximum     *float64
	}
	got := map[string]stored{}
	for rows.Next() {
		var key string
		var item stored
		if err := rows.Scan(&key, &item.value, &item.overridable, &item.minimum, &item.maximum); err != nil {
			t.Fatalf("scan application setting: %v", err)
		}
		got[key] = item
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate application settings: %v", err)
	}
	minimum, maximum := 1.0, 4.0
	want := map[string]struct {
		value string
		min   *float64
		max   *float64
	}{
		"max_depth":             {value: "2", min: &minimum, max: &maximum},
		"change_check_interval": {value: "86400"},
		"daily_call_limit":      {value: "0"},
		"quota_reserve":         {value: "100"},
		"per_second_rate":       {value: "4"},
		"burst":                 {value: "5"},
		"person_lane_share":     {value: "0.30"},
		"zip_ttl":               {value: "86400"},
		"retry_max":             {value: "5"},
		"retry_backoff":         {value: "30"},
		"node_cooldown":         {value: "2700"},
		"group_pools_enabled":   {value: "false"},
	}
	if len(got) != len(want) {
		t.Fatalf("seeded settings = %d, want %d", len(got), len(want))
	}
	for key, expected := range want {
		item, ok := got[key]
		if !ok || !item.overridable || !jsonEqual(item.value, expected.value) || !floatPtrEqual(item.minimum, expected.min) || !floatPtrEqual(item.maximum, expected.max) {
			t.Fatalf("seed %s = value %s overridable %v min %v max %v", key, item.value, item.overridable, item.minimum, item.maximum)
		}
	}

	repository := newRepository(t, pool)
	effective, err := app.New(repository).Effective(ctx, integrationUserB)
	if err != nil {
		t.Fatalf("Effective(defaults) error = %v", err)
	}
	wantEffective := domain.Effective{
		MaxDepth:            2,
		ChangeCheckInterval: 24 * time.Hour,
		DailyCallLimit:      0,
		QuotaReserve:        100,
		PerSecondRate:       4,
		Burst:               5,
		PersonLaneShare:     0.30,
		ZipTTL:              24 * time.Hour,
		RetryMax:            5,
		RetryBackoff:        30 * time.Second,
		NodeCooldown:        45 * time.Minute,
		GroupPoolsEnabled:   false,
	}
	if effective != wantEffective {
		t.Fatalf("effective seed = %+v, want %+v", effective, wantEffective)
	}
}

func assertUserRows(t *testing.T, pool *pgxpool.Pool, userID int64, want int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var got int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM user_settings WHERE user_id = $1`, userID).Scan(&got); err != nil {
		t.Fatalf("count user settings: %v", err)
	}
	if got != want {
		t.Fatalf("user %d settings = %d, want %d", userID, got, want)
	}
}

func assertUserValue(t *testing.T, pool *pgxpool.Pool, userID int64, key domain.Key, want string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var value []byte
	if err := pool.QueryRow(ctx, `SELECT value FROM user_settings WHERE user_id = $1 AND key = $2`, userID, string(key)).Scan(&value); err != nil {
		t.Fatalf("read user setting %s: %v", key, err)
	}
	if !jsonEqual(value, want) {
		t.Fatalf("user setting %s = %s, want %s", key, value, want)
	}
}

type storedEvent struct {
	key     string
	payload []byte
}

func eventsSince(t *testing.T, pool *pgxpool.Pool, startID int64) []storedEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	rows, err := pool.Query(ctx, `
		SELECT key, payload
		FROM outbox_events
		WHERE topic = $1 AND id > $2
		ORDER BY id`, domain.ChangedTopic, startID)
	if err != nil {
		t.Fatalf("read settings events: %v", err)
	}
	defer rows.Close()
	var events []storedEvent
	for rows.Next() {
		var event storedEvent
		if err := rows.Scan(&event.key, &event.payload); err != nil {
			t.Fatalf("scan settings event: %v", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate settings events: %v", err)
	}
	return events
}

func eventCount(t *testing.T, pool *pgxpool.Pool, startID int64) int {
	t.Helper()
	return len(eventsSince(t, pool, startID))
}

func assertEventCount(t *testing.T, pool *pgxpool.Pool, startID int64, want int) {
	t.Helper()
	if got := eventCount(t, pool, startID); got != want {
		t.Fatalf("settings events = %d, want %d", got, want)
	}
}

func assertUserChange(t *testing.T, event storedEvent, userID int64, wantValue string) {
	t.Helper()
	var change domain.Change
	if err := json.Unmarshal(event.payload, &change); err != nil {
		t.Fatalf("unmarshal %s: %v", event.key, err)
	}
	wantKey := fmt.Sprintf("user:%d:%s", userID, domain.MaxDepth)
	if event.key != wantKey || change.Key != domain.MaxDepth || change.Scope != domain.ScopeUser || change.UserID == nil || *change.UserID != userID || !jsonEqual(change.Value, wantValue) {
		t.Fatalf("event %s = %+v", event.key, change)
	}
}

func restoreApplicationSettings(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, setting := range domain.Defaults() {
		tag, err := pool.Exec(ctx, `
			UPDATE app_settings
			SET value = $2::jsonb, user_overridable = $3, min = $4, max = $5
			WHERE key = $1`,
			string(setting.Key), string(setting.Value), setting.UserOverridable, setting.Min, setting.Max)
		if err != nil {
			t.Errorf("restore %s: %v", setting.Key, err)
			continue
		}
		if tag.RowsAffected() != 1 {
			t.Errorf("restore %s affected %d rows", setting.Key, tag.RowsAffected())
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM app_settings WHERE key = 'cap_order_probe'`); err != nil {
		t.Errorf("delete cap probe: %v", err)
	}
}

func deleteUsers(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `DELETE FROM user_settings WHERE user_id IN ($1, $2)`, integrationUserA, integrationUserB); err != nil {
		t.Errorf("delete user settings: %v", err)
	}
}

func jsonEqual(got []byte, want string) bool {
	var left, right any
	if json.Unmarshal(got, &left) != nil || json.Unmarshal([]byte(want), &right) != nil {
		return false
	}
	return reflect.DeepEqual(left, right)
}

func floatPtrEqual(got, want *float64) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return *got == *want
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
