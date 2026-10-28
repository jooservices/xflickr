// Package providertest contains reusable conformance checks for storage
// provider adapters. Provider implementations should call Run from their own
// test package with a fresh provider for each scenario.
package providertest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/jooservices/xflickr/internal/sync/app"
)

// Scenario configures deterministic failures for operations exercised by the
// contract. A new provider is created for each scenario and contract case.
type Scenario struct {
	UploadErrors []error
}

// Factory creates a fresh, non-nil provider for one contract test case.
type Factory func(testing.TB, Scenario) app.StorageProvider

// Run checks the behavior every storage provider adapter must share.
func Run(t *testing.T, factory Factory) {
	t.Helper()
	check(t, factory != nil, "provider contract requires a factory")
	newProvider := func(t *testing.T, scenario Scenario) app.StorageProvider {
		t.Helper()
		provider := factory(t, scenario)
		check(t, provider != nil, "provider contract factory returned nil")
		return provider
	}

	tests := []struct {
		name string
		run  func(*testing.T)
	}{
		{
			name: "capabilities",
			run: func(t *testing.T) {
				capabilities := newProvider(t, Scenario{}).Capabilities()
				err := capabilities.Validate()
				check(t, err == nil, "Capabilities().Validate() error = %v", err)
			},
		},
		{
			name: "ensure album is idempotent",
			run: func(t *testing.T) {
				provider := newProvider(t, Scenario{})
				first, err := provider.EnsureAlbum(context.Background(), "Contract album")
				check(t, err == nil, "first EnsureAlbum() error = %v", err)
				check(t, first.Valid(), "first EnsureAlbum() returned an empty identifier")
				second, err := provider.EnsureAlbum(context.Background(), "Contract album")
				check(t, err == nil, "second EnsureAlbum() error = %v", err)
				check(t, second == first, "EnsureAlbum() identifiers differ: first %q, second %q", first, second)
			},
		},
		{
			name: "upload exists and delete",
			run: func(t *testing.T) {
				provider := newProvider(t, Scenario{})
				albumID := ensureContractAlbum(t, provider)
				capabilities := provider.Capabilities()
				itemID, err := provider.Upload(context.Background(), albumID, contractAsset())
				check(t, err == nil, "Upload() error = %v", err)
				check(t, itemID.Valid(), "Upload() returned an empty identifier")
				exists, err := provider.Exists(context.Background(), itemID)
				check(t, err == nil && exists, "Exists() = %t, %v; want true, nil", exists, err)
				if capabilities.SupportsDelete {
					err := provider.Delete(context.Background(), itemID)
					check(t, err == nil, "Delete() error = %v", err)
					exists, err = provider.Exists(context.Background(), itemID)
					check(t, err == nil && !exists, "Exists() after Delete() = %t, %v; want false, nil", exists, err)
				} else {
					err := provider.Delete(context.Background(), itemID)
					check(t, errors.Is(err, app.ErrUnsupported), "Delete() on provider without delete support error = %v; want ErrUnsupported", err)
				}
			},
		},
		{
			name: "rate limit can be retried",
			run: func(t *testing.T) {
				provider := newProvider(t, Scenario{UploadErrors: []error{app.ErrRateLimited}})
				albumID := ensureContractAlbum(t, provider)
				itemID, err := provider.Upload(context.Background(), albumID, contractAsset())
				check(t, !itemID.Valid() && errors.Is(err, app.ErrRateLimited), "first Upload() = %q, %v; want empty ID and ErrRateLimited", itemID, err)
				itemID, err = provider.Upload(context.Background(), albumID, contractAsset())
				check(t, err == nil && itemID.Valid(), "retry Upload() = %q, %v; want a valid ID and nil", itemID, err)
			},
		},
		{
			name: "authentication failure is identifiable",
			run: func(t *testing.T) {
				provider := newProvider(t, Scenario{UploadErrors: []error{app.ErrAuthentication}})
				albumID := ensureContractAlbum(t, provider)
				itemID, err := provider.Upload(context.Background(), albumID, contractAsset())
				check(t, !itemID.Valid() && errors.Is(err, app.ErrAuthentication), "Upload() = %q, %v; want empty ID and ErrAuthentication", itemID, err)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, test.run)
	}
}

func ensureContractAlbum(t *testing.T, provider app.StorageProvider) app.RemoteAlbumID {
	t.Helper()
	albumID, err := provider.EnsureAlbum(context.Background(), "Contract album")
	check(t, err == nil, "EnsureAlbum() error = %v", err)
	check(t, albumID.Valid(), "EnsureAlbum() returned an empty identifier")
	return albumID
}

func check(t *testing.T, ok bool, message string, args ...any) {
	t.Helper()
	if !ok {
		t.Fatalf(message, args...)
	}
}

func contractAsset() app.Asset {
	content := []byte("provider contract asset")
	digest := sha256.Sum256(content)
	return app.Asset{
		Name:        "contract.txt",
		ContentType: "text/plain",
		Size:        int64(len(content)),
		SHA256:      hex.EncodeToString(digest[:]),
		Content:     bytes.NewReader(content),
	}
}
