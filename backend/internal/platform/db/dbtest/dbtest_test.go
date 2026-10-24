package dbtest

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/config"
)

func TestValidateDatabaseName(t *testing.T) {
	tests := []struct {
		name    string
		allowed bool
	}{
		{name: "xflickr_test", allowed: true},
		{name: "xflickr_wtk80", allowed: true},
		{name: "xflickr_wtfeature1", allowed: true},
		{name: "xflickr", allowed: false},
		{name: "other_test", allowed: false},
		{name: "xflickr_wt", allowed: false},
		{name: "xflickr_wtwith-dash", allowed: false},
		{name: "", allowed: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateDatabaseName(test.name)
			if test.allowed && err != nil {
				t.Fatalf("ValidateDatabaseName(%q) error = %v", test.name, err)
			}
			if !test.allowed && err == nil {
				t.Fatalf("ValidateDatabaseName(%q) succeeded, want error", test.name)
			}
		})
	}
}

func TestOpenPoolMigratesOnlyAnIsolatedDatabase(t *testing.T) {
	cfg := config.DBConfig{Name: "xflickr_wtk80"}
	var created, migrated bool
	pool, err := openPool(context.Background(), cfg, func(_ context.Context, got config.DBConfig) (*pgxpool.Pool, error) {
		created = true
		if got.Name != cfg.Name {
			t.Fatalf("pool factory got database %q", got.Name)
		}
		return newUnconnectedPool(t)
	}, func(_ context.Context, got config.DBConfig) error {
		migrated = true
		if got.Name != cfg.Name {
			t.Fatalf("migrator got database %q", got.Name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("openPool() error = %v", err)
	}
	if !created || !migrated {
		t.Fatalf("openPool() called create=%t migrate=%t", created, migrated)
	}
	pool.Close()
}

func TestOpenPoolRejectsUnsafeDatabaseBeforeConnecting(t *testing.T) {
	created := false
	_, err := openPool(context.Background(), config.DBConfig{Name: "xflickr"}, func(context.Context, config.DBConfig) (*pgxpool.Pool, error) {
		created = true
		return nil, nil
	}, func(context.Context, config.DBConfig) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "refusing database") || created {
		t.Fatalf("openPool() error = %v, create called=%t", err, created)
	}
}

func TestOpenPoolWrapsPoolAndMigrationFailures(t *testing.T) {
	t.Run("pool factory", func(t *testing.T) {
		wantErr := errors.New("pool unavailable")
		_, err := openPool(context.Background(), config.DBConfig{Name: "xflickr_test"}, func(context.Context, config.DBConfig) (*pgxpool.Pool, error) {
			return nil, wantErr
		}, func(context.Context, config.DBConfig) error { return nil })
		if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "open test PostgreSQL pool") {
			t.Fatalf("openPool() error = %v", err)
		}
	})

	t.Run("migration", func(t *testing.T) {
		pool, err := newUnconnectedPool(t)
		if err != nil {
			t.Fatalf("newUnconnectedPool() error = %v", err)
		}
		wantErr := errors.New("migration failed")
		_, err = openPool(context.Background(), config.DBConfig{Name: "xflickr_test"}, func(context.Context, config.DBConfig) (*pgxpool.Pool, error) {
			return pool, nil
		}, func(context.Context, config.DBConfig) error { return wantErr })
		if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "migrate test PostgreSQL database") {
			t.Fatalf("openPool() error = %v", err)
		}
	})
}

func TestProvisionEmptyDatabaseCreatesAndDropsAnIsolatedName(t *testing.T) {
	source := config.DBConfig{Name: "xflickr_wtk80", Host: "db.internal", User: "worker"}
	createAdmin := &scriptedAdmin{}
	dropAdmin := &scriptedAdmin{}
	opened := 0
	open := func(_ context.Context, got config.DBConfig) (databaseAdmin, error) {
		if got.Name != source.Name || got.Host != source.Host {
			t.Fatalf("maintenance connection used database %q host %q", got.Name, got.Host)
		}
		opened++
		if opened == 1 {
			return createAdmin, nil
		}
		return dropAdmin, nil
	}
	const isolatedName = "xflickr_wttexample1"
	isolated, cleanup, err := provisionEmptyDatabase(context.Background(), source, open, constantName(isolatedName))
	if err != nil {
		t.Fatalf("provisionEmptyDatabase() error = %v", err)
	}
	if isolated.Name != isolatedName || isolated.Host != source.Host || isolated.User != source.User {
		t.Fatalf("isolated config = %+v", isolated)
	}
	if createAdmin.closed != 1 || len(createAdmin.statements) != 1 || createAdmin.statements[0] != createDatabaseSQL(isolatedName) {
		t.Fatalf("create statements = %#v, closed = %d", createAdmin.statements, createAdmin.closed)
	}

	if err := cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup() error = %v", err)
	}
	if dropAdmin.closed != 1 || len(dropAdmin.statements) != 1 || dropAdmin.statements[0] != dropDatabaseSQL(isolatedName) {
		t.Fatalf("drop statements = %#v, closed = %d", dropAdmin.statements, dropAdmin.closed)
	}
}

func TestProvisionEmptyDatabaseRejectsUnsafeSourceBeforeConnecting(t *testing.T) {
	opened := false
	_, _, err := provisionEmptyDatabase(context.Background(), config.DBConfig{Name: "xflickr"}, func(context.Context, config.DBConfig) (databaseAdmin, error) {
		opened = true
		return nil, nil
	}, constantName("xflickr_wttexample1"))
	if err == nil || !strings.Contains(err.Error(), "refusing database") || opened {
		t.Fatalf("provisionEmptyDatabase() error = %v, opened = %t", err, opened)
	}
}

func TestProvisionEmptyDatabaseRetriesDuplicateNames(t *testing.T) {
	admin := &scriptedAdmin{errFor: func(call int, _ string) error {
		if call == 0 {
			return &pgconn.PgError{Code: "42P04"}
		}
		return nil
	}}
	names := []string{"xflickr_wtttaken01", "xflickr_wttfresh01"}
	isolated, cleanup, err := provisionEmptyDatabase(context.Background(), config.DBConfig{Name: "xflickr_test"}, func(context.Context, config.DBConfig) (databaseAdmin, error) {
		return admin, nil
	}, func(string) (string, error) {
		name := names[0]
		names = names[1:]
		return name, nil
	})
	if err != nil {
		t.Fatalf("provisionEmptyDatabase() error = %v", err)
	}
	if isolated.Name != "xflickr_wttfresh01" {
		t.Fatalf("isolated database = %q", isolated.Name)
	}
	if len(admin.statements) != 2 || admin.statements[0] != createDatabaseSQL("xflickr_wtttaken01") || admin.statements[1] != createDatabaseSQL("xflickr_wttfresh01") {
		t.Fatalf("create statements = %#v", admin.statements)
	}
	if err := cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup() error = %v", err)
	}
	if admin.statements[len(admin.statements)-1] != dropDatabaseSQL("xflickr_wttfresh01") {
		t.Fatalf("statements = %#v", admin.statements)
	}
}

func TestProvisionEmptyDatabaseStopsOnCreateAndNameErrors(t *testing.T) {
	t.Run("maintenance connection", func(t *testing.T) {
		wantErr := errors.New("admin unavailable")
		_, _, err := provisionEmptyDatabase(context.Background(), config.DBConfig{Name: "xflickr_test"}, func(context.Context, config.DBConfig) (databaseAdmin, error) {
			return nil, wantErr
		}, constantName("xflickr_wttexample1"))
		if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "open database maintenance connection") {
			t.Fatalf("provisionEmptyDatabase() error = %v", err)
		}
	})

	t.Run("name generator", func(t *testing.T) {
		admin := &scriptedAdmin{}
		wantErr := errors.New("name failed")
		_, _, err := provisionEmptyDatabase(context.Background(), config.DBConfig{Name: "xflickr_test"}, func(context.Context, config.DBConfig) (databaseAdmin, error) {
			return admin, nil
		}, func(string) (string, error) { return "", wantErr })
		if !errors.Is(err, wantErr) || admin.closed != 1 || len(admin.statements) != 0 {
			t.Fatalf("provisionEmptyDatabase() error = %v, statements = %#v, closed = %d", err, admin.statements, admin.closed)
		}
	})

	t.Run("invalid generated name", func(t *testing.T) {
		admin := &scriptedAdmin{}
		_, _, err := provisionEmptyDatabase(context.Background(), config.DBConfig{Name: "xflickr_test"}, func(context.Context, config.DBConfig) (databaseAdmin, error) {
			return admin, nil
		}, constantName("xflickr"))
		if err == nil || !strings.Contains(err.Error(), "isolated test database name") || !strings.Contains(err.Error(), "refusing database") || len(admin.statements) != 0 {
			t.Fatalf("provisionEmptyDatabase() error = %v, statements = %#v", err, admin.statements)
		}
	})

	t.Run("matches source database", func(t *testing.T) {
		admin := &scriptedAdmin{}
		_, _, err := provisionEmptyDatabase(context.Background(), config.DBConfig{Name: "xflickr_test"}, func(context.Context, config.DBConfig) (databaseAdmin, error) {
			return admin, nil
		}, constantName("xflickr_test"))
		if err == nil || !strings.Contains(err.Error(), "matches the source database") || len(admin.statements) != 0 {
			t.Fatalf("provisionEmptyDatabase() error = %v, statements = %#v", err, admin.statements)
		}
	})

	t.Run("create permission", func(t *testing.T) {
		admin := &scriptedAdmin{errFor: func(int, string) error {
			return &pgconn.PgError{Code: "42501"}
		}}
		_, _, err := provisionEmptyDatabase(context.Background(), config.DBConfig{Name: "xflickr_test"}, func(context.Context, config.DBConfig) (databaseAdmin, error) {
			return admin, nil
		}, constantName("xflickr_wttexample1"))
		if err == nil || !strings.Contains(err.Error(), "create isolated test database") || len(admin.statements) != 1 {
			t.Fatalf("provisionEmptyDatabase() error = %v, statements = %#v", err, admin.statements)
		}
	})

	t.Run("plain create error", func(t *testing.T) {
		wantErr := errors.New("create failed")
		admin := &scriptedAdmin{errFor: func(int, string) error { return wantErr }}
		_, _, err := provisionEmptyDatabase(context.Background(), config.DBConfig{Name: "xflickr_test"}, func(context.Context, config.DBConfig) (databaseAdmin, error) {
			return admin, nil
		}, constantName("xflickr_wttexample1"))
		if !errors.Is(err, wantErr) || len(admin.statements) != 1 {
			t.Fatalf("provisionEmptyDatabase() error = %v, statements = %#v", err, admin.statements)
		}
	})

	t.Run("duplicate name exhausted", func(t *testing.T) {
		admin := &scriptedAdmin{errFor: func(int, string) error {
			return &pgconn.PgError{Code: "42P04"}
		}}
		_, _, err := provisionEmptyDatabase(context.Background(), config.DBConfig{Name: "xflickr_test"}, func(context.Context, config.DBConfig) (databaseAdmin, error) {
			return admin, nil
		}, constantName("xflickr_wttexample1"))
		var pgErr *pgconn.PgError
		if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "42P04" || len(admin.statements) != isolatedDatabaseAttempts {
			t.Fatalf("provisionEmptyDatabase() error = %v, statements = %d", err, len(admin.statements))
		}
	})
}

func TestProvisionEmptyDatabaseDropErrors(t *testing.T) {
	t.Run("maintenance connection", func(t *testing.T) {
		wantErr := errors.New("admin unavailable")
		opened := 0
		_, cleanup, err := provisionEmptyDatabase(context.Background(), config.DBConfig{Name: "xflickr_test"}, func(context.Context, config.DBConfig) (databaseAdmin, error) {
			opened++
			if opened > 1 {
				return nil, wantErr
			}
			return &scriptedAdmin{}, nil
		}, constantName("xflickr_wttexample1"))
		if err != nil {
			t.Fatalf("provisionEmptyDatabase() error = %v", err)
		}
		if err := cleanup(context.Background()); !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "open database maintenance connection") {
			t.Fatalf("cleanup() error = %v", err)
		}
	})

	t.Run("drop statement", func(t *testing.T) {
		wantErr := errors.New("drop failed")
		admin := &scriptedAdmin{errFor: func(_ int, sql string) error {
			if strings.HasPrefix(sql, "DROP DATABASE") {
				return wantErr
			}
			return nil
		}}
		_, cleanup, err := provisionEmptyDatabase(context.Background(), config.DBConfig{Name: "xflickr_test"}, func(context.Context, config.DBConfig) (databaseAdmin, error) {
			return admin, nil
		}, constantName("xflickr_wttexample1"))
		if err != nil {
			t.Fatalf("provisionEmptyDatabase() error = %v", err)
		}
		if err := cleanup(context.Background()); !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "drop isolated test database") {
			t.Fatalf("cleanup() error = %v", err)
		}
	})
}

func TestOpenMigratedClosesThePoolWhenTheTestEnds(t *testing.T) {
	cfg := config.DBConfig{Name: "xflickr_wtk80"}
	var created config.DBConfig
	var migrated config.DBConfig
	t.Run("open", func(t *testing.T) {
		pool := openMigrated(t, cfg, func(_ context.Context, got config.DBConfig) (*pgxpool.Pool, error) {
			created = got
			return newUnconnectedPool(t)
		}, func(_ context.Context, got config.DBConfig) error {
			migrated = got
			return nil
		})
		if pool == nil {
			t.Fatal("openMigrated() returned no pool")
		}
	})
	if created.Name != cfg.Name || migrated.Name != cfg.Name {
		t.Fatalf("created database %q, migrated database %q", created.Name, migrated.Name)
	}
}

func TestOpenMigratedFatalsBeforeConnectingToALiveDatabase(t *testing.T) {
	tb := runRecordingTB(func(tb testing.TB) {
		openMigrated(tb, config.DBConfig{Name: "xflickr"}, func(context.Context, config.DBConfig) (*pgxpool.Pool, error) {
			return nil, errors.New("pool should not be created")
		}, func(context.Context, config.DBConfig) error { return nil })
	})
	if !strings.Contains(tb.fatal, "open migrated test PostgreSQL database") || !strings.Contains(tb.fatal, "refusing database") {
		t.Fatalf("fatal = %q", tb.fatal)
	}
}

func TestEmptyDatabaseDropsOnCleanup(t *testing.T) {
	admin := &scriptedAdmin{}
	source := config.DBConfig{Name: "xflickr_test", Host: "db.internal"}
	var got config.DBConfig
	t.Run("create", func(t *testing.T) {
		got = emptyDatabase(t, source, func(context.Context, config.DBConfig) (databaseAdmin, error) {
			return admin, nil
		}, constantName("xflickr_wttexample1"))
	})
	if got.Name != "xflickr_wttexample1" || got.Host != source.Host {
		t.Fatalf("isolated config name = %q host = %q", got.Name, got.Host)
	}
	if len(admin.statements) != 2 || admin.statements[0] != createDatabaseSQL("xflickr_wttexample1") || admin.statements[1] != dropDatabaseSQL("xflickr_wttexample1") {
		t.Fatalf("statements = %#v", admin.statements)
	}
}

func TestEmptyDatabaseFatalsWhenCreateFails(t *testing.T) {
	tb := runRecordingTB(func(tb testing.TB) {
		emptyDatabase(tb, config.DBConfig{Name: "not-a-test-database"}, func(context.Context, config.DBConfig) (databaseAdmin, error) {
			return nil, errors.New("admin should not be opened")
		}, constantName("xflickr_wttexample1"))
	})
	if !strings.Contains(tb.fatal, "create isolated test database") || !strings.Contains(tb.fatal, "refusing database") {
		t.Fatalf("fatal = %q", tb.fatal)
	}
}

func TestEmptyDatabaseReportsDropFailure(t *testing.T) {
	wantErr := errors.New("drop failed")
	tb := &recordingTB{}
	got := emptyDatabase(tb, config.DBConfig{Name: "xflickr_test"}, func(context.Context, config.DBConfig) (databaseAdmin, error) {
		return &scriptedAdmin{errFor: func(_ int, sql string) error {
			if strings.HasPrefix(sql, "DROP DATABASE") {
				return wantErr
			}
			return nil
		}}, nil
	}, constantName("xflickr_wttexample1"))
	if got.Name != "xflickr_wttexample1" {
		t.Fatalf("isolated database = %q", got.Name)
	}
	if len(tb.cleanups) != 1 {
		t.Fatalf("cleanups = %d", len(tb.cleanups))
	}
	tb.cleanups[0]()
	if len(tb.errors) != 1 || !strings.Contains(tb.errors[0], "drop isolated test database") || !strings.Contains(tb.errors[0], wantErr.Error()) {
		t.Fatalf("cleanup errors = %#v", tb.errors)
	}
}

func TestRandomWorktreeDatabaseName(t *testing.T) {
	seen := make(map[string]bool)
	for range 20 {
		name, err := randomWorktreeDatabaseName("xflickr_wtk80")
		if err != nil {
			t.Fatalf("randomWorktreeDatabaseName() error = %v", err)
		}
		if err := ValidateDatabaseName(name); err != nil {
			t.Fatalf("ValidateDatabaseName(%q) error = %v", name, err)
		}
		if !strings.HasPrefix(name, "xflickr_wtt") || name == "xflickr_wtk80" || seen[name] {
			t.Fatalf("generated name = %q", name)
		}
		seen[name] = true
	}
}

func TestNewRandomDatabaseNameReportsReaderAndCollisionFailures(t *testing.T) {
	t.Run("reader", func(t *testing.T) {
		_, err := newRandomDatabaseName("xflickr_test", errReader{})
		if err == nil || !strings.Contains(err.Error(), "generate isolated test database name") {
			t.Fatalf("newRandomDatabaseName() error = %v", err)
		}
	})

	t.Run("collisions", func(t *testing.T) {
		excluded := "xflickr_wtt" + hex.EncodeToString(make([]byte, 4))
		reader := bytes.NewReader(bytes.Repeat([]byte{0}, 4*isolatedDatabaseAttempts))
		_, err := newRandomDatabaseName(excluded, reader)
		if err == nil || !strings.Contains(err.Error(), "exhausted attempts") {
			t.Fatalf("newRandomDatabaseName() error = %v", err)
		}
	})
}

func TestOpenDatabaseAdminReturnsConnectionError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := openDatabaseAdmin(ctx, config.DBConfig{
		Host:              "127.0.0.1",
		Port:              1,
		Name:              "xflickr_test",
		User:              "postgres",
		SSLMode:           "disable",
		MaxConns:          1,
		MinConns:          0,
		MaxConnLifetime:   time.Second,
		MaxConnIdleTime:   time.Second,
		HealthCheckPeriod: time.Second,
	})
	if err == nil {
		t.Fatal("openDatabaseAdmin() succeeded without a PostgreSQL server")
	}
}

func TestIsDuplicateDatabase(t *testing.T) {
	if isDuplicateDatabase(nil) || isDuplicateDatabase(errors.New("create failed")) || isDuplicateDatabase(&pgconn.PgError{Code: "42501"}) {
		t.Fatal("isDuplicateDatabase() reported a non-duplicate error")
	}
	if !isDuplicateDatabase(&pgconn.PgError{Code: "42P04"}) {
		t.Fatal("isDuplicateDatabase() missed duplicate_database")
	}
}

type scriptedAdmin struct {
	statements []string
	errFor     func(call int, sql string) error
	calls      int
	closed     int
}

func (a *scriptedAdmin) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	a.statements = append(a.statements, sql)
	var err error
	if a.errFor != nil {
		err = a.errFor(a.calls, sql)
	}
	a.calls++
	return pgconn.CommandTag{}, err
}

func (a *scriptedAdmin) Close() { a.closed++ }

func constantName(name string) databaseNamer {
	return func(string) (string, error) { return name, nil }
}

func createDatabaseSQL(name string) string {
	return "CREATE DATABASE " + pgx.Identifier{name}.Sanitize()
}

func dropDatabaseSQL(name string) string {
	return "DROP DATABASE IF EXISTS " + pgx.Identifier{name}.Sanitize() + " WITH (FORCE)"
}

type recordingTB struct {
	testing.TB
	fatal    string
	errors   []string
	cleanups []func()
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Fatalf(format string, args ...any) {
	r.fatal = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

func (r *recordingTB) Errorf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

func (r *recordingTB) Cleanup(fn func()) {
	r.cleanups = append(r.cleanups, fn)
}

func runRecordingTB(fn func(testing.TB)) *recordingTB {
	tb := &recordingTB{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(tb)
	}()
	<-done
	return tb
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func newUnconnectedPool(t *testing.T) (*pgxpool.Pool, error) {
	t.Helper()
	poolConfig, err := pgxpool.ParseConfig("postgres://localhost:5432/xflickr_test?sslmode=disable")
	if err != nil {
		return nil, err
	}
	return pgxpool.NewWithConfig(context.Background(), poolConfig)
}
