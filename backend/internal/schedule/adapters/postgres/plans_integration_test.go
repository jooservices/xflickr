package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/audit"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/schedule/adapters/postgres"
	"github.com/jooservices/xflickr/internal/schedule/app"
	"github.com/jooservices/xflickr/internal/schedule/domain"
)

func TestPlanStoreIntegration(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	cfg = dbtest.EmptyDatabase(t, cfg)
	pool := dbtest.Open(t, cfg)
	store := postgres.New(pool)
	plans, err := store.ListPlans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) < 2 || plans[0].Name != "free" && plans[1].Name != "free" || plans[0].Name != "pro" && plans[1].Name != "pro" {
		t.Fatalf("seed plans=%+v", plans)
	}
	var free, pro domain.Plan
	for _, p := range plans {
		if p.Name == "free" {
			free = p
		}
		if p.Name == "pro" {
			pro = p
		}
	}
	if free.Weight != 1 || free.ResourceShare != .2 || pro.Weight != 4 || pro.ResourceShare != .8 {
		t.Fatalf("seed tiers free=%+v pro=%+v", free, pro)
	}
	prefix := fmt.Sprintf("jooxf112-%d", time.Now().UnixNano())
	var userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'test','user') RETURNING id`, prefix+"@example.invalid").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID) })
	auditActor := "integration:" + prefix
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM audit_events WHERE actor=$1`, auditActor) })
	got, err := store.PlanForUser(ctx, userID)
	if err != nil || got.Name != "free" {
		t.Fatalf("default user plan=%+v err=%v", got, err)
	}
	updated := append([]domain.Plan(nil), plans...)
	planAudit := audit.Event{Actor: auditActor, Action: "plans.update", Target: "plans", Detail: map[string]any{"plans": plans}}
	t.Cleanup(func() { _ = store.SavePlans(context.Background(), plans, planAudit) })
	updated[0].MaxDepthCap++
	if err := store.SavePlans(ctx, updated, planAudit); err != nil {
		t.Fatal(err)
	}
	assignAudit := audit.Event{Actor: auditActor, Action: "user.plan.assign", Target: fmt.Sprintf("user:%d", userID), Detail: map[string]any{"plan_id": pro.ID}}
	if err := store.AssignUserPlan(ctx, userID, pro.ID, assignAudit); err != nil {
		t.Fatal(err)
	}
	got, err = store.PlanForUser(ctx, userID)
	if err != nil || got.Name != "pro" {
		t.Fatalf("assigned user plan=%+v err=%v", got, err)
	}
	if err := store.AssignUserPlan(ctx, userID, 1<<60, assignAudit); !errors.Is(err, app.ErrPlanNotFound) {
		t.Fatalf("missing plan error=%v", err)
	}
	if err := store.AssignUserPlan(ctx, 1<<60, pro.ID, assignAudit); !errors.Is(err, app.ErrUserNotFound) {
		t.Fatalf("missing user error=%v", err)
	}
	if _, err := store.PlanForUser(ctx, 1<<60); !errors.Is(err, app.ErrUserNotFound) {
		t.Fatalf("missing user lookup error=%v", err)
	}
}

func TestPlanUpdateRollsBackWhenAuditCannotBeWritten(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	cfg = dbtest.EmptyDatabase(t, cfg)
	pool := dbtest.Open(t, cfg)
	store := postgres.New(pool)
	var original int
	if err := pool.QueryRow(ctx, `SELECT max_depth_cap FROM plans WHERE name='free'`).Scan(&original); err != nil {
		t.Fatal(err)
	}
	updated, err := store.ListPlans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range updated {
		if updated[i].Name == "free" {
			updated[i].MaxDepthCap = original + 1
		}
	}
	event := audit.Event{Actor: "user:1", Action: "plans.update", Target: "plans", Detail: map[string]any{"invalid": math.Inf(1)}}
	if err := store.SavePlans(ctx, updated, event); err == nil {
		t.Fatal("SavePlans succeeded with an unserializable audit event")
	}
	var actual int
	if err := pool.QueryRow(ctx, `SELECT max_depth_cap FROM plans WHERE name='free'`).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != original {
		t.Fatalf("plan update survived audit failure: depth=%d, want %d", actual, original)
	}
}
