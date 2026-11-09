package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestBudgetUsesSmallestHourlyCeiling(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := budgetIntegrationPool(t)
	setBudgetSetting(ctx, t, pool, "usable_hourly_quota", "3400")
	setBudgetSetting(ctx, t, pool, "daily_call_limit", "0")

	highKey := insertBudgetAPIKey(ctx, t, pool, 3600)
	lowKey := insertBudgetAPIKey(ctx, t, pool, 1800)
	loader := Budgets{Pool: pool}
	for _, test := range []struct {
		name string
		id   int64
		want int64
	}{
		{name: "usable quota", id: highKey, want: 3400},
		{name: "key limit", id: lowKey, want: 1800},
	} {
		t.Run(test.name, func(t *testing.T) {
			budget, err := loader.Budget(ctx, test.id)
			if err != nil || budget.HourlyLimit != test.want {
				t.Fatalf("Budget() = %+v, %v; want hourly limit %d", budget, err, test.want)
			}
		})
	}

	setBudgetSetting(ctx, t, pool, "daily_call_limit", "2500")
	budget, err := loader.Budget(ctx, highKey)
	if err != nil || budget.HourlyLimit != 2500 {
		t.Fatalf("positive daily limit budget = %+v, %v; want 2500", budget, err)
	}
}

func TestBudgetReadsChangedUsableQuota(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := budgetIntegrationPool(t)
	keyID := insertBudgetAPIKey(ctx, t, pool, 3600)
	loader := Budgets{Pool: pool}
	initial, err := loader.Budget(ctx, keyID)
	if err != nil || initial.HourlyLimit != 3400 {
		t.Fatalf("initial Budget() = %+v, %v; want 3400", initial, err)
	}
	setBudgetSetting(ctx, t, pool, "usable_hourly_quota", "2100")

	updated, err := loader.Budget(ctx, keyID)
	if err != nil || updated.HourlyLimit != 2100 {
		t.Fatalf("Budget() after committed setting update = %+v, %v; want 2100", updated, err)
	}
}

func TestBudgetFailsClosedOnInvalidUsableQuota(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := budgetIntegrationPool(t)
	keyID := insertBudgetAPIKey(ctx, t, pool, 3600)
	setBudgetSetting(ctx, t, pool, "usable_hourly_quota", `"invalid"`)

	if budget, err := (Budgets{Pool: pool}).Budget(ctx, keyID); err == nil {
		t.Fatalf("invalid setting returned budget %+v", budget)
	}
}

func TestBudgetPlanUsesPrimaryKeys(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool := budgetIntegrationPool(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer rollbackCancel()
		if err := tx.Rollback(rollbackCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("rollback query plan fixture: %v", err)
		}
		vacuumCtx, vacuumCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer vacuumCancel()
		if _, err := pool.Exec(vacuumCtx, "VACUUM (ANALYZE) app_settings"); err != nil {
			t.Errorf("clean query plan fixture statistics: %v", err)
		}
	})

	unique := fmt.Sprintf("__quota_plan_%d_", time.Now().UnixNano())
	if _, err := tx.Exec(ctx, `INSERT INTO app_settings (key, value, user_overridable)
		SELECT $1 || n::text, '0'::jsonb, false FROM generate_series(1, 400000) AS n`, unique); err != nil {
		t.Fatalf("insert 400000 query plan settings: %v", err)
	}
	if _, err := tx.Exec(ctx, "ANALYZE app_settings"); err != nil {
		t.Fatalf("analyze query plan fixture: %v", err)
	}
	var userID, keyID int64
	if err := tx.QueryRow(ctx, `INSERT INTO users (email, password_hash, role, locale)
		VALUES ($1, 'integration-hash', 'user', 'en') RETURNING id`, unique+"@example.invalid").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO api_keys
		(user_id, label, key_public, secret_sealed, per_hour_limit, per_second_limit, burst)
		VALUES ($1, $2, $3, '{}'::jsonb, 3600, 4, 5) RETURNING id`, userID, unique, unique).Scan(&keyID); err != nil {
		t.Fatal(err)
	}

	var rawPlan []byte
	if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+budgetSQL, keyID).Scan(&rawPlan); err != nil {
		t.Fatalf("explain budget query: %v", err)
	}
	indexes := make(map[string]bool)
	var envelope []map[string]json.RawMessage
	if err := json.Unmarshal(rawPlan, &envelope); err != nil || len(envelope) != 1 {
		t.Fatalf("decode query plan: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(envelope[0]["Plan"], &root); err != nil {
		t.Fatalf("decode plan tree: %v", err)
	}
	if err := inspectBudgetPlan(root, indexes); err != nil {
		t.Fatal(err)
	}
	for _, index := range []string{"api_keys_pkey", "app_settings_pkey"} {
		if !indexes[index] {
			t.Errorf("query plan did not use %s: %s", index, rawPlan)
		}
	}
	t.Logf("EXPLAIN (ANALYZE, BUFFERS) on 400000 synthetic settings: %s", rawPlan)

	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback query plan fixture: %v", err)
	}
}

func inspectBudgetPlan(node map[string]any, indexes map[string]bool) error {
	nodeType, _ := node["Node Type"].(string)
	relation, _ := node["Relation Name"].(string)
	if strings.Contains(nodeType, "Seq Scan") && (relation == "api_keys" || relation == "app_settings") {
		return fmt.Errorf("budget plan contains %s on %s", nodeType, relation)
	}
	if indexName, ok := node["Index Name"].(string); ok {
		indexes[indexName] = true
	}
	children, _ := node["Plans"].([]any)
	for _, child := range children {
		childNode, ok := child.(map[string]any)
		if !ok {
			return fmt.Errorf("unexpected query plan node %T", child)
		}
		if err := inspectBudgetPlan(childNode, indexes); err != nil {
			return err
		}
	}
	return nil
}

func budgetIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the isolated worktree database")
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "xflickr_wtjooxf366" {
		t.Fatal("refusing non-ticket worktree database")
	}
	return dbtest.Open(t, cfg)
}

func insertBudgetAPIKey(ctx context.Context, t *testing.T, pool *pgxpool.Pool, hourly int64) int64 {
	t.Helper()
	unique := fmt.Sprintf("budget-%d", time.Now().UnixNano())
	var userID, keyID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, role, locale)
		VALUES ($1, 'integration-hash', 'user', 'en') RETURNING id`, unique+"@example.invalid").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM users WHERE id=$1`, userID); err != nil {
			t.Errorf("cleanup budget test user: %v", err)
		}
	})
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys
		(user_id, label, key_public, secret_sealed, per_hour_limit, per_second_limit, burst)
		VALUES ($1, $2, $3, '{}'::jsonb, $4, 100, 100) RETURNING id`, userID, unique, unique, hourly).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	return keyID
}

func setBudgetSetting(ctx context.Context, t *testing.T, pool *pgxpool.Pool, key, raw string) {
	t.Helper()
	var original []byte
	if err := pool.QueryRow(ctx, `SELECT value FROM app_settings WHERE key=$1`, key).Scan(&original); err != nil {
		t.Fatalf("read %s setting: %v", key, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE app_settings SET value=$1::jsonb WHERE key=$2`, raw, key); err != nil {
		t.Fatalf("update %s setting: %v", key, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanupCtx, `UPDATE app_settings SET value=$1::jsonb WHERE key=$2`, string(original), key); err != nil {
			t.Errorf("restore %s setting: %v", key, err)
		}
	})
}
