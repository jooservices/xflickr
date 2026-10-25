package roles

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/joho/godotenv"

	"github.com/jooservices/xflickr/internal/platform/config"
)

func TestConfiguredAPIRoleIntegration(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../.."))
	env, err := godotenv.Read(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	dbCfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"DB_HOST": dbCfg.Host, "DB_PORT": fmt.Sprint(dbCfg.Port), "DB_NAME": dbCfg.Name, "DB_USER": dbCfg.User, "DB_PASSWORD": dbCfg.Password,
		"REDIS_HOST": "127.0.0.1", "REDIS_PORT": "16382", "REDIS_DB": "8", "REDIS_PASSWORD": env["REDIS_PASSWORD"], "REDIS_PREFIX": env["REDIS_PREFIX"],
		"OPENSEARCH_INDEX_PREFIX": "xflickr_wtk112_", "NODE_ID": "jOOXF-112-integration",
		"XFLICKR_MASTER_KEYS": "integration:" + base64.StdEncoding.EncodeToString(make([]byte, 32)), "XFLICKR_MASTER_KEY_ACTIVE": "integration",
	} {
		t.Setenv(name, value)
	}
	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reservation.Addr().String()
	if err := reservation.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- newConfiguredAPIRole(address, "integration").Run(ctx) }()
	client := &http.Client{Timeout: time.Second}
	var response *http.Response
	for attempt := 0; attempt < 30; attempt++ {
		response, err = client.Get("http://" + address + "/healthz")
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	cancel()
	if err != nil {
		t.Fatalf("API role did not start: %v", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status=%d", response.StatusCode)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("API role returned: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("API role did not stop")
	}
}
