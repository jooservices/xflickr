package app

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRemoteIDsValidateNonEmptyValues(t *testing.T) {
	if !RemoteAlbumID("album-1").Valid() || RemoteAlbumID(" \t ").Valid() {
		t.Fatal("RemoteAlbumID.Valid() did not distinguish populated and empty identifiers")
	}
	if !RemoteItemID("item-1").Valid() || RemoteItemID("").Valid() {
		t.Fatal("RemoteItemID.Valid() did not distinguish populated and empty identifiers")
	}
}

func TestCapabilitiesValidateMaximumAssetSize(t *testing.T) {
	for _, capabilities := range []Capabilities{{}, {SupportsAlbums: true, SupportsDelete: true, MaxAssetBytes: 1024}} {
		if err := capabilities.Validate(); err != nil {
			t.Errorf("Capabilities(%+v).Validate() error = %v", capabilities, err)
		}
	}
	if err := (Capabilities{MaxAssetBytes: -1}).Validate(); !errors.Is(err, ErrInvalidCapabilities) {
		t.Fatalf("negative maximum asset size error = %v", err)
	}
}

func TestAssetValidateRequiredMetadataAndChecksum(t *testing.T) {
	valid := Asset{Name: "photo.jpg", ContentType: "image/jpeg", Size: 4, SHA256: strings.Repeat("a", 64), Content: bytes.NewReader([]byte("data"))}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid asset error = %v", err)
	}

	tests := []struct {
		name  string
		asset Asset
	}{
		{name: "empty name", asset: Asset{ContentType: valid.ContentType, Size: valid.Size, SHA256: valid.SHA256, Content: bytes.NewReader(nil)}},
		{name: "empty content type", asset: Asset{Name: valid.Name, Size: valid.Size, SHA256: valid.SHA256, Content: bytes.NewReader(nil)}},
		{name: "negative size", asset: Asset{Name: valid.Name, ContentType: valid.ContentType, Size: -1, SHA256: valid.SHA256, Content: bytes.NewReader(nil)}},
		{name: "missing content", asset: Asset{Name: valid.Name, ContentType: valid.ContentType, Size: valid.Size, SHA256: valid.SHA256}},
		{name: "short checksum", asset: Asset{Name: valid.Name, ContentType: valid.ContentType, Size: valid.Size, SHA256: "abc", Content: bytes.NewReader(nil)}},
		{name: "non-hex checksum", asset: Asset{Name: valid.Name, ContentType: valid.ContentType, Size: valid.Size, SHA256: strings.Repeat("z", 64), Content: bytes.NewReader(nil)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.asset.Validate(); !errors.Is(err, ErrInvalidAsset) {
				t.Fatalf("Validate() error = %v, want ErrInvalidAsset", err)
			}
		})
	}
}

func TestStorageErrorsAreDistinct(t *testing.T) {
	if ErrRateLimited == nil || ErrAuthentication == nil || errors.Is(ErrRateLimited, ErrAuthentication) {
		t.Fatal("rate-limit and authentication errors must be distinct sentinels")
	}
}
