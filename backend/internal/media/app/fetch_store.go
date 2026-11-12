package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

var errInterruptedBody = errors.New("media: interrupted CDN response body")

// store streams the response body into the blob, continuing after the partial
// bytes of this attempt. An interrupted body keeps its bytes for a resume.
func (f *Fetcher) store(ctx context.Context, job *fetchJob, response *CDNResponse) (verdict, error) {
	body := &limitedBody{ctx: ctx, body: response.Body, host: job.limit, rate: f.Limits.BytesPerSecond, wait: f.wait}
	copyErr := f.storeAttempt(ctx, job.blobKey, job.offset, body)
	if closeErr := closeResponse(response); closeErr != nil {
		return verdictFailed, fmt.Errorf("media: close CDN response: %w", closeErr)
	}
	if copyErr != nil && !errors.Is(copyErr, errInterruptedBody) {
		return verdictFailed, fmt.Errorf("media: store CDN response: %w", copyErr)
	}
	info, err := f.Blobs.Stat(ctx, job.blobKey)
	if err != nil {
		return verdictFailed, err
	}
	if copyErr != nil {
		return verdictRetry, copyErr
	}
	return f.verify(ctx, job, response, info.Size)
}

// verify checks the stored size against the response and the expected size,
// then computes the checksum of the complete blob.
func (f *Fetcher) verify(ctx context.Context, job *fetchJob, response *CDNResponse, stored int64) (verdict, error) {
	received := stored - job.offset
	if response.ContentLength >= 0 && received != response.ContentLength {
		return f.discard(ctx, job.blobKey, "discard mismatched blob",
			fmt.Errorf("media: Content-Length mismatch: received %d bytes, expected %d", received, response.ContentLength))
	}
	if job.want >= 0 && stored != job.want {
		return f.discard(ctx, job.blobKey, "discard unexpected-size blob",
			fmt.Errorf("media: expected %d bytes, received %d", job.want, stored))
	}
	digest, err := f.hash(ctx, job.blobKey)
	if err != nil {
		return verdictFailed, err
	}
	job.checksum, job.bytes = digest, stored
	return verdictStored, nil
}

// discard deletes a rejected blob. A failed delete is fatal; otherwise reason
// is the failure of the attempt that is retried.
func (f *Fetcher) discard(ctx context.Context, key, label string, reason error) (verdict, error) {
	if err := f.Blobs.Delete(ctx, key); err != nil {
		return verdictFailed, fmt.Errorf("media: %s: %w", label, err)
	}
	return verdictRetry, reason
}

// storeAttempt writes an existing partial blob and the next response as one
// stream. Network read failures are converted to EOF so the partial bytes are
// committed atomically and can be resumed on the next attempt.
func (f *Fetcher) storeAttempt(ctx context.Context, key string, offset int64, body io.Reader) error {
	var prefix io.Reader
	if offset > 0 {
		partial, err := f.Blobs.Get(ctx, key)
		if err != nil {
			return fmt.Errorf("read partial blob: %w", err)
		}
		defer partial.Close()
		prefix = partial
	}
	response := &resumableReader{reader: body}
	var combined io.Reader = response
	if prefix != nil {
		combined = io.MultiReader(prefix, response)
	}
	if err := f.Blobs.Put(ctx, key, combined); err != nil {
		return err
	}
	return response.err
}

type resumableReader struct {
	reader io.Reader
	err    error
}

func (r *resumableReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		r.err = errors.Join(errInterruptedBody, err)
		return n, io.EOF
	}
	if err != nil {
		return n, io.EOF
	}
	return n, nil
}

func (f *Fetcher) partialSize(ctx context.Context, key string) (int64, error) {
	info, err := f.Blobs.Stat(ctx, key)
	if errors.Is(err, ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("media: inspect partial blob: %w", err)
	}
	return info.Size, nil
}

func (f *Fetcher) hash(ctx context.Context, key string) (string, error) {
	blob, err := f.Blobs.Get(ctx, key)
	if err != nil {
		return "", fmt.Errorf("media: read stored blob for checksum: %w", err)
	}
	defer blob.Close()
	h := sha256.New()
	if _, err := io.Copy(h, &fetchContextReader{ctx: ctx, reader: blob}); err != nil {
		return "", fmt.Errorf("media: checksum blob: %w", err)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

type fetchContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *fetchContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, fmt.Errorf("media: checksum context: %w", err)
	}
	n, err := r.reader.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, fmt.Errorf("media: checksum read: %w", err)
	}
	if err != nil {
		return n, io.EOF
	}
	return n, nil
}
