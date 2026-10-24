// Package crypto implements envelope encryption for persisted application secrets.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	dataKeySize   = 32
	masterKeySize = 32
)

var (
	// ErrUnknownKeyID means the sealed value refers to a master key that is not
	// present in the configured keyring.
	ErrUnknownKeyID = errors.New("crypto: unknown master key id")
	// ErrAuthentication means ciphertext, wrapped key, or associated data failed
	// AES-GCM authentication.
	ErrAuthentication = errors.New("crypto: authentication failed")
	// ErrInvalidSealed means the persisted envelope is incomplete or malformed.
	ErrInvalidSealed = errors.New("crypto: invalid sealed value")
)

// Seal encrypts plaintext with a fresh random data key and wraps that key with
// the active master key. The supplied associated data must be provided again
// to Open and Rewrap.
func (k *Keyring) Seal(plaintext, aad []byte) (Sealed, error) {
	if k == nil {
		return Sealed{}, errors.New("crypto: nil keyring")
	}

	dek := make([]byte, dataKeySize)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		wipe(dek)
		return Sealed{}, fmt.Errorf("crypto: generate data key: %w", err)
	}
	defer wipe(dek)

	dataAEAD, err := newAEAD(dek)
	if err != nil {
		return Sealed{}, err
	}
	nonce := make([]byte, dataAEAD.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return Sealed{}, fmt.Errorf("crypto: generate data nonce: %w", err)
	}
	ciphertext := dataAEAD.Seal(nil, nonce, plaintext, aad)

	wrappedDEK, err := k.wrapDEK(dek, aad)
	if err != nil {
		return Sealed{}, err
	}
	return Sealed{
		KeyID:      k.activeID,
		Nonce:      nonce,
		Ciphertext: ciphertext,
		WrappedDEK: wrappedDEK,
	}, nil
}

// Open authenticates and decrypts a sealed value using its recorded master
// key and the associated data supplied when it was sealed.
func (k *Keyring) Open(sealed Sealed, aad []byte) ([]byte, error) {
	if k == nil {
		return nil, errors.New("crypto: nil keyring")
	}
	if err := validateSealed(sealed); err != nil {
		return nil, err
	}
	dek, err := k.unwrapDEK(sealed, aad)
	if err != nil {
		return nil, err
	}
	defer wipe(dek)

	dataAEAD, err := newAEAD(dek)
	if err != nil {
		return nil, err
	}
	plaintext, err := dataAEAD.Open(nil, sealed.Nonce, sealed.Ciphertext, aad)
	if err != nil {
		return nil, ErrAuthentication
	}
	return plaintext, nil
}

// Rewrap changes only the encryption of the data key. The ciphertext and data
// nonce remain byte-for-byte unchanged, and associated data is authenticated
// while unwrapping and wrapping the key.
func (k *Keyring) Rewrap(sealed Sealed, aad []byte) (Sealed, error) {
	if k == nil {
		return Sealed{}, errors.New("crypto: nil keyring")
	}
	if err := validateSealed(sealed); err != nil {
		return Sealed{}, err
	}
	dek, err := k.unwrapDEK(sealed, aad)
	if err != nil {
		return Sealed{}, err
	}
	defer wipe(dek)

	wrappedDEK, err := k.wrapDEK(dek, aad)
	if err != nil {
		return Sealed{}, err
	}
	rotated := sealed.clone()
	rotated.KeyID = k.activeID
	rotated.WrappedDEK = wrappedDEK
	return rotated, nil
}

func (k *Keyring) unwrapDEK(sealed Sealed, aad []byte) ([]byte, error) {
	masterKey, ok := k.keys[sealed.KeyID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownKeyID, sealed.KeyID)
	}
	masterAEAD, err := newAEAD(masterKey)
	if err != nil {
		return nil, err
	}
	nonceSize := masterAEAD.NonceSize()
	nonce := sealed.WrappedDEK[:nonceSize]
	ciphertext := sealed.WrappedDEK[nonceSize:]
	dek, err := masterAEAD.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrAuthentication
	}
	if len(dek) != dataKeySize {
		wipe(dek)
		return nil, ErrInvalidSealed
	}
	return dek, nil
}

func (k *Keyring) wrapDEK(dek, aad []byte) ([]byte, error) {
	masterKey := k.keys[k.activeID]
	masterAEAD, err := newAEAD(masterKey)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, masterAEAD.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("crypto: generate wrapping nonce: %w", err)
	}
	wrapper := masterAEAD.Seal(nil, nonce, dek, aad)
	wrapped := make([]byte, 0, len(nonce)+len(wrapper))
	wrapped = append(wrapped, nonce...)
	wrapped = append(wrapped, wrapper...)
	return wrapped, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: initialize AES-256: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: initialize AES-GCM: %w", err)
	}
	return aead, nil
}

func validateSealed(sealed Sealed) error {
	if sealed.KeyID == "" || len(sealed.Nonce) != 12 || len(sealed.Ciphertext) < 16 || len(sealed.WrappedDEK) != 12+dataKeySize+16 {
		return ErrInvalidSealed
	}
	return nil
}

// RowAAD returns unambiguous associated data for a table and row identifier.
// Use the same table spelling and row ID representation when sealing and
// opening a value. Length-prefixing prevents delimiter ambiguity.
func RowAAD(table, rowID string) []byte {
	aad := make([]byte, 8+len(table)+len(rowID))
	binary.BigEndian.PutUint32(aad[:4], uint32(len(table)))
	copy(aad[4:], table)
	rowOffset := 4 + len(table)
	binary.BigEndian.PutUint32(aad[rowOffset:rowOffset+4], uint32(len(rowID)))
	copy(aad[rowOffset+4:], rowID)
	return aad
}

// IsActive reports whether a sealed value uses the keyring's active key.
func (k *Keyring) IsActive(keyID string) bool {
	if k == nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(keyID), []byte(k.activeID)) == 1
}

func wipe(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
