package app_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"reflect"
	"testing"

	mediaapp "github.com/jooservices/xflickr/internal/media/app"
	"github.com/jooservices/xflickr/internal/sync/adapters/fake"
	"github.com/jooservices/xflickr/internal/sync/app"
)

func TestUploadVerifiedBlob(t *testing.T) {
	content := []byte("verified photo")
	blob, provider := newRig(content)
	provider.verifyAsset = true
	id, err := newEngine(t, provider, blob).Upload(t.Context(), inputFor(content))
	check(t, err == nil && id == "remote-new", "Upload() = %q, %v", id, err)
	check(t, reflect.DeepEqual(provider.uploaded, content) && provider.asset.Size == int64(len(content)) && provider.asset.SHA256 == digest(content) && provider.existsID == id, "asset = %q, size=%d, sha256=%q, exists ID=%q", provider.uploaded, provider.asset.Size, provider.asset.SHA256, provider.existsID)
	check(t, reflect.DeepEqual(*blob.events, []string{"get", "upload", "exists", "close", "delete"}), "call order = %v", *blob.events)
	check(t, !blob.exists, "verified blob was not deleted")
}

func TestUploadExistingRemoteSkipsUpload(t *testing.T) {
	for _, present := range []bool{true, false} {
		t.Run(map[bool]string{true: "blob present", false: "blob already gone"}[present], func(t *testing.T) {
			blob, provider := newRig(nil)
			blob.exists, provider.existsResults = present, []bool{true}
			in := inputFor(nil)
			in.ExistingID = "remote-old"
			id, err := newEngine(t, provider, blob).Upload(t.Context(), in)
			check(t, err == nil && id == in.ExistingID && blob.getCount == 0 && provider.uploadCount == 0 && blob.deleteCount == 1, "Upload() = %q, %v; gets=%d uploads=%d deletes=%d", id, err, blob.getCount, provider.uploadCount, blob.deleteCount)
		})
	}
}

func TestUploadMissingRemoteUploadsAgain(t *testing.T) {
	content := []byte("retry photo")
	blob, provider := newRig(content)
	provider.existsResults = []bool{false, true}
	in := inputFor(content)
	in.ExistingID = "remote-old"
	id, err := newEngine(t, provider, blob).Upload(t.Context(), in)
	check(t, err == nil && id == "remote-new" && provider.uploadCount == 1 && provider.existsCount == 2 && reflect.DeepEqual(provider.uploaded, content), "Upload() = %q, %v; uploads=%d exists=%d bytes=%q", id, err, provider.uploadCount, provider.existsCount, provider.uploaded)
}

func TestUploadPreservesBlobOnProviderFailure(t *testing.T) {
	providerError := errors.New("provider unavailable")
	for _, tc := range []struct {
		name                 string
		uploadID             app.RemoteItemID
		uploadErr, existsErr error
		exists               []bool
		wantID               app.RemoteItemID
		wantErr              error
	}{
		{name: "upload error", uploadErr: providerError, wantErr: providerError},
		{name: "rate limit with ID", uploadID: "remote-new", uploadErr: app.ErrRateLimited, wantID: "remote-new", wantErr: app.ErrRateLimited},
		{name: "exists error", existsErr: providerError, wantID: "remote-new", wantErr: providerError},
		{name: "exists false", exists: []bool{false}, wantID: "remote-new"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blob, provider := newRig([]byte("keep me"))
			provider.uploadID, provider.uploadErr, provider.existsErr, provider.existsResults = tc.uploadID, tc.uploadErr, tc.existsErr, tc.exists
			if !provider.uploadID.Valid() && tc.uploadErr == nil {
				provider.uploadID = "remote-new"
			}
			id, err := newEngine(t, provider, blob).Upload(t.Context(), inputFor([]byte("keep me")))
			check(t, err != nil && (tc.wantErr == nil || errors.Is(err, tc.wantErr)) && id == tc.wantID && blob.exists && blob.deleteCount == 0, "Upload() = %q, %v; want ID %q and retained blob", id, err, tc.wantID)
		})
	}
}

func TestUploadReportsCleanupFailure(t *testing.T) {
	cleanupError := errors.New("cleanup failed")
	for _, tc := range []struct {
		name                string
		closeErr, deleteErr error
		deletes             int
	}{
		{name: "reader close", closeErr: cleanupError},
		{name: "blob delete", deleteErr: cleanupError, deletes: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blob, provider := newRig([]byte("cleanup"))
			blob.closeErr, blob.deleteErr = tc.closeErr, tc.deleteErr
			id, err := newEngine(t, provider, blob).Upload(t.Context(), inputFor([]byte("cleanup")))
			check(t, id == "remote-new" && errors.Is(err, cleanupError) && blob.deleteCount == tc.deletes, "Upload() = %q, %v; delete count=%d", id, err, blob.deleteCount)
		})
	}
}

func TestUploadRejectsInvalidInputAndCapabilities(t *testing.T) {
	_, err := app.NewEngine(nil, &blobFake{})
	check(t, err != nil, "NewEngine accepted nil provider")
	_, err = app.NewEngine(&providerFake{}, nil)
	check(t, err != nil, "NewEngine accepted nil blob reader")
	for _, tc := range []struct {
		name              string
		ctx               context.Context
		key, album, asset bool
		limit             int64
	}{
		{name: "nil context"},
		{name: "blank key", ctx: t.Context(), key: true},
		{name: "invalid album", ctx: t.Context(), album: true},
		{name: "invalid metadata", ctx: t.Context(), asset: true},
		{name: "invalid capabilities", ctx: t.Context(), limit: -1},
		{name: "asset too large", ctx: t.Context(), limit: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blob, provider := newRig([]byte("valid asset"))
			in := inputFor([]byte("valid asset"))
			if tc.key {
				in.BlobKey = " \t"
			}
			if tc.album {
				in.AlbumID = " "
			}
			if tc.asset {
				in.Name = ""
			}
			provider.capabilities.MaxAssetBytes = tc.limit
			_, err := newEngine(t, provider, blob).Upload(tc.ctx, in)
			check(t, err != nil && provider.uploadCount == 0 && provider.existsCount == 0, "Upload() error=%v uploads=%d exists=%d", err, provider.uploadCount, provider.existsCount)
		})
	}
}

func TestUploadWithFakeProvider(t *testing.T) {
	content := []byte("fake provider round trip")
	provider := fake.New()
	albumID, err := provider.EnsureAlbum(t.Context(), "album")
	check(t, err == nil, "EnsureAlbum() error = %v", err)
	events := []string{}
	blob := &blobFake{data: content, exists: true, events: &events}
	engine := newEngine(t, provider, blob)
	in := inputFor(content)
	in.AlbumID = albumID
	first, err := engine.Upload(t.Context(), in)
	check(t, err == nil && first.Valid(), "first Upload() = %q, %v", first, err)
	in.ExistingID = first
	second, err := engine.Upload(t.Context(), in)
	check(t, err == nil && second == first && blob.getCount == 1, "second Upload() = %q, %v; gets=%d", second, err, blob.getCount)
}

func newEngine(t *testing.T, provider app.StorageProvider, blobs *blobFake) *app.Engine {
	t.Helper()
	engine, err := app.NewEngine(provider, blobs)
	check(t, err == nil, "NewEngine() error = %v", err)
	return engine
}

func check(t *testing.T, ok bool, message string, args ...any) {
	t.Helper()
	if !ok {
		t.Fatalf(message, args...)
	}
}

func newRig(data []byte) (*blobFake, *providerFake) {
	events := []string{}
	return &blobFake{data: data, exists: true, events: &events}, &providerFake{uploadID: "remote-new", events: &events}
}

func inputFor(data []byte) app.UploadInput {
	sum := sha256.Sum256(data)
	return app.UploadInput{BlobKey: "owned/blob", Name: "photo.jpg", ContentType: "image/jpeg", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data)), AlbumID: "album-1"}
}

type blobFake struct {
	data                  []byte
	exists                bool
	getCount, deleteCount int
	closeErr, deleteErr   error
	events                *[]string
}

func (b *blobFake) Get(_ context.Context, _ string) (io.ReadCloser, error) {
	b.getCount++
	b.event("get")
	return &readerFake{Reader: bytes.NewReader(b.data), blob: b}, nil
}

func (b *blobFake) Delete(_ context.Context, _ string) error {
	b.deleteCount++
	b.event("delete")
	if b.deleteErr != nil {
		return b.deleteErr
	}
	if !b.exists {
		return mediaapp.ErrNotFound
	}
	b.exists = false
	return nil
}
func (b *blobFake) event(name string) { *b.events = append(*b.events, name) }

type readerFake struct {
	*bytes.Reader
	blob *blobFake
}

func (r *readerFake) Close() error { r.blob.event("close"); return r.blob.closeErr }

type providerFake struct {
	app.StorageProvider
	capabilities             app.Capabilities
	uploadID                 app.RemoteItemID
	uploadErr, existsErr     error
	existsResults            []bool
	existsID                 app.RemoteItemID
	uploadCount, existsCount int
	asset                    app.Asset
	uploaded                 []byte
	verifyAsset              bool
	events                   *[]string
}

func (p *providerFake) Capabilities() app.Capabilities { return p.capabilities }
func (p *providerFake) Upload(_ context.Context, _ app.RemoteAlbumID, asset app.Asset) (app.RemoteItemID, error) {
	p.uploadCount++
	p.event("upload")
	p.asset = asset
	content, err := io.ReadAll(asset.Content)
	if err != nil {
		return "", err
	}
	p.uploaded = content
	if p.verifyAsset && (int64(len(p.uploaded)) != asset.Size || digest(p.uploaded) != asset.SHA256) {
		return "", app.ErrInvalidAsset
	}
	return p.uploadID, p.uploadErr
}

func (p *providerFake) Exists(_ context.Context, id app.RemoteItemID) (bool, error) {
	p.existsCount++
	p.existsID = id
	p.event("exists")
	if p.existsErr != nil {
		return false, p.existsErr
	}
	if len(p.existsResults) == 0 {
		return true, nil
	}
	result := p.existsResults[0]
	p.existsResults = p.existsResults[1:]
	return result, nil
}
func (p *providerFake) event(name string) { *p.events = append(*p.events, name) }
func digest(data []byte) string           { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

var _ app.StorageProvider = (*providerFake)(nil)
