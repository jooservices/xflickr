// Package local implements the filesystem BlobStore adapter.
package local

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jooservices/xflickr/internal/media/app"
)

var ErrNotFound = errors.New("media: blob not found")

// BlobStore stores blobs under a local directory. A zero maxBytes disables the
// cap; negative caps are rejected by New.
type BlobStore struct {
	root     string
	maxBytes int64
}

// New creates a local filesystem store.
func New(root string, maxBytes int64) (*BlobStore, error) {
	if strings.TrimSpace(root) == "" || maxBytes < 0 {
		return nil, errors.New("media: invalid local blob store configuration")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("media: resolve blob directory: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("media: create blob directory: %w", err)
	}
	return &BlobStore{root: absolute, maxBytes: maxBytes}, nil
}

func (s *BlobStore) path(key string) (string, error) {
	if s == nil || s.root == "" || strings.TrimSpace(key) == "" || filepath.IsAbs(key) {
		return "", errors.New("media: invalid blob key")
	}
	clean := filepath.Clean(filepath.FromSlash(key))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("media: invalid blob key")
	}
	path := filepath.Join(s.root, clean)
	if !strings.HasPrefix(path, s.root+string(filepath.Separator)) {
		return "", errors.New("media: invalid blob key")
	}
	return path, nil
}

func (s *BlobStore) Put(ctx context.Context, key string, src io.Reader) error {
	if ctx == nil || src == nil {
		return errors.New("media: invalid blob put")
	}
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("media: create blob parent: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".xflickr-blob-*")
	if err != nil {
		return fmt.Errorf("media: create blob: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	_, copyErr := io.Copy(tmp, &contextReader{ctx: ctx, reader: src})
	closeErr := tmp.Close()
	if copyErr != nil {
		return fmt.Errorf("media: write blob: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("media: close blob: %w", closeErr)
	}
	if s.maxBytes > 0 {
		usage, err := s.Usage(ctx)
		if err != nil {
			return err
		}
		oldSize := int64(0)
		if info, err := os.Stat(path); err == nil {
			oldSize = info.Size()
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("media: stat existing blob: %w", err)
		}
		// Usage includes the temporary file written above, so subtract the old
		// target and compare with the final on-disk size.
		if usage-oldSize > s.maxBytes {
			return errors.New("media: blob store capacity exceeded")
		}
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("media: commit blob: %w", err)
	}
	return nil
}

func (s *BlobStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if ctx == nil {
		return nil, errors.New("media: invalid blob get")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.path(key)
	if err != nil {
		return nil, err
	}
	// key has been restricted to a relative path beneath the configured root.
	file, err := os.Open(path) // #nosec G304
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("media: open blob: %w", err)
	}
	return file, nil
}

func (s *BlobStore) Delete(ctx context.Context, key string) error {
	if ctx == nil {
		return errors.New("media: invalid blob delete")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("media: remove blob: %w", err)
	}
	return nil
}

func (s *BlobStore) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if ctx == nil || ttl <= 0 {
		return "", errors.New("media: invalid presign request")
	}
	if _, err := s.Stat(ctx, key); err != nil {
		return "", err
	}
	path, _ := s.path(key)
	return (&url.URL{Scheme: "file", Path: path, RawQuery: "expires=" + url.QueryEscape(time.Now().Add(ttl).UTC().Format(time.RFC3339))}).String(), nil
}

func (s *BlobStore) Stat(ctx context.Context, key string) (app.BlobInfo, error) {
	if ctx == nil {
		return app.BlobInfo{}, errors.New("media: invalid blob stat")
	}
	if err := ctx.Err(); err != nil {
		return app.BlobInfo{}, err
	}
	path, err := s.path(key)
	if err != nil {
		return app.BlobInfo{}, err
	}
	stat, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return app.BlobInfo{}, ErrNotFound
	}
	if err != nil {
		return app.BlobInfo{}, fmt.Errorf("media: stat blob: %w", err)
	}
	return app.BlobInfo{Key: key, Size: stat.Size(), LastModified: stat.ModTime()}, nil
}

func (s *BlobStore) Usage(ctx context.Context) (int64, error) {
	if s == nil || s.root == "" || ctx == nil {
		return 0, errors.New("media: invalid usage request")
	}
	var total int64
	err := filepath.WalkDir(s.root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("media: walk blob directory: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("media: inspect blob entry: %w", err)
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("media: calculate blob usage: %w", err)
	}
	return total, nil
}

func (s *BlobStore) Full(ctx context.Context) (bool, error) {
	if s == nil || s.maxBytes <= 0 {
		return false, nil
	}
	usage, err := s.Usage(ctx)
	if err != nil {
		return false, err
	}
	return usage >= s.maxBytes, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if errors.Is(err, io.EOF) || err == nil {
		return n, err //nolint:wrapcheck // io.Copy requires the original io.EOF value.
	}
	return n, fmt.Errorf("media: read blob source: %w", err)
}
