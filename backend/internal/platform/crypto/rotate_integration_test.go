package crypto_test

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

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/crypto"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestRotateRewrapsJSONBColumnInResumableBatches(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the configured XFlickr worktree database")
	}
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source file")
	}
	worktreeRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../"))
	dbConfig, err := config.LoadDatabaseWithDotEnv(filepath.Join(worktreeRoot, ".env"))
	if err != nil {
		t.Fatalf("load test PostgreSQL settings: %v", err)
	}
	pool := dbtest.Open(t, dbConfig)

	table := fmt.Sprintf("crypto_rotation_%d", time.Now().UnixNano())
	quotedTable := `"` + table + `"`
	if _, err := pool.Exec(t.Context(), "CREATE TABLE "+quotedTable+" (id uuid PRIMARY KEY, secret jsonb NOT NULL)"); err != nil {
		t.Fatalf("create rotation table: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP TABLE IF EXISTS "+quotedTable)
	})

	oldKey := bytes.Repeat([]byte{3}, 32)
	activeKey := bytes.Repeat([]byte{9}, 32)
	oldRing, err := crypto.NewKeyring(map[string][]byte{"old": oldKey}, "old")
	if err != nil {
		t.Fatal(err)
	}
	activeRing, err := crypto.NewKeyring(map[string][]byte{"old": oldKey, "active": activeKey}, "active")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{
		"00000000-0000-0000-0000-000000000001",
		"00000000-0000-0000-0000-000000000002",
		"00000000-0000-0000-0000-000000000003",
	} {
		aadID := id
		if id == "00000000-0000-0000-0000-000000000003" {
			aadID = "wrong-row"
		}
		sealed, err := oldRing.Seal([]byte("secret-"+id), crypto.RowAAD(table, aadID))
		if err != nil {
			t.Fatal(err)
		}
		value, err := sealed.Value()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(t.Context(), "INSERT INTO "+quotedTable+" (id, secret) VALUES ($1, $2)", id, value); err != nil {
			t.Fatalf("insert sealed test value: %v", err)
		}
	}

	target := []crypto.Target{{Table: table, IDColumn: "id", SealedColumn: "secret"}}
	rotationDB := crypto.NewPGXRotationDB(pool)
	rotated, err := crypto.Rotate(t.Context(), rotationDB, activeRing, target, 1)
	if !errors.Is(err, crypto.ErrAuthentication) || rotated != 2 {
		t.Fatalf("first Rotate() = (%d, %v), want two committed rows and an AAD failure", rotated, err)
	}
	remainingID := "00000000-0000-0000-0000-000000000003"
	remaining, err := oldRing.Seal([]byte("secret-"+remainingID), crypto.RowAAD(table, remainingID))
	if err != nil {
		t.Fatal(err)
	}
	remainingValue, err := remaining.Value()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "UPDATE "+quotedTable+" SET secret = $1 WHERE id = $2", remainingValue, remainingID); err != nil {
		t.Fatalf("repair remaining sealed fixture: %v", err)
	}

	rotated, err = crypto.Rotate(t.Context(), rotationDB, activeRing, target, 1)
	if err != nil || rotated != 1 {
		t.Fatalf("resumed Rotate() = (%d, %v), want one remaining row", rotated, err)
	}
	rotated, err = crypto.Rotate(t.Context(), rotationDB, activeRing, target, 1)
	if err != nil || rotated != 0 {
		t.Fatalf("completed Rotate() = (%d, %v), want no remaining rows", rotated, err)
	}

	rows, err := pool.Query(t.Context(), "SELECT id, secret FROM "+quotedTable+" ORDER BY id")
	if err != nil {
		t.Fatalf("read rotated values: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var sealed crypto.Sealed
		if err := rows.Scan(&id, &sealed); err != nil {
			t.Fatalf("scan rotated value: %v", err)
		}
		if sealed.KeyID != "active" {
			t.Errorf("row %q key ID = %q, want active", id, sealed.KeyID)
		}
		opened, err := activeRing.Open(sealed, crypto.RowAAD(table, id))
		if err != nil || string(opened) != "secret-"+id {
			t.Errorf("Open(row %q) = (%q, %v)", id, opened, err)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate rotated values: %v", err)
	}
}
