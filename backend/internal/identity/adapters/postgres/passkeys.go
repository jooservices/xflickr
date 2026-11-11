// Package postgres stores identity data in PostgreSQL.
package postgres

import (
	"context"
	"fmt"

	"github.com/jooservices/xflickr/internal/identity/domain"
)

// InsertPasskey stores a credential record for its owner.
func (repo *Repository) InsertPasskey(ctx context.Context, credential domain.PasskeyCredential) error {
	_, err := repo.pool.Exec(ctx, `
INSERT INTO webauthn_credentials (credential_id, user_id, credential_json)
VALUES ($1, $2, $3)`, credential.CredentialID, credential.UserID, credential.CredentialJSON)
	if err != nil {
		return fmt.Errorf("identity: insert passkey: %w", err)
	}
	return nil
}

// FindPasskey returns the credential record with the given credential ID.
func (repo *Repository) FindPasskey(ctx context.Context, credentialID []byte) (domain.PasskeyCredential, error) {
	var credential domain.PasskeyCredential
	err := repo.pool.QueryRow(ctx, `
SELECT credential_id, user_id, credential_json
FROM webauthn_credentials WHERE credential_id = $1`, credentialID).Scan(
		&credential.CredentialID, &credential.UserID, &credential.CredentialJSON)
	if err != nil {
		return domain.PasskeyCredential{}, fmt.Errorf("identity: find passkey: %w", err)
	}
	return credential, nil
}
