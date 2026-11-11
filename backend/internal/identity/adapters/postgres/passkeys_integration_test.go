package postgres_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/identity/adapters/postgres"
	"github.com/jooservices/xflickr/internal/identity/domain"
)

func TestPasskeyCredentialRoundTrip(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := openSessionPool(t)
	repository := postgres.New(pool)
	userID := insertPasskeyUser(t, ctx, pool)
	credential := domain.PasskeyCredential{
		CredentialID:   []byte(fmt.Sprintf("credential-%d", time.Now().UnixNano())),
		UserID:         userID,
		CredentialJSON: []byte(`{"publicKey":"AQID","counter":0}`),
	}

	if err := repository.InsertPasskey(ctx, credential); err != nil {
		t.Fatalf("InsertPasskey() error = %v", err)
	}
	got, err := repository.FindPasskey(ctx, credential.CredentialID)
	if err != nil {
		t.Fatalf("FindPasskey() error = %v", err)
	}
	var gotJSON, wantJSON any
	if err := json.Unmarshal(got.CredentialJSON, &gotJSON); err != nil {
		t.Fatalf("decode stored credential JSON: %v", err)
	}
	if err := json.Unmarshal(credential.CredentialJSON, &wantJSON); err != nil {
		t.Fatalf("decode requested credential JSON: %v", err)
	}
	if !bytes.Equal(got.CredentialID, credential.CredentialID) || got.UserID != credential.UserID || !reflect.DeepEqual(gotJSON, wantJSON) {
		t.Fatalf("FindPasskey() = %+v, want %+v", got, credential)
	}
	if err := repository.InsertPasskey(ctx, credential); err == nil {
		t.Fatal("InsertPasskey() accepted a duplicate credential ID")
	} else {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
			t.Fatalf("duplicate InsertPasskey() error = %v, want unique violation", err)
		}
	}

	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete passkey user: %v", err)
	}
	if _, err := repository.FindPasskey(ctx, credential.CredentialID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("FindPasskey() after user deletion error = %v, want pgx.ErrNoRows", err)
	}
}

func TestFindPasskeyUsesIndex(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	pool := openSessionPool(t)
	userID := insertPasskeyUser(t, ctx, pool)
	runID := fmt.Sprintf("passkey-plan-%d", time.Now().UnixNano())
	const fixtureRows = int64(400_000)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM webauthn_credentials WHERE credential_json->>'fixture_run' = $1`, runID); err != nil {
			t.Errorf("delete passkey plan fixture: %v", err)
		}
	})

	command, err := pool.Exec(ctx, `
INSERT INTO webauthn_credentials (credential_id, user_id, credential_json)
SELECT convert_to($1, 'UTF8') || decode(lpad(to_hex(value), 16, '0'), 'hex'), $2,
       jsonb_build_object('fixture_run', $1)
FROM generate_series(1, $3::bigint) AS fixture(value)`, runID, userID, fixtureRows)
	if err != nil {
		t.Fatalf("insert passkey plan fixture: %v", err)
	}
	if command.RowsAffected() != fixtureRows {
		t.Fatalf("inserted %d passkey plan rows, want %d", command.RowsAffected(), fixtureRows)
	}
	if _, err := pool.Exec(ctx, `ANALYZE webauthn_credentials`); err != nil {
		t.Fatalf("analyze passkey credentials: %v", err)
	}

	credentialID := append([]byte(runID), make([]byte, 8)...)
	binary.BigEndian.PutUint64(credentialID[len(runID):], uint64(fixtureRows/2))
	rows, err := pool.Query(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
SELECT credential_id, user_id, credential_json
FROM webauthn_credentials WHERE credential_id = $1`, credentialID)
	if err != nil {
		t.Fatalf("explain passkey lookup: %v", err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan passkey lookup plan: %v", err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read passkey lookup plan: %v", err)
	}
	if strings.Contains(strings.ToLower(plan.String()), "seq scan") {
		t.Fatalf("passkey lookup plan uses a sequential scan:\n%s", plan.String())
	}
	if !strings.Contains(plan.String(), "webauthn_credentials_pkey") {
		t.Fatalf("passkey lookup plan does not use the credential primary key:\n%s", plan.String())
	}
	t.Logf("passkey lookup plan:\n%s", plan.String())
}

func insertPasskeyUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int64 {
	t.Helper()
	email := fmt.Sprintf("jooxf95-passkey-%d@example.invalid", time.Now().UnixNano())
	var userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email, password_hash, role) VALUES ($1, 'test-hash', 'user') RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Errorf("delete passkey test user: %v", err)
		}
	})
	return userID
}
