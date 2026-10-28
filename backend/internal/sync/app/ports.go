// Package app defines the cloud storage provider port and shared value types.
package app

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

var (
	// ErrRateLimited means the provider rejected a request because of its rate limit.
	ErrRateLimited = errors.New("sync: storage provider rate limited")
	// ErrAuthentication means the provider credentials are no longer accepted.
	ErrAuthentication = errors.New("sync: storage provider authentication failed")
	// ErrInvalidAsset means an upload asset is missing required metadata or content.
	ErrInvalidAsset = errors.New("sync: invalid upload asset")
	// ErrInvalidAlbum means an album name is empty or invalid.
	ErrInvalidAlbum = errors.New("sync: invalid remote album")
	// ErrInvalidRemoteID means a remote identifier is empty or invalid.
	ErrInvalidRemoteID = errors.New("sync: invalid remote identifier")
	// ErrInvalidCapabilities means a provider reports an unusable capability limit.
	ErrInvalidCapabilities = errors.New("sync: invalid provider capabilities")
	// ErrUnsupported means the provider does not support the requested operation.
	ErrUnsupported = errors.New("sync: provider operation is unsupported")
)

// RemoteAlbumID identifies an album in a cloud storage provider.
type RemoteAlbumID string

// Valid reports whether the remote album identifier contains a value.
func (id RemoteAlbumID) Valid() bool { return strings.TrimSpace(string(id)) != "" }

// RemoteItemID identifies an uploaded item in a cloud storage provider.
type RemoteItemID string

// Valid reports whether the remote item identifier contains a value.
func (id RemoteItemID) Valid() bool { return strings.TrimSpace(string(id)) != "" }

// Capabilities describes provider limits and optional remote operations.
// A MaxAssetBytes value of zero means the provider does not publish a limit.
type Capabilities struct {
	SupportsAlbums bool
	SupportsDelete bool
	MaxAssetBytes  int64
}

// Validate rejects capability limits that cannot be enforced meaningfully.
func (c Capabilities) Validate() error {
	if c.MaxAssetBytes < 0 {
		return fmt.Errorf("%w: maximum asset size cannot be negative", ErrInvalidCapabilities)
	}
	return nil
}

// Asset is a verified local file ready to upload. Content is streamed and must
// be a fresh reader for each upload attempt.
type Asset struct {
	Name        string
	ContentType string
	Size        int64
	SHA256      string
	Content     io.Reader
}

// Validate checks the metadata required to upload and later verify an asset.
func (a Asset) Validate() error {
	if strings.TrimSpace(a.Name) == "" || strings.TrimSpace(a.ContentType) == "" || a.Size < 0 || a.Content == nil {
		return ErrInvalidAsset
	}
	if len(a.SHA256) != 64 {
		return fmt.Errorf("%w: SHA-256 must contain 64 hexadecimal characters", ErrInvalidAsset)
	}
	if _, err := hex.DecodeString(a.SHA256); err != nil {
		return fmt.Errorf("%w: SHA-256 must be hexadecimal", ErrInvalidAsset)
	}
	return nil
}

// StorageProvider stores and verifies media in a user's connected destination.
// Implementations must return ErrRateLimited and ErrAuthentication so callers
// can distinguish retryable throttling from credentials requiring repair.
// EnsureAlbum may map to the provider's default destination when it has no
// user-visible album support. Delete returns ErrUnsupported when
// Capabilities().SupportsDelete is false.
type StorageProvider interface {
	Capabilities() Capabilities
	EnsureAlbum(context.Context, string) (RemoteAlbumID, error)
	Upload(context.Context, RemoteAlbumID, Asset) (RemoteItemID, error)
	Exists(context.Context, RemoteItemID) (bool, error)
	Delete(context.Context, RemoteItemID) error
}
