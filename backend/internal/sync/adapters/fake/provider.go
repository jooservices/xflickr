// Package fake provides an in-memory cloud storage provider for contract tests.
package fake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/jooservices/xflickr/internal/sync/app"
)

var errContextRequired = errors.New("fake sync provider: context is required")

type storedItem struct {
	albumID app.RemoteAlbumID
	asset   []byte
}

// Provider keeps albums and uploaded items in memory. uploadErrors injects
// provider failures in order so callers can test retry and auth behavior.
type Provider struct {
	mu           sync.RWMutex
	albums       map[string]app.RemoteAlbumID
	albumNames   map[app.RemoteAlbumID]string
	items        map[app.RemoteItemID]storedItem
	uploadErrors []error
	nextAlbumID  int64
	nextItemID   int64
}

// New creates an in-memory provider and optionally queues upload failures.
func New(uploadErrors ...error) *Provider {
	return &Provider{
		albums:       make(map[string]app.RemoteAlbumID),
		albumNames:   make(map[app.RemoteAlbumID]string),
		items:        make(map[app.RemoteItemID]storedItem),
		uploadErrors: append([]error(nil), uploadErrors...),
	}
}

func (p *Provider) Capabilities() app.Capabilities {
	return app.Capabilities{SupportsAlbums: true, SupportsDelete: true}
}

func (p *Provider) EnsureAlbum(ctx context.Context, name string) (app.RemoteAlbumID, error) {
	if err := providerContextError(ctx); err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", app.ErrInvalidAlbum
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if id, ok := p.albums[name]; ok {
		return id, nil
	}
	p.nextAlbumID++
	id := app.RemoteAlbumID(fmt.Sprintf("fake-album-%d", p.nextAlbumID))
	p.albums[name] = id
	p.albumNames[id] = name
	return id, nil
}

func (p *Provider) Upload(ctx context.Context, albumID app.RemoteAlbumID, asset app.Asset) (app.RemoteItemID, error) {
	if err := providerContextError(ctx); err != nil {
		return "", err
	}
	if !albumID.Valid() {
		return "", app.ErrInvalidRemoteID
	}
	if err := asset.Validate(); err != nil {
		return "", err
	}

	p.mu.Lock()
	if _, ok := p.albumNames[albumID]; !ok {
		p.mu.Unlock()
		return "", app.ErrInvalidRemoteID
	}
	if len(p.uploadErrors) > 0 {
		err := p.uploadErrors[0]
		p.uploadErrors = p.uploadErrors[1:]
		p.mu.Unlock()
		if err != nil {
			return "", err
		}
	} else {
		p.mu.Unlock()
	}

	content, err := io.ReadAll(asset.Content)
	if err != nil {
		return "", fmt.Errorf("fake sync provider: read upload content: %w", err)
	}
	if int64(len(content)) != asset.Size {
		return "", fmt.Errorf("%w: content size does not match metadata", app.ErrInvalidAsset)
	}
	digest := sha256.Sum256(content)
	if !strings.EqualFold(asset.SHA256, hex.EncodeToString(digest[:])) {
		return "", fmt.Errorf("%w: content checksum does not match metadata", app.ErrInvalidAsset)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextItemID++
	id := app.RemoteItemID(fmt.Sprintf("fake-item-%d", p.nextItemID))
	p.items[id] = storedItem{albumID: albumID, asset: append([]byte(nil), content...)}
	return id, nil
}

func (p *Provider) Exists(ctx context.Context, itemID app.RemoteItemID) (bool, error) {
	if err := providerContextError(ctx); err != nil {
		return false, err
	}
	if !itemID.Valid() {
		return false, app.ErrInvalidRemoteID
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.items[itemID]
	return ok, nil
}

func (p *Provider) Delete(ctx context.Context, itemID app.RemoteItemID) error {
	if err := providerContextError(ctx); err != nil {
		return err
	}
	if !itemID.Valid() {
		return app.ErrInvalidRemoteID
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.items, itemID)
	return nil
}

func providerContextError(ctx context.Context) error {
	if ctx == nil {
		return errContextRequired
	}
	return ctx.Err()
}

var _ app.StorageProvider = (*Provider)(nil)
