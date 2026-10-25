package crypto

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestSealOpenRoundTripUsesFreshDataKeys(t *testing.T) {
	keyring := testKeyring(t, "active", 1)
	aad := RowAAD("settings", "42")
	first, err := keyring.Seal([]byte("flickr-secret"), aad)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	second, err := keyring.Seal([]byte("flickr-secret"), aad)
	if err != nil {
		t.Fatalf("second Seal() error = %v", err)
	}
	if first.KeyID != "active" || len(first.Nonce) != 12 || len(first.WrappedDEK) != 60 {
		t.Fatalf("sealed envelope has unexpected metadata: key ID %q, nonce %d bytes, wrapped key %d bytes", first.KeyID, len(first.Nonce), len(first.WrappedDEK))
	}
	if bytes.Equal(first.Ciphertext, second.Ciphertext) || bytes.Equal(first.WrappedDEK, second.WrappedDEK) {
		t.Fatal("independent Seal() calls reused encrypted material")
	}
	opened, err := keyring.Open(first, aad)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if string(opened) != "flickr-secret" {
		t.Fatalf("Open() = %q, want original plaintext", opened)
	}
}

func TestOpenRejectsWrongAssociatedData(t *testing.T) {
	keyring := testKeyring(t, "active", 1)
	sealed, err := keyring.Seal([]byte("secret"), RowAAD("settings", "row-1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := keyring.Open(sealed, RowAAD("settings", "row-2")); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("Open() error = %v, want authentication failure", err)
	}
}

func TestOpenRejectsSameIDMasterKeyReplacementWithoutChangingEnvelope(t *testing.T) {
	original := testKeyring(t, "develop-v1", 1)
	configuredReplacement := testKeyring(t, "develop-v1", 2)
	aad := RowAAD("api_keys", "42")
	sealed, err := original.Seal([]byte("credential-secret"), aad)
	if err != nil {
		t.Fatal(err)
	}
	before := sealed.clone()
	_, openErr := configuredReplacement.Open(sealed, aad)
	if !errors.Is(openErr, ErrAuthentication) {
		t.Fatalf("Open() error = %v, want authentication failure", openErr)
	}
	var classified interface{ CredentialDecryptionFailure() }
	if !errors.As(openErr, &classified) {
		t.Fatal("authentication error does not identify a credential decryption failure")
	}
	if !bytes.Equal(sealed.Nonce, before.Nonce) || !bytes.Equal(sealed.Ciphertext, before.Ciphertext) || !bytes.Equal(sealed.WrappedDEK, before.WrappedDEK) || sealed.KeyID != before.KeyID {
		t.Fatal("failed Open() modified the persisted envelope")
	}
	if strings.Contains(openErr.Error(), "credential-secret") {
		t.Fatal("authentication error exposed secret material")
	}
}

func TestOpenRejectsTamperedCiphertextAndWrappedKey(t *testing.T) {
	keyring := testKeyring(t, "active", 1)
	aad := []byte("row binding")
	sealed, err := keyring.Seal([]byte("secret"), aad)
	if err != nil {
		t.Fatal(err)
	}
	tamperedCiphertext := sealed.clone()
	tamperedCiphertext.Ciphertext[0] ^= 0xff
	if _, err := keyring.Open(tamperedCiphertext, aad); !errors.Is(err, ErrAuthentication) {
		t.Errorf("tampered ciphertext error = %v, want authentication failure", err)
	}
	tamperedWrappedDEK := sealed.clone()
	tamperedWrappedDEK.WrappedDEK[len(tamperedWrappedDEK.WrappedDEK)-1] ^= 0xff
	if _, err := keyring.Open(tamperedWrappedDEK, aad); !errors.Is(err, ErrAuthentication) {
		t.Errorf("tampered wrapped key error = %v, want authentication failure", err)
	}
}

func TestOpenRejectsUnknownKeyID(t *testing.T) {
	keyring := testKeyring(t, "active", 1)
	sealed, err := keyring.Seal([]byte("secret"), nil)
	if err != nil {
		t.Fatal(err)
	}
	sealed.KeyID = "removed-key"
	if _, err := keyring.Open(sealed, nil); !errors.Is(err, ErrUnknownKeyID) {
		t.Fatalf("Open() error = %v, want unknown key ID", err)
	}
}

func TestRewrapChangesMasterKeyWithoutChangingCiphertext(t *testing.T) {
	oldRing := testKeyring(t, "old", 3)
	plaintext := []byte("oauth-token")
	aad := RowAAD("credentials", "abc")
	sealed, err := oldRing.Seal(plaintext, aad)
	if err != nil {
		t.Fatal(err)
	}
	newRing, err := NewKeyring(map[string][]byte{
		"old":    bytes.Repeat([]byte{3}, 32),
		"active": bytes.Repeat([]byte{9}, 32),
	}, "active")
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := newRing.Rewrap(sealed, aad)
	if err != nil {
		t.Fatalf("Rewrap() error = %v", err)
	}
	if rotated.KeyID != "active" || !bytes.Equal(rotated.Nonce, sealed.Nonce) || !bytes.Equal(rotated.Ciphertext, sealed.Ciphertext) {
		t.Fatal("Rewrap() did not preserve the data ciphertext and nonce while selecting the active key")
	}
	if bytes.Equal(rotated.WrappedDEK, sealed.WrappedDEK) {
		t.Fatal("Rewrap() retained the old wrapped data key")
	}
	opened, err := newRing.Open(rotated, aad)
	if err != nil || !bytes.Equal(opened, plaintext) {
		t.Fatalf("Open(rotated) = (%q, %v), want original plaintext", opened, err)
	}
	if !newRing.IsActive(rotated.KeyID) || newRing.IsActive(sealed.KeyID) {
		t.Fatal("IsActive() returned an unexpected result")
	}
}

func TestRowAADIsUnambiguous(t *testing.T) {
	if bytes.Equal(RowAAD("ab", "c"), RowAAD("a", "bc")) {
		t.Fatal("RowAAD() produced the same value for distinct table/row pairs")
	}
}

func TestKeyringValidation(t *testing.T) {
	validKey := bytes.Repeat([]byte{1}, 32)
	for _, test := range []struct {
		name     string
		keys     map[string][]byte
		activeID string
	}{
		{name: "empty", keys: map[string][]byte{}, activeID: "active"},
		{name: "invalid active ID", keys: map[string][]byte{"active": validKey}, activeID: "bad id"},
		{name: "invalid key ID", keys: map[string][]byte{"bad id": validKey}, activeID: "bad id"},
		{name: "incorrect key length", keys: map[string][]byte{"active": []byte("short")}, activeID: "active"},
		{name: "missing active key", keys: map[string][]byte{"old": validKey}, activeID: "active"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewKeyring(test.keys, test.activeID); err == nil {
				t.Fatal("NewKeyring() unexpectedly succeeded")
			}
		})
	}

	input := map[string][]byte{"active": validKey}
	keyring, err := NewKeyring(input, "active")
	if err != nil {
		t.Fatal(err)
	}
	input["active"][0] = 2
	if keyring.keys["active"][0] != 1 {
		t.Fatal("NewKeyring() retained caller-owned key material")
	}
	if keyring.ActiveKeyID() != "active" || (*Keyring)(nil).ActiveKeyID() != "" || (*Keyring)(nil).IsActive("active") {
		t.Fatal("nil or active keyring accessors returned unexpected values")
	}
}

func TestParseMasterKeys(t *testing.T) {
	encoded1 := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	encoded2 := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	keys, err := ParseMasterKeys(" old:" + encoded1 + ", active:" + encoded2 + " ")
	if err != nil {
		t.Fatalf("ParseMasterKeys() error = %v", err)
	}
	if !bytes.Equal(keys["old"], bytes.Repeat([]byte{1}, 32)) || !bytes.Equal(keys["active"], bytes.Repeat([]byte{2}, 32)) {
		t.Fatal("ParseMasterKeys() returned incorrect key material")
	}
	for _, input := range []string{"", ",", "no-separator", "bad id:" + encoded1, "key:not-base64", "key:" + base64.StdEncoding.EncodeToString([]byte("short")), "key:" + encoded1 + ",key:" + encoded1} {
		if _, err := ParseMasterKeys(input); err == nil {
			t.Errorf("ParseMasterKeys(%q) unexpectedly succeeded", input)
		}
	}
	if _, err := ParseMasterKeys("secret:" + strings.Repeat("x", 50)); err == nil || strings.Contains(err.Error(), strings.Repeat("x", 50)) {
		t.Errorf("invalid-key error exposed key material: %v", err)
	}
}

func TestNewKeyringFromEnv(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32))
	t.Setenv(MasterKeysEnv, "active:"+encoded)
	t.Setenv(ActiveKeyEnv, "active")
	keyring, err := NewKeyringFromEnv()
	if err != nil || keyring.ActiveKeyID() != "active" {
		t.Fatalf("NewKeyringFromEnv() = (%v, %v), want active keyring", keyring, err)
	}
	t.Setenv(MasterKeysEnv, "")
	if _, err := NewKeyringFromEnv(); err == nil || !strings.Contains(err.Error(), MasterKeysEnv) {
		t.Errorf("missing master keys error = %v", err)
	}
	t.Setenv(MasterKeysEnv, "active:"+encoded)
	t.Setenv(ActiveKeyEnv, "")
	if _, err := NewKeyringFromEnv(); err == nil || !strings.Contains(err.Error(), ActiveKeyEnv) {
		t.Errorf("missing active key error = %v", err)
	}
}

func TestNilKeyringOperations(t *testing.T) {
	var keyring *Keyring
	if _, err := keyring.Seal(nil, nil); err == nil {
		t.Error("nil Keyring.Seal() unexpectedly succeeded")
	}
	if _, err := keyring.Open(Sealed{}, nil); err == nil {
		t.Error("nil Keyring.Open() unexpectedly succeeded")
	}
	if _, err := keyring.Rewrap(Sealed{}, nil); err == nil {
		t.Error("nil Keyring.Rewrap() unexpectedly succeeded")
	}
}

func testKeyring(t *testing.T, activeID string, keyByte byte) *Keyring {
	t.Helper()
	keyring, err := NewKeyring(map[string][]byte{activeID: bytes.Repeat([]byte{keyByte}, 32)}, activeID)
	if err != nil {
		t.Fatal(err)
	}
	return keyring
}
