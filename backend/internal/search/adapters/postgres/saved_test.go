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
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/search/adapters/postgres"
	"github.com/jooservices/xflickr/internal/search/app"
)

var errSavedSearchBackend = errors.New("saved search backend failed")

type savedSearchTestRow struct {
	values []any
	err    error
}

func (row savedSearchTestRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != len(row.values) {
		return fmt.Errorf("scan received %d destinations for %d values", len(destinations), len(row.values))
	}
	for index, destination := range destinations {
		target := reflect.ValueOf(destination)
		value := reflect.ValueOf(row.values[index])
		if target.Kind() != reflect.Pointer || target.IsNil() || !value.Type().AssignableTo(target.Elem().Type()) {
			return fmt.Errorf("scan value %d has incompatible type", index)
		}
		target.Elem().Set(value)
	}
	return nil
}

func TestSavedSearchPostgresRepositoryRejectsNilPool(t *testing.T) {
	if _, err := postgres.NewSavedRepository(nil); err == nil {
		t.Fatal("NewSavedRepository(nil) succeeded")
	}
	if _, err := postgres.NewSavedRepository(new(pgxpool.Pool)); err != nil {
		t.Fatalf("NewSavedRepository(pool) error = %v", err)
	}
}

func TestSavedSearchPostgresRepositoryQueries(t *testing.T) {
	wantTime := time.Date(2026, time.November, 11, 8, 30, 0, 123000, time.UTC)
	want := app.SavedSearch{ID: 41, UserID: 7, Query: "cats 雪", CreatedAt: wantTime}
	tests := []struct {
		name       string
		query      string
		values     []any
		queryError error
		want       app.SavedSearch
		wantErr    error
	}{
		{name: "create returns database values", query: "insert", values: []any{want.ID, want.UserID, want.Query, want.CreatedAt}, want: want},
		{name: "create wraps database errors", query: "insert", queryError: errSavedSearchBackend, wantErr: errSavedSearchBackend},
		{name: "get returns the owner scoped row", query: "select", values: []any{want.ID, want.UserID, want.Query, want.CreatedAt}, want: want},
		{name: "get maps missing rows", query: "select", queryError: pgx.ErrNoRows, wantErr: app.ErrNotFound},
		{name: "get wraps database errors", query: "select", queryError: errSavedSearchBackend, wantErr: errSavedSearchBackend},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			row := savedSearchTestRow{values: test.values, err: test.queryError}
			var statement string
			var arguments []any
			repository, err := postgres.NewSavedRepositoryWithQueryRow(func(_ context.Context, sql string, args ...any) pgx.Row {
				statement = sql
				arguments = append([]any(nil), args...)
				return row
			})
			if err != nil {
				t.Fatal(err)
			}

			var got app.SavedSearch
			if test.query == "insert" {
				got, err = repository.Create(t.Context(), 7, "cats 雪")
				if !strings.Contains(statement, "INSERT INTO saved_searches") || !reflect.DeepEqual(arguments, []any{int64(7), "cats 雪"}) {
					t.Fatalf("Create() query = %q with arguments %v", statement, arguments)
				}
			} else {
				got, err = repository.Get(t.Context(), 7, 41)
				if !strings.Contains(statement, "WHERE user_id = $1 AND id = $2") || !reflect.DeepEqual(arguments, []any{int64(7), int64(41)}) {
					t.Fatalf("Get() query = %q with arguments %v", statement, arguments)
				}
			}
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("repository error = %v, want %v", err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("repository result = %+v, want %+v", got, test.want)
			}
		})
	}
	if _, err := postgres.NewSavedRepositoryWithQueryRow(nil); err == nil {
		t.Fatal("NewSavedRepositoryWithQueryRow(nil) succeeded")
	}
}

func TestSavedSearchPostgresLifecycle(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	pool := savedSearchIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	repository, err := postgres.NewSavedRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	firstUserID := insertSavedSearchUser(t, pool)
	secondUserID := insertSavedSearchUser(t, pool)
	service := app.SavedSearches{Repository: repository}

	created, err := service.Create(ctx, firstUserID, "  cats in 雪  ")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.ID <= 0 || created.UserID != firstUserID || created.Query != "cats in 雪" || created.CreatedAt.IsZero() {
		t.Fatalf("created saved search = %+v", created)
	}
	got, err := service.Get(ctx, firstUserID, created.ID)
	if err != nil || got.ID != created.ID || got.UserID != created.UserID || got.Query != created.Query || !got.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("Get() = %+v, %v; want %+v", got, err, created)
	}
	if _, err := repository.Create(ctx, firstUserID, ""); err == nil {
		t.Fatal("repository accepted a query rejected by the database constraint")
	}
	canceledCtx, cancelRead := context.WithCancel(ctx)
	cancelRead()
	if _, err := repository.Get(canceledCtx, firstUserID, created.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("Get() with a canceled context error = %v, want context.Canceled", err)
	}
	if _, err := service.Get(ctx, secondUserID, created.ID); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("Get() for another user error = %v, want ErrNotFound", err)
	}
	if _, err := service.Get(ctx, firstUserID, created.ID+1000000); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("Get() for a missing id error = %v, want ErrNotFound", err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id = $1", firstUserID); err != nil {
		t.Fatalf("delete fixture user: %v", err)
	}
	var remains bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM saved_searches WHERE id = $1)", created.ID).Scan(&remains); err != nil {
		t.Fatalf("check cascade result: %v", err)
	}
	if remains {
		t.Fatal("saved search remained after deleting its owner")
	}
}

func TestSavedSearchPostgresLookupPlan(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	pool := savedSearchIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	userID := insertSavedSearchUser(t, pool)
	repository, err := postgres.NewSavedRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	target, err := repository.Create(ctx, userID, "target query")
	if err != nil {
		t.Fatalf("create target saved search: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO saved_searches (user_id, query)
SELECT $1, 'fixture query ' || value
FROM generate_series(1, 400000) AS value`, userID); err != nil {
		t.Fatalf("seed lookup plan fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, "ANALYZE saved_searches"); err != nil {
		t.Fatalf("analyze lookup plan fixture: %v", err)
	}

	const lookup = `SELECT id, user_id, query, created_at
FROM saved_searches
WHERE user_id = $1 AND id = $2`
	var rawPlan []byte
	if err := pool.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+lookup, userID, target.ID).Scan(&rawPlan); err != nil {
		t.Fatalf("explain saved search lookup: %v", err)
	}
	var plans []struct {
		Plan savedSearchPlanNode `json:"Plan"`
	}
	if err := json.Unmarshal(rawPlan, &plans); err != nil {
		t.Fatalf("decode saved search lookup plan: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("lookup plan entries = %d, want one", len(plans))
	}
	var foundIndex bool
	if hasSavedSearchSeqScan(plans[0].Plan, &foundIndex) {
		t.Fatalf("lookup plan contains a Seq Scan on saved_searches: %s", rawPlan)
	}
	if !foundIndex {
		t.Fatalf("lookup plan does not use saved_searches_user_id_id_idx: %s", rawPlan)
	}
	t.Logf("saved search lookup plan: %s", rawPlan)
}

type savedSearchPlanNode struct {
	NodeType     string                `json:"Node Type"`
	RelationName string                `json:"Relation Name"`
	IndexName    string                `json:"Index Name"`
	Plans        []savedSearchPlanNode `json:"Plans"`
}

func hasSavedSearchSeqScan(node savedSearchPlanNode, foundIndex *bool) bool {
	if node.RelationName == "saved_searches" && node.NodeType == "Seq Scan" {
		return true
	}
	if node.IndexName == "saved_searches_user_id_id_idx" {
		*foundIndex = true
	}
	for _, child := range node.Plans {
		if hasSavedSearchSeqScan(child, foundIndex) {
			return true
		}
	}
	return false
}

func savedSearchIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate saved search integration test source")
	}
	worktreeRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../"))
	dbConfig, err := config.LoadDatabaseWithDotEnv(filepath.Join(worktreeRoot, ".env"))
	if err != nil {
		t.Fatalf("load worktree PostgreSQL settings: %v", err)
	}
	if err := dbtest.ValidateDatabaseName(dbConfig.Name); err != nil {
		t.Fatal(err)
	}
	return dbtest.Open(t, dbConfig)
}

func insertSavedSearchUser(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	email := fmt.Sprintf("saved-search-%d-%d@example.invalid", os.Getpid(), time.Now().UnixNano())
	var userID int64
	if err := pool.QueryRow(t.Context(), `
INSERT INTO users (email, password_hash, role, locale)
VALUES ($1, 'integration-hash', 'user', 'en')
RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatalf("create saved search fixture user: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", userID); err != nil {
			t.Errorf("delete saved search fixture user %d: %v", userID, err)
		}
	})
	return userID
}
