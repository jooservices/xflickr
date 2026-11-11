package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	mediaapp "github.com/jooservices/xflickr/internal/media/app"
)

// BlobReader reads and removes an exclusively owned temporary upload blob.
type BlobReader interface {
	Get(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}

// UploadInput describes a fetched blob and its destination metadata.
type UploadInput struct {
	BlobKey     string
	Name        string
	ContentType string
	SHA256      string
	Size        int64
	AlbumID     RemoteAlbumID
	ExistingID  RemoteItemID
}

// Engine uploads owned temporary blobs and removes them after remote
// existence has been confirmed.
type Engine struct {
	provider StorageProvider
	blobs    BlobReader
}

// NewEngine validates and stores the provider and temporary blob reader.
func NewEngine(provider StorageProvider, blobs BlobReader) (*Engine, error) {
	if provider == nil || blobs == nil {
		return nil, errors.New("sync: engine requires a provider and blob reader")
	}
	return &Engine{provider: provider, blobs: blobs}, nil
}

// Upload uploads one fetched blob, verifies that its remote ID exists, and
// deletes the temporary blob only after verification succeeds.
func (e *Engine) Upload(ctx context.Context, in UploadInput) (RemoteItemID, error) {
	if ctx == nil {
		return "", errors.New("sync: upload requires a context")
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("sync: upload: %w", err)
	}
	if e == nil || e.provider == nil || e.blobs == nil {
		return "", errors.New("sync: upload requires an initialized engine")
	}
	if strings.TrimSpace(in.BlobKey) == "" || !in.AlbumID.Valid() {
		return "", errors.New("sync: invalid upload input")
	}

	if in.ExistingID.Valid() {
		exists, err := e.provider.Exists(ctx, in.ExistingID)
		if err != nil {
			return in.ExistingID, fmt.Errorf("sync: check existing remote item: %w", err)
		}
		if exists {
			if err := deleteBlob(ctx, e.blobs, in.BlobKey, true); err != nil {
				return in.ExistingID, err
			}
			return in.ExistingID, nil
		}
	}

	capabilities := e.provider.Capabilities()
	if err := capabilities.Validate(); err != nil {
		return "", fmt.Errorf("sync: validate provider capabilities: %w", err)
	}
	if capabilities.MaxAssetBytes > 0 && in.Size > capabilities.MaxAssetBytes {
		return "", fmt.Errorf("sync: asset size %d exceeds provider limit %d", in.Size, capabilities.MaxAssetBytes)
	}

	reader, err := e.blobs.Get(ctx, in.BlobKey)
	if err != nil {
		return "", fmt.Errorf("sync: open temporary blob: %w", err)
	}
	if reader == nil {
		return "", errors.New("sync: blob reader returned no content")
	}
	asset := Asset{
		Name: in.Name, ContentType: in.ContentType, Size: in.Size,
		SHA256: in.SHA256, Content: reader,
	}
	if err := asset.Validate(); err != nil {
		return "", errors.Join(fmt.Errorf("sync: validate upload asset: %w", err), closeReader(reader))
	}

	id, uploadErr := e.provider.Upload(ctx, in.AlbumID, asset)
	if !id.Valid() {
		id = ""
	}
	if uploadErr != nil {
		return id, errors.Join(fmt.Errorf("sync: upload temporary blob: %w", uploadErr), closeReader(reader))
	}
	if !id.Valid() {
		return "", errors.Join(errors.New("sync: provider returned an invalid remote item ID"), closeReader(reader))
	}

	exists, err := e.provider.Exists(ctx, id)
	if err != nil {
		return id, errors.Join(fmt.Errorf("sync: verify remote item: %w", err), closeReader(reader))
	}
	if !exists {
		return id, errors.Join(errors.New("sync: uploaded remote item does not exist"), closeReader(reader))
	}
	if err := closeReader(reader); err != nil {
		return id, err
	}
	if err := deleteBlob(ctx, e.blobs, in.BlobKey, false); err != nil {
		return id, err
	}
	return id, nil
}

func closeReader(reader io.ReadCloser) error {
	if err := reader.Close(); err != nil {
		return fmt.Errorf("sync: close temporary blob: %w", err)
	}
	return nil
}

func deleteBlob(ctx context.Context, blobs BlobReader, key string, missingIsSuccess bool) error {
	if err := blobs.Delete(ctx, key); err != nil {
		if missingIsSuccess && errors.Is(err, mediaapp.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("sync: delete temporary blob: %w", err)
	}
	return nil
}
