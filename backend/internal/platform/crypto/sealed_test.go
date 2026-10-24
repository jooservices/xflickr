package crypto

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestSealedValueAndScanRoundTrip(t *testing.T) {
	keyring := testKeyring(t, "active", 1)
	want, err := keyring.Seal([]byte("secret"), []byte("row"))
	if err != nil {
		t.Fatal(err)
	}
	value, err := want.Value()
	if err != nil {
		t.Fatalf("Value() error = %v", err)
	}
	encoded, ok := value.([]byte)
	if !ok {
		t.Fatalf("Value() type = %T, want []byte", value)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"key_id", "nonce", "ciphertext", "wrapped_dek"} {
		if _, ok := fields[field]; !ok {
			t.Errorf("serialized envelope is missing %q", field)
		}
	}

	var got Sealed
	if err := got.Scan(encoded); err != nil {
		t.Fatalf("Scan([]byte) error = %v", err)
	}
	if !bytes.Equal(got.Nonce, want.Nonce) || !bytes.Equal(got.Ciphertext, want.Ciphertext) || !bytes.Equal(got.WrappedDEK, want.WrappedDEK) || got.KeyID != want.KeyID {
		t.Fatal("Scan() did not restore the sealed envelope")
	}
	opened, err := keyring.Open(got, []byte("row"))
	if err != nil || string(opened) != "secret" {
		t.Fatalf("Open(scanned) = (%q, %v), want original plaintext", opened, err)
	}

	var fromString Sealed
	if err := fromString.Scan(string(encoded)); err != nil {
		t.Fatalf("Scan(string) error = %v", err)
	}
	if fromString.KeyID != want.KeyID {
		t.Errorf("Scan(string) key ID = %q, want %q", fromString.KeyID, want.KeyID)
	}
}

func TestSealedScanNilAndRejectsInvalidInput(t *testing.T) {
	sealed := Sealed{KeyID: "stale"}
	if err := sealed.Scan(nil); err != nil {
		t.Fatalf("Scan(nil) error = %v", err)
	}
	if sealed.KeyID != "" || len(sealed.Nonce) != 0 || len(sealed.Ciphertext) != 0 || len(sealed.WrappedDEK) != 0 {
		t.Fatalf("Scan(nil) = %#v, want zero value", sealed)
	}
	if err := sealed.Scan(int64(12)); err == nil {
		t.Error("Scan(int64) unexpectedly succeeded")
	}
	if err := sealed.Scan([]byte("{")); err == nil {
		t.Error("Scan(malformed JSON) unexpectedly succeeded")
	}
	if err := sealed.Scan([]byte(`{"key_id":"missing-fields"}`)); !errors.Is(err, ErrInvalidSealed) {
		t.Errorf("Scan(incomplete envelope) error = %v, want invalid sealed value", err)
	}
	var nilReceiver *Sealed
	if err := nilReceiver.Scan(nil); err == nil {
		t.Error("Scan() on nil receiver unexpectedly succeeded")
	}
}

func TestSealedValueRejectsInvalidEnvelope(t *testing.T) {
	if _, err := (Sealed{}).Value(); !errors.Is(err, ErrInvalidSealed) {
		t.Fatalf("Value() error = %v, want invalid sealed value", err)
	}
}
