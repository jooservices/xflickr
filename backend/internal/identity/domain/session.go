package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"
)

const (
	// OpaqueTokenBytes is the entropy of a session id and a CSRF token.
	OpaqueTokenBytes = 32
	// EncodedOpaqueTokenLength is the base64.RawURLEncoding length of OpaqueTokenBytes.
	EncodedOpaqueTokenLength = 43
	// SessionLifetime is the sliding lifetime of a server-side session.
	SessionLifetime = 14 * 24 * time.Hour
	// SessionSlideInterval is the minimum gap between session expiry writes.
	SessionSlideInterval = time.Minute
)

// Session is an authenticated server-side session. The opaque id is not stored.
type Session struct {
	ID         int64
	UserID     int64
	ExpiresAt  time.Time
	LastSeenAt time.Time
	CreatedAt  time.Time
	UserAgent  string
	IPAddress  string
}

var readRandom = rand.Read

// NewOpaqueToken returns a URL-safe token with OpaqueTokenBytes of entropy.
func NewOpaqueToken() (string, error) {
	buf := make([]byte, OpaqueTokenBytes)
	if _, err := readRandom(buf); err != nil {
		return "", fmt.Errorf("identity: generate opaque token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// ValidOpaqueToken reports whether value is a token produced by NewOpaqueToken.
func ValidOpaqueToken(value string) bool {
	if len(value) != EncodedOpaqueTokenLength {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == OpaqueTokenBytes
}

// HashSessionToken returns the SHA-256 digest stored for an opaque session id.
func HashSessionToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	hash := make([]byte, len(sum))
	copy(hash, sum[:])
	return hash
}
