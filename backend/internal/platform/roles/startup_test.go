package roles

import (
	"context"
	"encoding/base64"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/config"
)

func TestWorkerRolesStopAgainstUnreachableLoopbackServices(t *testing.T) {
	setRoleStartupEnvironment(t)
	withUnreachableRolePool(t)
	roles := []struct {
		name string
		run  func(context.Context) error
	}{
		{"scheduler", (schedulerRole{}).Run},
		{"crawl", (crawlRole{}).Run},
		{"sync", (syncRole{}).Run},
		{"media", (mediaRole{}).Run},
	}
	for _, role := range roles {
		t.Run(role.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := role.run(ctx); err == nil {
				t.Fatal("role started without a reachable loopback database")
			}
		})
	}
}

func TestConfiguredAPIRoleStopsAfterStartingOnLocalListener(t *testing.T) {
	setRoleStartupEnvironment(t)
	withUnreachableRolePool(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listenerReady := make(chan struct{})
	role := newConfiguredAPIRole("127.0.0.1:0", "coverage-test")
	role.listenerFactory = func(string) (net.Listener, error) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err == nil {
			close(listenerReady)
		}
		return listener, err
	}
	done := make(chan error, 1)
	go func() { done <- role.Run(ctx) }()
	select {
	case <-listenerReady:
	case err := <-done:
		t.Fatalf("API role exited before its listener: %v", err)
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("API role did not reach its local listener")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("API role shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("API role did not stop after cancellation")
	}
}

func setRoleStartupEnvironment(t *testing.T) {
	t.Helper()
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	values := map[string]string{
		"DB_HOST":                   "127.0.0.1",
		"DB_PORT":                   "1",
		"DB_NAME":                   "xflickr_loopback_test",
		"DB_USER":                   "unused",
		"DB_PASSWORD":               "unused",
		"REDIS_HOST":                "127.0.0.1",
		"REDIS_PORT":                "1",
		"REDIS_PREFIX":              "role-unit:",
		"OPENSEARCH_INDEX_PREFIX":   "role_unit_",
		"NODE_ID":                   "role-unit",
		"XFLICKR_MASTER_KEYS":       "test:" + key,
		"XFLICKR_MASTER_KEY_ACTIVE": "test",
		"BLOB_DRIVER":               "local",
		"BLOB_LOCAL_DIR":            t.TempDir(),
		"ARGON2ID_MEMORY":           "32768",
		"ARGON2ID_ITERATIONS":       "1",
		"ARGON2ID_PARALLELISM":      "1",
		"ARGON2ID_SALT_LENGTH":      "16",
		"ARGON2ID_KEY_LENGTH":       "32",
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
	if _, err := config.Load(); err != nil {
		t.Fatalf("invalid role test configuration: %v", err)
	}
}

func withUnreachableRolePool(t *testing.T) {
	t.Helper()
	previous := openRoleDatabase
	openRoleDatabase = func(ctx context.Context, _ config.DBConfig) (*pgxpool.Pool, error) {
		return pgxpool.New(ctx, "postgres://unused:unused@127.0.0.1:1/xflickr_loopback_test?sslmode=disable&connect_timeout=1")
	}
	t.Cleanup(func() { openRoleDatabase = previous })
}
