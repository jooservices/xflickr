package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/crypto"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestCryptoRotateCommandAgainstWorktreeDatabase(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the configured XFlickr worktree database")
	}
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source file")
	}
	worktreeRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../"))
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(worktreeRoot); err != nil {
		t.Fatalf("change to worktree root: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDir) })

	dbConfig, err := config.LoadDatabaseWithDotEnv(filepath.Join(worktreeRoot, ".env"))
	if err != nil {
		t.Fatalf("load test PostgreSQL settings: %v", err)
	}
	pool := dbtest.Open(t, dbConfig)

	table := fmt.Sprintf("crypto_command_%d", time.Now().UnixNano())
	quotedTable := `"` + table + `"`
	if _, err := pool.Exec(t.Context(), "CREATE TABLE "+quotedTable+" (id uuid PRIMARY KEY, secret jsonb NOT NULL)"); err != nil {
		t.Fatalf("create command rotation table: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP TABLE IF EXISTS "+quotedTable)
	})

	oldKey := bytes.Repeat([]byte{5}, 32)
	activeKey := bytes.Repeat([]byte{8}, 32)
	oldRing, err := crypto.NewKeyring(map[string][]byte{"old": oldKey}, "old")
	if err != nil {
		t.Fatal(err)
	}
	rowID := "00000000-0000-0000-0000-000000000001"
	sealed, err := oldRing.Seal([]byte("command-secret"), crypto.RowAAD(table, rowID))
	if err != nil {
		t.Fatal(err)
	}
	value, err := sealed.Value()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "INSERT INTO "+quotedTable+" (id, secret) VALUES ($1, $2)", rowID, value); err != nil {
		t.Fatalf("insert command rotation fixture: %v", err)
	}
	encodedKeys := "old:" + base64.StdEncoding.EncodeToString(oldKey) + ",active:" + base64.StdEncoding.EncodeToString(activeKey)
	t.Setenv(crypto.MasterKeysEnv, encodedKeys)
	t.Setenv(crypto.ActiveKeyEnv, "active")

	var stdout, stderr bytes.Buffer
	code := runCryptoRotate(context.Background(), []string{"--target", table + ",id,secret", "--batch-size=1"}, &stdout, &stderr, runConfiguredRotation)
	if code != 0 || stderr.Len() != 0 || stdout.String() != "rotated 1 sealed value(s)\n" {
		t.Fatalf("crypto rotate command = (%d, stdout=%q, stderr=%q)", code, stdout.String(), stderr.String())
	}
	var rotated crypto.Sealed
	if err := pool.QueryRow(t.Context(), "SELECT secret FROM "+quotedTable+" WHERE id = $1", rowID).Scan(&rotated); err != nil {
		t.Fatalf("read command-rotated value: %v", err)
	}
	activeRing, err := crypto.NewKeyring(map[string][]byte{"old": oldKey, "active": activeKey}, "active")
	if err != nil {
		t.Fatal(err)
	}
	opened, err := activeRing.Open(rotated, crypto.RowAAD(table, rowID))
	if err != nil || string(opened) != "command-secret" {
		t.Fatalf("Open(command-rotated) = (%q, %v), want original plaintext", opened, err)
	}
}
