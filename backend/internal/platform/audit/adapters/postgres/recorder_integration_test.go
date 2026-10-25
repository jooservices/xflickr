package postgres_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	auditpostgres "github.com/jooservices/xflickr/internal/platform/audit/adapters/postgres"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db"
)

func TestRecorderIntegration(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.NewPool(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	target := time.Now().UTC().Format("20060102150405.000000000")
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM audit_events WHERE target=$1`, target) })
	if err := auditpostgres.New(pool).Record(ctx, "user:42", "user.plan.assign", target, map[string]any{"plan_id": 2}); err != nil {
		t.Fatal(err)
	}
	var actor, action, detail string
	if err := pool.QueryRow(ctx, `SELECT actor,action,detail::text FROM audit_events WHERE target=$1`, target).Scan(&actor, &action, &detail); err != nil {
		t.Fatal(err)
	}
	if actor != "user:42" || action != "user.plan.assign" || detail != "{\"plan_id\": 2}" {
		t.Fatalf("stored event = %q %q %q", actor, action, detail)
	}
}
