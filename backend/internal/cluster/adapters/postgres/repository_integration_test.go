package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/cluster/adapters/postgres"
	"github.com/jooservices/xflickr/internal/cluster/app"
	"github.com/jooservices/xflickr/internal/cluster/domain"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestNodeTokenAndHeartbeatLifecycle(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the isolated worktree database")
	}
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve integration test source path")
	}
	worktreeRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(worktreeRoot, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	pool := dbtest.Open(t, cfg)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	name := fmt.Sprintf("integration-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM nodes WHERE name=$1`, name); err != nil {
			t.Errorf("delete test node: %v", err)
		}
	})

	repository := postgres.New(pool)
	service := app.Nodes{Repository: repository}
	node, token, err := service.Create(ctx, name, []string{"crawl", "media"})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	if node.Name != name || node.State != domain.StateOffline {
		t.Fatalf("created node = %+v", node)
	}
	var storedHash []byte
	if err := pool.QueryRow(ctx, `SELECT token_hash FROM nodes WHERE name=$1`, name).Scan(&storedHash); err != nil {
		t.Fatalf("read token hash: %v", err)
	}
	if !bytes.Equal(storedHash, domain.HashNodeToken(token)) || bytes.Contains(storedHash, []byte(token)) {
		t.Fatal("database did not store the one-way token hash")
	}
	if _, err := service.AuthenticateToken(ctx, token); err != nil {
		t.Fatalf("authenticate generated token: %v", err)
	}
	if _, err := service.AuthenticateToken(ctx, testOtherToken()); !errors.Is(err, app.ErrInvalidToken) {
		t.Fatalf("unknown token error = %v", err)
	}

	at := time.Now().UTC().Truncate(time.Microsecond)
	updated, err := service.Heartbeat(ctx, name, app.HeartbeatInput{Version: "v1", EgressIP: "192.0.2.44", Metrics: []byte(`{"load":0.25}`)})
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if updated.State != domain.StateOnline || updated.Version != "v1" || updated.EgressIP != "192.0.2.44" || updated.LastHeartbeatAt == nil || updated.LastHeartbeatAt.Before(at.Add(-time.Second)) {
		t.Fatalf("updated node = %+v", updated)
	}
	if string(updated.Metrics) != `{"load": 0.25}` {
		t.Fatalf("metrics = %s", updated.Metrics)
	}
	if _, err := pool.Exec(ctx, `UPDATE nodes SET state='disabled' WHERE name=$1`, name); err != nil {
		t.Fatalf("disable test node: %v", err)
	}
	if _, err := service.AuthenticateToken(ctx, token); !errors.Is(err, app.ErrInvalidToken) {
		t.Fatalf("disabled node token error = %v", err)
	}
}

func testOtherToken() string {
	return "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
}
