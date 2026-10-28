package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

var ErrDownloadNotFound = errors.New("media: download not found")

const DefaultDownloadLinkTTL = 15 * time.Minute

// DownloadPart is persisted metadata for one temporary ZIP object.
type DownloadPart struct {
	DownloadID int64
	Number     int
	BlobKey    string
	Bytes      int64
	ExpiresAt  time.Time
}

// DownloadPartRepository owns user-scoped links, transfer acknowledgements,
// retries, and expiry state in PostgreSQL.
type DownloadPartRepository interface {
	PartsForDownload(context.Context, int64, int64) ([]DownloadPart, error)
	MarkPartFetched(context.Context, int64, int64, int) error
	RetryFailedDownload(context.Context, int64, int64) (int, error)
}

// DownloadPartLink is a short-lived URL the owner can use to fetch one ZIP.
type DownloadPartLink struct {
	Number    int
	URL       string
	Bytes     int64
	ExpiresAt time.Time
}

// DownloadPartLinks signs part URLs and records successful transfers.
type DownloadPartLinks struct {
	Repository DownloadPartRepository
	Blobs      BlobStore
	TTL        time.Duration
	Now        func() time.Time
}

// Get returns links only for a ready, unexpired download owned by userID.
func (s DownloadPartLinks) Get(ctx context.Context, userID, downloadID int64) ([]DownloadPartLink, error) {
	if ctx == nil || s.Repository == nil || s.Blobs == nil || userID <= 0 || downloadID <= 0 {
		return nil, errors.New("media: invalid download link request")
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	parts, err := s.Repository.PartsForDownload(ctx, userID, downloadID)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, ErrDownloadNotFound
	}
	ttl := s.TTL
	if ttl <= 0 {
		ttl = DefaultDownloadLinkTTL
	}
	links := make([]DownloadPartLink, 0, len(parts))
	for _, part := range parts {
		link, err := createDownloadPartLink(ctx, s.Blobs, part, now, ttl)
		if err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, nil
}

func createDownloadPartLink(ctx context.Context, blobs BlobStore, part DownloadPart, now time.Time, ttl time.Duration) (DownloadPartLink, error) {
	if part.Number <= 0 || part.BlobKey == "" || part.Bytes < 0 || part.ExpiresAt.IsZero() {
		return DownloadPartLink{}, errors.New("media: invalid download part metadata")
	}
	partTTL := part.ExpiresAt.Sub(now)
	if partTTL <= 0 {
		return DownloadPartLink{}, ErrDownloadNotFound
	}
	if partTTL > ttl {
		partTTL = ttl
	}
	url, err := blobs.PresignGet(ctx, part.BlobKey, partTTL)
	if errors.Is(err, ErrNotFound) {
		// The expiry worker may remove a part after the repository read.
		// Treat that race as an unavailable download, never as a 500.
		return DownloadPartLink{}, ErrDownloadNotFound
	}
	if err != nil {
		return DownloadPartLink{}, fmt.Errorf("media: create download part link: %w", err)
	}
	return DownloadPartLink{Number: part.Number, URL: url, Bytes: part.Bytes, ExpiresAt: now.Add(partTTL)}, nil
}

// MarkFetched acknowledges one successful part transfer. The acknowledgement
// is idempotent; the expiry worker removes the package after its final part.
func (s DownloadPartLinks) MarkFetched(ctx context.Context, userID, downloadID int64, partNumber int) error {
	if ctx == nil || s.Repository == nil || userID <= 0 || downloadID <= 0 || partNumber <= 0 {
		return errors.New("media: invalid fetched-part acknowledgement")
	}
	return s.Repository.MarkPartFetched(ctx, userID, downloadID, partNumber)
}

type FailedDownloadRepository interface {
	RetryFailedDownload(context.Context, int64, int64) (int, error)
}

// RetryFailed requeues only items still marked failed. Its repository performs
// the status changes and queue inserts in one transaction.
type RetryFailed struct {
	Repository FailedDownloadRepository
}

func (s RetryFailed) Run(ctx context.Context, userID, downloadID int64) (int, error) {
	if ctx == nil || s.Repository == nil || userID <= 0 || downloadID <= 0 {
		return 0, errors.New("media: invalid failed-download retry")
	}
	return s.Repository.RetryFailedDownload(ctx, userID, downloadID)
}

type ExpiredDownloadPart struct {
	DownloadID int64
	Number     int
	BlobKey    string
}

type DownloadExpiryRepository interface {
	ExpiredDownloadParts(context.Context, int) ([]ExpiredDownloadPart, error)
	DeleteDownloadPart(context.Context, int64, int) error
	MarkDownloadExpired(context.Context, int64) error
	MarkExpiredDownloads(context.Context, int) error
}

// DownloadExpiryCleaner removes only ZIP parts that belong to a completed
// download whose parts are all fetched or whose download/part TTL elapsed.
type DownloadExpiryCleaner struct {
	Repository DownloadExpiryRepository
	Blobs      BlobStore
	Interval   time.Duration
	Batch      int
	Logger     *slog.Logger
}

func (c DownloadExpiryCleaner) RunOnce(ctx context.Context) error {
	if ctx == nil || c.Repository == nil || c.Blobs == nil {
		return errors.New("media: download expiry cleaner requires context, repository, and blob store")
	}
	batch := c.Batch
	if batch <= 0 {
		batch = DefaultBatchSize
	}
	parts, err := c.Repository.ExpiredDownloadParts(ctx, batch)
	if err != nil {
		return fmt.Errorf("media: select expired download parts: %w", err)
	}
	downloads := make(map[int64]struct{}, len(parts))
	var failures []error
	for _, part := range parts {
		downloads[part.DownloadID] = struct{}{}
		if err := c.deleteExpiredDownloadPart(ctx, part); err != nil {
			failures = append(failures, err)
		}
	}
	for downloadID := range downloads {
		if err := c.Repository.MarkDownloadExpired(ctx, downloadID); err != nil {
			failures = append(failures, fmt.Errorf("mark download %d expired: %w", downloadID, err))
		}
	}
	if err := c.Repository.MarkExpiredDownloads(ctx, batch); err != nil {
		failures = append(failures, fmt.Errorf("mark partless downloads expired: %w", err))
	}
	return errors.Join(failures...)
}

func (c DownloadExpiryCleaner) deleteExpiredDownloadPart(ctx context.Context, part ExpiredDownloadPart) error {
	if part.DownloadID <= 0 || part.Number <= 0 || part.BlobKey == "" {
		return errors.New("media: invalid expired download part")
	}
	if err := c.Blobs.Delete(ctx, part.BlobKey); err != nil && !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrInvalidKey) {
		return fmt.Errorf("delete download part %q: %w", part.BlobKey, err)
	}
	if err := c.Repository.DeleteDownloadPart(ctx, part.DownloadID, part.Number); err != nil {
		return fmt.Errorf("remove download part %d/%d: %w", part.DownloadID, part.Number, err)
	}
	return nil
}

func (c DownloadExpiryCleaner) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("media: download expiry cleaner requires context")
	}
	if c.Interval <= 0 {
		c.Interval = DefaultCleanInterval
	}
	logger := c.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if err := c.RunOnce(ctx); err != nil && ctx.Err() == nil {
		logger.ErrorContext(ctx, "download expiry cleanup failed", "error", err)
	}
	ticker := time.NewTicker(c.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := c.RunOnce(ctx); err != nil && ctx.Err() == nil {
				logger.ErrorContext(ctx, "download expiry cleanup failed", "error", err)
			}
		}
	}
}
