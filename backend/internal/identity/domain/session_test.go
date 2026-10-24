package domain

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

func TestSessionLifetimes(t *testing.T) {
	if SessionLifetime != 14*24*time.Hour {
		t.Fatalf("SessionLifetime = %s", SessionLifetime)
	}
	if SessionSlideInterval != time.Minute {
		t.Fatalf("SessionSlideInterval = %s", SessionSlideInterval)
	}
	if EncodedOpaqueTokenLength != base64.RawURLEncoding.EncodedLen(OpaqueTokenBytes) {
		t.Fatalf("EncodedOpaqueTokenLength = %d", EncodedOpaqueTokenLength)
	}
}

func TestOpaqueTokenAndSessionHash(t *testing.T) {
	first, err := NewOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !ValidOpaqueToken(first) || !ValidOpaqueToken(second) {
		t.Fatalf("tokens = %q, %q", first, second)
	}
	if ValidOpaqueToken("") || ValidOpaqueToken(first[:len(first)-1]) || ValidOpaqueToken(first+"*") {
		t.Fatal("accepted a malformed opaque token")
	}
	hash := HashSessionToken(first)
	if len(hash) != 32 || bytes.Equal(hash, []byte(first)) {
		t.Fatalf("hash = %x", hash)
	}
	if !bytes.Equal(hash, HashSessionToken(first)) || bytes.Equal(hash, HashSessionToken(second)) {
		t.Fatal("session hash is not a stable digest of the opaque token")
	}
}

func TestNewOpaqueTokenReportsRandomFailure(t *testing.T) {
	original := readRandom
	t.Cleanup(func() { readRandom = original })
	readRandom = func([]byte) (int, error) { return 0, errors.New("boom") }
	if _, err := NewOpaqueToken(); err == nil {
		t.Fatal("expected random reader failure")
	}
}
