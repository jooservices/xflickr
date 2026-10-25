package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

const (
	DefaultCleanInterval = 5 * time.Minute
	DefaultBatchSize     = 100
)

// Cleaner removes expired temporary blobs and their metadata.
type Cleaner struct {
	Assets   AssetRepository
	Blobs    BlobStore
	Interval time.Duration
	Batch    int
	Logger   *slog.Logger
}

// RunOnce deletes expired blobs before removing their database rows. A row is
// retained when deleting its blob fails so a later pass can retry it.
func (c Cleaner) RunOnce(ctx context.Context) error {
	if ctx == nil || c.Assets == nil || c.Blobs == nil {
		return errors.New("media: cleaner requires context, assets, and blob store")
	}
	batch := c.Batch
	if batch <= 0 {
		batch = DefaultBatchSize
	}
	assets, err := c.Assets.ExpiredAssets(ctx, batch)
	if err != nil {
		return fmt.Errorf("media: select expired assets: %w", err)
	}
	var failures []error
	for _, asset := range assets {
		if strings.TrimSpace(asset.BlobKey) != "" {
			if err := c.Blobs.Delete(ctx, asset.BlobKey); err != nil && !errors.Is(err, ErrInvalidKey) {
				failures = append(failures, fmt.Errorf("delete blob %q: %w", asset.BlobKey, err))
				continue
			}
		}
		if err := c.Assets.DeleteAsset(ctx, asset.ID); err != nil {
			failures = append(failures, fmt.Errorf("delete asset %d: %w", asset.ID, err))
		}
	}
	return errors.Join(failures...)
}

// Run performs an immediate cleanup and then repeats at the configured interval.
func (c Cleaner) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("media: cleaner requires context")
	}
	if c.Interval <= 0 {
		c.Interval = DefaultCleanInterval
	}
	logger := c.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if err := c.RunOnce(ctx); err != nil && ctx.Err() == nil {
		logger.ErrorContext(ctx, "media cleaner pass failed", "error", err)
	}
	ticker := time.NewTicker(c.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := c.RunOnce(ctx); err != nil && ctx.Err() == nil {
				logger.ErrorContext(ctx, "media cleaner pass failed", "error", err)
			}
		}
	}
}
