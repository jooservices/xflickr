package postgres

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jooservices/xflickr/internal/identity/domain"
)

func TestInsertPasskey(t *testing.T) {
	credential := domain.PasskeyCredential{
		CredentialID:   []byte("credential-id"),
		UserID:         42,
		CredentialJSON: []byte(`{"publicKey":"AQID"}`),
	}
	pool := &passkeyPoolFake{}
	if err := testRepo(pool).InsertPasskey(t.Context(), credential); err != nil {
		t.Fatalf("InsertPasskey() error = %v", err)
	}
	if got, want := strings.Join(strings.Fields(pool.query), " "), "INSERT INTO webauthn_credentials (credential_id, user_id, credential_json) VALUES ($1, $2, $3)"; got != want {
		t.Fatalf("InsertPasskey() query = %q, want %q", got, want)
	}
	if len(pool.args) != 3 || !bytes.Equal(pool.args[0].([]byte), credential.CredentialID) || pool.args[1] != credential.UserID || !bytes.Equal(pool.args[2].([]byte), credential.CredentialJSON) {
		t.Fatalf("InsertPasskey() args = %#v", pool.args)
	}

	databaseErr := errors.New("database failed")
	if err := testRepo(&passkeyPoolFake{execErr: databaseErr}).InsertPasskey(t.Context(), credential); !errors.Is(err, databaseErr) {
		t.Fatalf("InsertPasskey() error = %v, want wrapped database error", err)
	}
}

func TestFindPasskey(t *testing.T) {
	credentialID := []byte("credential-id")
	credentialJSON := []byte(`{"publicKey":"AQID"}`)
	databaseErr := errors.New("database failed")
	for _, tc := range []struct {
		name string
		row  pgx.Row
		want domain.PasskeyCredential
		err  error
	}{
		{
			name: "found",
			row:  identityRow{values: []any{credentialID, int64(42), credentialJSON}},
			want: domain.PasskeyCredential{CredentialID: credentialID, UserID: 42, CredentialJSON: credentialJSON},
		},
		{name: "missing", row: identityRow{err: pgx.ErrNoRows}, err: pgx.ErrNoRows},
		{name: "database failure", row: identityRow{err: databaseErr}, err: databaseErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := &passkeyPoolFake{row: tc.row}
			got, err := testRepo(pool).FindPasskey(t.Context(), credentialID)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("FindPasskey() error = %v, want %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("FindPasskey() error = %v", err)
			}
			if !bytes.Equal(got.CredentialID, tc.want.CredentialID) || got.UserID != tc.want.UserID || !bytes.Equal(got.CredentialJSON, tc.want.CredentialJSON) {
				t.Fatalf("FindPasskey() = %+v, want %+v", got, tc.want)
			}
			if got, want := strings.Join(strings.Fields(pool.query), " "), "SELECT credential_id, user_id, credential_json FROM webauthn_credentials WHERE credential_id = $1"; got != want {
				t.Fatalf("FindPasskey() query = %q, want %q", got, want)
			}
			if len(pool.args) != 1 || !bytes.Equal(pool.args[0].([]byte), credentialID) {
				t.Fatalf("FindPasskey() args = %#v", pool.args)
			}
		})
	}
}

type passkeyPoolFake struct {
	query   string
	args    []any
	row     pgx.Row
	execErr error
}

func (pool *passkeyPoolFake) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	pool.query, pool.args = query, append([]any(nil), args...)
	return pgconn.NewCommandTag("INSERT 0 1"), pool.execErr
}

func (*passkeyPoolFake) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, nil
}

func (pool *passkeyPoolFake) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	pool.query, pool.args = query, append([]any(nil), args...)
	return pool.row
}

func (*passkeyPoolFake) Begin(context.Context) (pgx.Tx, error) { return nil, nil }

func (*passkeyPoolFake) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) { return nil, nil }
