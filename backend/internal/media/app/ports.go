// Package app defines media storage ports.
package app

import (
	"context"
	"io"
	"time"

	"github.com/jooservices/xflickr/internal/catalog/domain"
)

// BlobInfo describes a stored temporary blob.
type BlobInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// BlobStore stores temporary files behind a single storage abstraction.
type BlobStore interface {
	Put(context.Context, string, io.Reader) error
	Get(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
	PresignGet(context.Context, string, time.Duration) (string, error)
	Stat(context.Context, string) (BlobInfo, error)
	Usage(context.Context) (int64, error)
	Full(context.Context) (bool, error)
}

// AssetRepository lists and removes persisted temporary-file metadata.
type AssetRepository interface {
	ExpiredAssets(context.Context, int) ([]domain.Asset, error)
	DeleteAsset(context.Context, int64) error
}
