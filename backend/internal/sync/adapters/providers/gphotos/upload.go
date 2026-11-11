// Package gphotos implements the Google Photos raw upload step.
package gphotos

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/jooservices/xflickr/internal/sync/app"
)

const maxUploadTokenBytes = 4 * 1024

// Client uploads verified asset bytes and returns Google's temporary upload token.
type Client struct {
	client    *http.Client
	uploadURL string
}

// NewClient creates a raw upload client using the caller's configured HTTP client.
// The caller supplies authentication and must disable redirects on the client.
func NewClient(client *http.Client, uploadURL string) (*Client, error) {
	if client == nil {
		return nil, errors.New("gphotos: HTTP client is required")
	}
	u, err := url.ParseRequestURI(uploadURL)
	if err != nil || !u.IsAbs() || u.Scheme == "" || u.Hostname() == "" {
		return nil, errors.New("gphotos: upload URL must be an absolute URL with a host")
	}
	return &Client{client: client, uploadURL: uploadURL}, nil
}

// UploadBytes sends one raw upload request and returns its temporary upload token.
func (c *Client) UploadBytes(ctx context.Context, asset app.Asset) (string, error) {
	if c == nil || c.client == nil {
		return "", errors.New("gphotos: HTTP client is required")
	}
	if ctx == nil {
		return "", errors.New("gphotos: context is required")
	}
	if err := asset.Validate(); err != nil {
		return "", fmt.Errorf("gphotos: validate asset: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("gphotos: upload request canceled: %w", err)
	}

	stream := &countingReader{reader: asset.Content, digest: sha256.New(), done: make(chan struct{})}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.uploadURL, stream)
	if err != nil {
		return "", errors.New("gphotos: create upload request")
	}
	request.ContentLength = asset.Size
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("X-Goog-Upload-Content-Type", asset.ContentType)
	request.Header.Set("X-Goog-Upload-Protocol", "raw")

	response, err := c.client.Do(request)
	if err != nil {
		_ = stream.Close()
		if contextErr := ctx.Err(); contextErr != nil {
			return "", fmt.Errorf("gphotos: upload request canceled: %w", contextErr)
		}
		if stream.isComplete() {
			count, _, reachedEOF := stream.snapshot()
			if reachedEOF && count != asset.Size {
				return "", fmt.Errorf("%w: uploaded byte count does not match metadata", app.ErrInvalidAsset)
			}
		}
		return "", errors.New("gphotos: send upload request")
	}
	if response == nil || response.Body == nil {
		_ = stream.Close()
		return "", errors.New("gphotos: upload response has no body")
	}
	defer func() { _ = response.Body.Close() }()
	if !stream.wait(ctx) {
		return "", fmt.Errorf("gphotos: upload request canceled: %w", ctx.Err())
	}

	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", fmt.Errorf("%w: Google Photos rejected the credentials", app.ErrAuthentication)
	case http.StatusTooManyRequests:
		return "", fmt.Errorf("%w: Google Photos rate limited the upload", app.ErrRateLimited)
	default:
		return "", fmt.Errorf("gphotos: upload request failed with HTTP status %d", response.StatusCode)
	}

	count, actualDigest, _ := stream.snapshot()
	if count != asset.Size {
		return "", fmt.Errorf("%w: uploaded byte count does not match metadata", app.ErrInvalidAsset)
	}
	if !strings.EqualFold(actualDigest, asset.SHA256) {
		return "", fmt.Errorf("%w: uploaded SHA-256 does not match metadata", app.ErrInvalidAsset)
	}

	token, err := readUploadToken(response.Body, ctx)
	if err != nil {
		return "", err
	}
	return token, nil
}

func readUploadToken(body io.Reader, ctx context.Context) (string, error) {
	content, err := io.ReadAll(io.LimitReader(body, maxUploadTokenBytes+1))
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return "", fmt.Errorf("gphotos: read upload token canceled: %w", contextErr)
		}
		return "", errors.New("gphotos: read upload token")
	}
	if len(content) > maxUploadTokenBytes {
		return "", errors.New("gphotos: upload token exceeds 4 KiB")
	}
	token := strings.TrimSpace(string(content))
	if token == "" {
		return "", errors.New("gphotos: upload response contains an empty token")
	}
	return token, nil
}

type countingReader struct {
	reader      io.Reader
	digest      hash.Hash
	done        chan struct{}
	doneOnce    sync.Once
	mu          sync.Mutex
	count       int64
	activeReads int
	reachedEOF  bool
	closed      bool
}

func (r *countingReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	r.activeReads++
	r.mu.Unlock()

	n, err := r.reader.Read(p)
	r.mu.Lock()
	r.count += int64(n)
	if n > 0 {
		_, _ = r.digest.Write(p[:n])
	}
	if errors.Is(err, io.EOF) {
		r.reachedEOF = true
	}
	r.activeReads--
	finished := r.activeReads == 0 && (r.closed || r.reachedEOF)
	r.mu.Unlock()
	if finished {
		r.finish()
	}
	return n, err
}

func (r *countingReader) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	activeReads := r.activeReads
	closer, canClose := r.reader.(io.Closer)
	r.mu.Unlock()

	var err error
	if canClose {
		err = closer.Close()
	}
	if activeReads == 0 {
		r.finish()
	}
	return err
}

func (r *countingReader) wait(ctx context.Context) bool {
	select {
	case <-r.done:
		return true
	case <-ctx.Done():
		_ = r.Close()
		return false
	}
}

func (r *countingReader) snapshot() (int64, string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count, hex.EncodeToString(r.digest.Sum(nil)), r.reachedEOF
}

func (r *countingReader) isComplete() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

func (r *countingReader) finish() {
	r.doneOnce.Do(func() { close(r.done) })
}
