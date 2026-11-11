// Package domain contains identity business types and invariants.
package domain

// PasskeyCredential stores a WebAuthn credential for its owning user.
type PasskeyCredential struct {
	CredentialID   []byte
	UserID         int64
	CredentialJSON []byte
}
