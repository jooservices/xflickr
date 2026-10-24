package crypto

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
)

// Sealed is a JSONB-ready envelope. WrappedDEK contains the wrapping nonce
// followed by the AES-GCM wrapped data key; Nonce is the data encryption
// nonce. The slices are encoded as base64 by encoding/json.
type Sealed struct {
	KeyID      string `json:"key_id"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
	WrappedDEK []byte `json:"wrapped_dek"`
}

// Value implements driver.Valuer so Sealed can be stored as a PostgreSQL
// JSONB value.
func (s Sealed) Value() (driver.Value, error) {
	if err := validateSealed(s); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("crypto: encode sealed value: %w", err)
	}
	return encoded, nil
}

// Scan implements sql.Scanner for PostgreSQL JSONB and byte/string drivers.
func (s *Sealed) Scan(value any) error {
	if s == nil {
		return errors.New("crypto: cannot scan into nil Sealed")
	}
	if value == nil {
		*s = Sealed{}
		return nil
	}

	var encoded []byte
	switch value := value.(type) {
	case []byte:
		encoded = value
	case string:
		encoded = []byte(value)
	default:
		return fmt.Errorf("crypto: cannot scan %T into Sealed", value)
	}
	var scanned Sealed
	if err := json.Unmarshal(encoded, &scanned); err != nil {
		return fmt.Errorf("crypto: decode sealed value: %w", err)
	}
	if err := validateSealed(scanned); err != nil {
		return err
	}
	*s = scanned
	return nil
}

func (s Sealed) clone() Sealed {
	return Sealed{
		KeyID:      s.KeyID,
		Nonce:      append([]byte(nil), s.Nonce...),
		Ciphertext: append([]byte(nil), s.Ciphertext...),
		WrappedDEK: append([]byte(nil), s.WrappedDEK...),
	}
}
