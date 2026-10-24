package crypto

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

const (
	MasterKeysEnv = "XFLICKR_MASTER_KEYS"
	ActiveKeyEnv  = "XFLICKR_MASTER_KEY_ACTIVE"
)

var keyIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// Keyring holds configured master keys and the ID used for new envelopes.
// Keys are copied at construction time so callers cannot mutate the ring.
type Keyring struct {
	keys     map[string][]byte
	activeID string
}

// NewKeyring validates and creates a keyring from raw 32-byte AES-256 keys.
func NewKeyring(keys map[string][]byte, activeID string) (*Keyring, error) {
	if len(keys) == 0 {
		return nil, errors.New("crypto: at least one master key is required")
	}
	if !validKeyID(activeID) {
		return nil, errors.New("crypto: active master key id is invalid")
	}
	cloned := make(map[string][]byte, len(keys))
	for id, key := range keys {
		if !validKeyID(id) {
			return nil, errors.New("crypto: master key id is invalid")
		}
		if len(key) != masterKeySize {
			return nil, fmt.Errorf("crypto: master key %q must be 32 bytes", id)
		}
		cloned[id] = append([]byte(nil), key...)
	}
	if _, ok := cloned[activeID]; !ok {
		return nil, errors.New("crypto: active master key is not configured")
	}
	return &Keyring{keys: cloned, activeID: activeID}, nil
}

// NewKeyringFromEnv reads the comma-separated id:base64 key list and active
// key ID from the required application environment variables.
func NewKeyringFromEnv() (*Keyring, error) {
	keysValue, keysOK := os.LookupEnv(MasterKeysEnv)
	activeID, activeOK := os.LookupEnv(ActiveKeyEnv)
	if !keysOK || strings.TrimSpace(keysValue) == "" {
		return nil, fmt.Errorf("crypto: %s is required", MasterKeysEnv)
	}
	if !activeOK || strings.TrimSpace(activeID) == "" {
		return nil, fmt.Errorf("crypto: %s is required", ActiveKeyEnv)
	}
	keys, err := ParseMasterKeys(keysValue)
	if err != nil {
		return nil, err
	}
	return NewKeyring(keys, strings.TrimSpace(activeID))
}

// ParseMasterKeys parses an id:base64,id:base64 key list. Key material is
// never included in errors.
func ParseMasterKeys(value string) (map[string][]byte, error) {
	keys := make(map[string][]byte)
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			return nil, errors.New("crypto: master key list contains an empty entry")
		}
		id, encoded, found := strings.Cut(entry, ":")
		id = strings.TrimSpace(id)
		encoded = strings.TrimSpace(encoded)
		if !found || !validKeyID(id) || encoded == "" {
			return nil, errors.New("crypto: master key entry must be id:base64")
		}
		if _, exists := keys[id]; exists {
			return nil, fmt.Errorf("crypto: duplicate master key id %q", id)
		}
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, errors.New("crypto: master key value must be valid base64")
		}
		if len(key) != masterKeySize {
			wipe(key)
			return nil, fmt.Errorf("crypto: master key %q must decode to 32 bytes", id)
		}
		keys[id] = key
	}
	if len(keys) == 0 {
		return nil, errors.New("crypto: at least one master key is required")
	}
	return keys, nil
}

func validKeyID(id string) bool {
	return keyIDPattern.MatchString(id)
}

// ActiveKeyID returns the ID selected for newly sealed values.
func (k *Keyring) ActiveKeyID() string {
	if k == nil {
		return ""
	}
	return k.activeID
}
