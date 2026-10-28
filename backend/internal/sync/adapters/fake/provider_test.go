package fake

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/jooservices/xflickr/internal/sync/app"
	"github.com/jooservices/xflickr/internal/sync/providertest"
)

func TestProviderSatisfiesContract(t *testing.T) {
	providertest.Run(t, func(_ testing.TB, scenario providertest.Scenario) app.StorageProvider {
		return New(scenario.UploadErrors...)
	})
}

func TestEnsureAlbumValidationAndIdempotency(t *testing.T) {
	provider := New()
	if _, err := provider.EnsureAlbum(nil, "album"); !errors.Is(err, errContextRequired) {
		t.Fatalf("EnsureAlbum(nil, ...) error = %v", err)
	}
	if _, err := provider.EnsureAlbum(context.Background(), " \t "); !errors.Is(err, app.ErrInvalidAlbum) {
		t.Fatalf("EnsureAlbum(blank, ...) error = %v", err)
	}

	first, err := provider.EnsureAlbum(context.Background(), "  family  ")
	if err != nil {
		t.Fatalf("EnsureAlbum() error = %v", err)
	}
	second, err := provider.EnsureAlbum(context.Background(), "family")
	if err != nil || second != first {
		t.Fatalf("second EnsureAlbum() = %q, %v; want %q, nil", second, err, first)
	}
}

func TestUploadValidatesInputsAndVerifiesContent(t *testing.T) {
	provider := New()
	ctx := context.Background()
	albumID, err := provider.EnsureAlbum(ctx, "album")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := provider.Upload(nil, albumID, validAsset("data")); !errors.Is(err, errContextRequired) {
		t.Fatalf("Upload(nil, ...) error = %v", err)
	}
	if _, err := provider.Upload(ctx, " ", validAsset("data")); !errors.Is(err, app.ErrInvalidRemoteID) {
		t.Fatalf("Upload(blank album, ...) error = %v", err)
	}
	if _, err := provider.Upload(ctx, "missing-album", validAsset("data")); !errors.Is(err, app.ErrInvalidRemoteID) {
		t.Fatalf("Upload(unknown album, ...) error = %v", err)
	}
	if _, err := provider.Upload(ctx, albumID, app.Asset{}); !errors.Is(err, app.ErrInvalidAsset) {
		t.Fatalf("Upload(invalid asset) error = %v", err)
	}

	readerFailure := errors.New("reader failed")
	asset := validAsset("data")
	asset.Content = failingReader{err: readerFailure}
	if _, err := provider.Upload(ctx, albumID, asset); !errors.Is(err, readerFailure) {
		t.Fatalf("Upload(read error) error = %v", err)
	}

	wrongSize := validAsset("data")
	wrongSize.Size++
	if _, err := provider.Upload(ctx, albumID, wrongSize); !errors.Is(err, app.ErrInvalidAsset) {
		t.Fatalf("Upload(size mismatch) error = %v", err)
	}

	wrongDigest := validAsset("data")
	wrongDigest.SHA256 = strings.Repeat("0", 64)
	if _, err := provider.Upload(ctx, albumID, wrongDigest); !errors.Is(err, app.ErrInvalidAsset) {
		t.Fatalf("Upload(checksum mismatch) error = %v", err)
	}

	itemID, err := provider.Upload(ctx, albumID, validAsset("data"))
	if err != nil || !itemID.Valid() {
		t.Fatalf("Upload(valid asset) = %q, %v", itemID, err)
	}
}

func TestUploadFailureInjectionDoesNotConsumeAsset(t *testing.T) {
	provider := New(nil, app.ErrRateLimited, app.ErrAuthentication)
	ctx := context.Background()
	albumID, err := provider.EnsureAlbum(ctx, "album")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Upload(ctx, albumID, validAsset("one")); err != nil {
		t.Fatalf("nil queued failure should continue upload, got %v", err)
	}
	for _, want := range []error{app.ErrRateLimited, app.ErrAuthentication} {
		asset := validAsset("two")
		if itemID, err := provider.Upload(ctx, albumID, asset); !errors.Is(err, want) || itemID.Valid() {
			t.Fatalf("Upload() = %q, %v; want empty ID and %v", itemID, err, want)
		}
		if got, err := io.ReadAll(asset.Content); err != nil || string(got) != "two" {
			t.Fatalf("injected failure consumed content: %q, %v", got, err)
		}
	}
}

func TestExistsDeleteAndContextValidation(t *testing.T) {
	provider := New()
	if _, err := provider.Exists(nil, "item"); !errors.Is(err, errContextRequired) {
		t.Fatalf("Exists(nil, ...) error = %v", err)
	}
	if err := provider.Delete(nil, "item"); !errors.Is(err, errContextRequired) {
		t.Fatalf("Delete(nil, ...) error = %v", err)
	}
	if _, err := provider.Exists(context.Background(), " "); !errors.Is(err, app.ErrInvalidRemoteID) {
		t.Fatalf("Exists(blank ID) error = %v", err)
	}
	if err := provider.Delete(context.Background(), " "); !errors.Is(err, app.ErrInvalidRemoteID) {
		t.Fatalf("Delete(blank ID) error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.EnsureAlbum(ctx, "album"); !errors.Is(err, context.Canceled) {
		t.Fatalf("EnsureAlbum(canceled) error = %v", err)
	}
	if _, err := provider.Exists(ctx, "item"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Exists(canceled) error = %v", err)
	}
	if err := provider.Delete(ctx, "item"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete(canceled) error = %v", err)
	}

	albumID, err := provider.EnsureAlbum(context.Background(), "album")
	if err != nil {
		t.Fatal(err)
	}
	itemID, err := provider.Upload(context.Background(), albumID, validAsset("data"))
	if err != nil {
		t.Fatal(err)
	}
	if exists, err := provider.Exists(context.Background(), itemID); err != nil || !exists {
		t.Fatalf("Exists(uploaded item) = %t, %v", exists, err)
	}
	if err := provider.Delete(context.Background(), itemID); err != nil {
		t.Fatal(err)
	}
	if exists, err := provider.Exists(context.Background(), itemID); err != nil || exists {
		t.Fatalf("Exists(deleted item) = %t, %v", exists, err)
	}
	if err := provider.Delete(context.Background(), itemID); err != nil {
		t.Fatalf("repeated Delete() should be idempotent, got %v", err)
	}
}

func TestCapabilities(t *testing.T) {
	got := New().Capabilities()
	if err := got.Validate(); err != nil {
		t.Fatalf("Capabilities().Validate() error = %v", err)
	}
	if !got.SupportsAlbums || !got.SupportsDelete || got.MaxAssetBytes != 0 {
		t.Fatalf("unexpected fake capabilities: %+v", got)
	}
}

func validAsset(content string) app.Asset {
	data := []byte(content)
	digest := sha256.Sum256(data)
	return app.Asset{
		Name:        "asset.txt",
		ContentType: "text/plain",
		Size:        int64(len(data)),
		SHA256:      hex.EncodeToString(digest[:]),
		Content:     bytes.NewReader(data),
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }
