// Package domain contains node registry rules and token handling.
package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

type State string

const (
	StateOnline   State = "online"
	StateWarning  State = "warning"
	StateCooling  State = "cooling"
	StateOffline  State = "offline"
	StateDisabled State = "disabled"

	NodeTokenBytes         = 32
	EncodedNodeTokenLength = 43
	WarningAfter           = time.Minute
	OfflineAfter           = 5 * time.Minute
)

// Node stores public node metadata. Token material is kept separately as a
// hash and is never included in this type.
type Node struct {
	Name            string
	Roles           []string
	Region          string
	Version         string
	EgressIP        string
	State           State
	CoolingUntil    *time.Time
	LastHeartbeatAt *time.Time
	Metrics         json.RawMessage
}

var readRandom = rand.Read

// NewNodeToken returns a URL-safe bearer token with 256 bits of entropy.
func NewNodeToken() (string, error) {
	value := make([]byte, NodeTokenBytes)
	if _, err := readRandom(value); err != nil {
		return "", fmt.Errorf("cluster: generate node token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

// ValidNodeToken reports whether value is a token produced by NewNodeToken.
func ValidNodeToken(value string) bool {
	if len(value) != EncodedNodeTokenLength {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == NodeTokenBytes
}

// HashNodeToken returns the SHA-256 digest stored in the database.
func HashNodeToken(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return append([]byte(nil), sum[:]...)
}

// EffectiveState derives heartbeat health without rewriting persisted
// operational state. A disabled node stays disabled; an active cooling window
// takes precedence over heartbeat age.
func (node Node) EffectiveState(now time.Time) State {
	if node.State == StateDisabled {
		return StateDisabled
	}
	if node.CoolingUntil != nil && now.Before(*node.CoolingUntil) {
		return StateCooling
	}
	if node.LastHeartbeatAt == nil {
		return StateOffline
	}
	age := now.Sub(*node.LastHeartbeatAt)
	if age > OfflineAfter {
		return StateOffline
	}
	if age > WarningAfter {
		return StateWarning
	}
	return StateOnline
}
