package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const FetchMaxAttempts = 3

// CDNRequest describes the only HTTP behavior used for an original download.
type CDNRequest struct {
	URL   string
	Range string
}

// CDNResponse is a streaming result from the CDN adapter.
type CDNResponse struct {
	StatusCode    int
	ContentLength int64
	ContentRange  string
	Body          io.ReadCloser
}

// CDNClient performs streaming CDN requests without exposing HTTP adapter
// types to the application layer.
type CDNClient interface {
	Get(context.Context, CDNRequest) (*CDNResponse, error)
}

// SizeRefresher asks the crawl engine to refresh the recorded size URL once
// after the CDN reports that a recorded URL is no longer available.
type SizeRefresher func(context.Context, string) (url string, expectedBytes int64, err error)

// FetchLimits protect a CDN host from excessive parallel downloads and
// bandwidth use. A zero value leaves that limit unlimited.
type FetchLimits struct {
	PerHostConcurrency int
	BytesPerSecond     int64
}

// Fetcher streams CDN files into temporary blob storage and verifies their
// size and SHA-256 checksum. It makes at most three HTTP attempts per fetch.
type Fetcher struct {
	Blobs   BlobStore
	HTTP    CDNClient
	Limits  FetchLimits
	Refresh SizeRefresher
	Wait    func(context.Context, time.Duration) error

	mu    sync.Mutex
	hosts map[string]*hostLimit
	keys  map[string]*keyLimit
}

type keyLimit struct {
	semaphore chan struct{}
	refs      int
}

type hostLimit struct {
	semaphore chan struct{}
	mu        sync.Mutex
	next      time.Time
}

// Fetch downloads url, optionally checking an expected byte count. The blob
// key is stable for the requested URL and the checksum is lowercase hex SHA-256.
//
//nolint:cyclop // This coordinates fetch validation, retries, URL refresh, and host limiter changes.
func (f *Fetcher) Fetch(ctx context.Context, source string, expected ...int64) (blobKey string, checksum string, bytes int64, err error) {
	if f == nil || ctx == nil || f.Blobs == nil || f.HTTP == nil || strings.TrimSpace(source) == "" || len(expected) > 1 || (len(expected) == 1 && expected[0] < 0) {
		return "", "", 0, errors.New("media: invalid fetch request")
	}
	parsed, err := url.Parse(source)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", "", 0, errors.New("media: fetch URL must be absolute http or https")
	}
	want := int64(-1)
	if len(expected) == 1 {
		want = expected[0]
	}
	urlHash := sha256.Sum256([]byte(source))
	blobKey = fmt.Sprintf("originals/%x", urlHash)
	releaseKey, err := f.acquireKey(ctx, blobKey)
	if err != nil {
		return "", "", 0, err
	}
	defer releaseKey()
	limit := f.hostLimit(strings.ToLower(parsed.Host))
	if err := f.acquire(ctx, limit); err != nil {
		return "", "", 0, err
	}
	hostAcquired := true
	defer func() {
		if hostAcquired {
			f.release(limit)
		}
	}()

	refreshed := false
	var lastErr error
	for attempt := 0; attempt < FetchMaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", "", 0, err
		}
		currentSize, statErr := f.partialSize(ctx, blobKey)
		if statErr != nil {
			return "", "", 0, statErr
		}
		request := CDNRequest{URL: source}
		if currentSize > 0 {
			request.Range = fmt.Sprintf("bytes=%d-", currentSize)
		}
		response, callErr := f.HTTP.Get(ctx, request)
		if callErr != nil {
			lastErr = callErr
			continue
		}
		if response == nil {
			lastErr = errors.New("media: CDN returned no response")
			continue
		}
		if response.Body == nil {
			lastErr = errors.New("media: CDN returned an empty response body")
			continue
		}
		if response.StatusCode == 416 {
			if closeErr := closeResponse(response); closeErr != nil {
				return "", "", 0, fmt.Errorf("media: close unsatisfiable range response: %w", closeErr)
			}
			if currentSize > 0 {
				if err := f.Blobs.Delete(ctx, blobKey); err != nil {
					return "", "", 0, fmt.Errorf("media: discard unsatisfiable partial blob: %w", err)
				}
				lastErr = errors.New("media: CDN rejected resume range; restarting download")
				continue
			}
			lastErr = errors.New("media: CDN rejected empty range request")
			continue
		}
		if (response.StatusCode == 403 || response.StatusCode == 404) && !refreshed && f.Refresh != nil {
			if closeErr := closeResponse(response); closeErr != nil {
				return "", "", 0, fmt.Errorf("media: close unavailable CDN response: %w", closeErr)
			}
			newURL, newExpected, refreshErr := f.Refresh(ctx, source)
			if refreshErr != nil {
				return "", "", 0, fmt.Errorf("media: refresh CDN size URL: %w", refreshErr)
			}
			if strings.TrimSpace(newURL) == "" || newExpected < 0 {
				return "", "", 0, errors.New("media: size refresh returned invalid URL or size")
			}
			if currentSize > 0 {
				if err := f.Blobs.Delete(ctx, blobKey); err != nil {
					return "", "", 0, fmt.Errorf("media: discard stale partial blob: %w", err)
				}
			}
			source, want, refreshed = newURL, newExpected, true
			parsed, err = url.Parse(source)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return "", "", 0, errors.New("media: refreshed URL must be absolute http or https")
			}
			// The original size URL identifies the stored temporary blob. Keep
			// host protection accurate if the refreshed CDN URL has another host.
			newLimit := f.hostLimit(strings.ToLower(parsed.Host))
			if newLimit != limit {
				f.release(limit)
				hostAcquired = false
				limit = newLimit
				if err := f.acquire(ctx, limit); err != nil {
					return "", "", 0, err
				}
				hostAcquired = true
			}
			// Refreshing the URL is a separate operation from downloading it.
			// Preserve all three download attempts for the refreshed URL.
			attempt--
			continue
		}
		if response.StatusCode != 200 && response.StatusCode != 206 {
			if closeErr := closeResponse(response); closeErr != nil {
				lastErr = errors.Join(fmt.Errorf("media: CDN returned HTTP %d", response.StatusCode), closeErr)
			} else {
				lastErr = fmt.Errorf("media: CDN returned HTTP %d", response.StatusCode)
			}
			continue
		}
		if response.StatusCode == 206 {
			start, ok := rangeStart(response.ContentRange)
			if !ok || start != currentSize {
				if closeErr := closeResponse(response); closeErr != nil {
					lastErr = errors.Join(errors.New("media: CDN returned an invalid range response"), closeErr)
				} else {
					lastErr = errors.New("media: CDN returned an invalid range response")
				}
				if err := f.Blobs.Delete(ctx, blobKey); err != nil {
					return "", "", 0, fmt.Errorf("media: discard invalid partial blob: %w", err)
				}
				continue
			}
		}
		if response.StatusCode == 200 {
			currentSize = 0 // Server ignored Range; this response starts over.
		}
		body := &limitedBody{ctx: ctx, body: response.Body, host: limit, rate: f.Limits.BytesPerSecond, wait: f.wait}
		copyErr := f.storeAttempt(ctx, blobKey, currentSize, body)
		if closeErr := closeResponse(response); closeErr != nil {
			return "", "", 0, fmt.Errorf("media: close CDN response: %w", closeErr)
		}
		if copyErr != nil && !errors.Is(copyErr, errInterruptedBody) {
			return "", "", 0, fmt.Errorf("media: store CDN response: %w", copyErr)
		}
		info, statErr := f.Blobs.Stat(ctx, blobKey)
		if statErr != nil {
			return "", "", 0, statErr
		}
		expectedResponse := response.ContentLength
		if copyErr == nil && expectedResponse >= 0 && info.Size-currentSize != expectedResponse {
			lastErr = fmt.Errorf("media: Content-Length mismatch: received %d bytes, expected %d", info.Size-currentSize, expectedResponse)
			if err := f.Blobs.Delete(ctx, blobKey); err != nil {
				return "", "", 0, fmt.Errorf("media: discard mismatched blob: %w", err)
			}
			continue
		}
		if copyErr == nil && want >= 0 && info.Size != want {
			lastErr = fmt.Errorf("media: expected %d bytes, received %d", want, info.Size)
			if err := f.Blobs.Delete(ctx, blobKey); err != nil {
				return "", "", 0, fmt.Errorf("media: discard unexpected-size blob: %w", err)
			}
			continue
		}
		if copyErr == nil {
			digest, hashErr := f.hash(ctx, blobKey)
			if hashErr != nil {
				return "", "", 0, hashErr
			}
			return blobKey, digest, info.Size, nil
		}
		lastErr = copyErr
	}
	if lastErr == nil {
		lastErr = errors.New("media: CDN fetch failed")
	}
	return "", "", 0, fmt.Errorf("media: fetch failed after %d attempts: %w", FetchMaxAttempts, lastErr)
}

func (f *Fetcher) acquireKey(ctx context.Context, key string) (func(), error) {
	f.mu.Lock()
	if f.keys == nil {
		f.keys = make(map[string]*keyLimit)
	}
	limit := f.keys[key]
	if limit == nil {
		limit = &keyLimit{semaphore: make(chan struct{}, 1)}
		f.keys[key] = limit
	}
	limit.refs++
	f.mu.Unlock()
	select {
	case limit.semaphore <- struct{}{}:
		return func() {
			<-limit.semaphore
			f.mu.Lock()
			limit.refs--
			if limit.refs == 0 {
				delete(f.keys, key)
			}
			f.mu.Unlock()
		}, nil
	case <-ctx.Done():
		f.mu.Lock()
		limit.refs--
		if limit.refs == 0 {
			delete(f.keys, key)
		}
		f.mu.Unlock()
		return nil, ctx.Err()
	}
}

func closeResponse(response *CDNResponse) error {
	if response == nil || response.Body == nil {
		return nil
	}
	if err := response.Body.Close(); err != nil {
		return fmt.Errorf("media: close CDN response body: %w", err)
	}
	return nil
}

var errInterruptedBody = errors.New("media: interrupted CDN response body")

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

func rangeStart(value string) (int64, bool) {
	if !strings.HasPrefix(value, "bytes ") {
		return 0, false
	}
	span, _, ok := strings.Cut(strings.TrimPrefix(value, "bytes "), "/")
	if !ok {
		return 0, false
	}
	start, _, ok := strings.Cut(span, "-")
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(start, 10, 64)
	return n, err == nil
}

func (f *Fetcher) hostLimit(host string) *hostLimit {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hosts == nil {
		f.hosts = make(map[string]*hostLimit)
	}
	limit := f.hosts[host]
	if limit == nil {
		limit = &hostLimit{}
		if f.Limits.PerHostConcurrency > 0 {
			limit.semaphore = make(chan struct{}, f.Limits.PerHostConcurrency)
		}
		f.hosts[host] = limit
	}
	return limit
}

func (f *Fetcher) acquire(ctx context.Context, limit *hostLimit) error {
	if limit.semaphore == nil {
		return nil
	}
	select {
	case limit.semaphore <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("media: wait for CDN host: %w", ctx.Err())
	}
}

func (f *Fetcher) release(limit *hostLimit) {
	if limit != nil && limit.semaphore != nil {
		<-limit.semaphore
	}
}

func (f *Fetcher) wait(ctx context.Context, duration time.Duration) error {
	if f.Wait != nil {
		return f.Wait(ctx, duration)
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type limitedBody struct {
	ctx  context.Context
	body io.Reader
	host *hostLimit
	rate int64
	wait func(context.Context, time.Duration) error
}

func (r *limitedBody) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.body.Read(p)
	if n > 0 && r.rate > 0 {
		delay := time.Duration(float64(time.Second) * float64(n) / float64(r.rate))
		r.host.mu.Lock()
		now := time.Now()
		start := now
		if r.host.next.After(start) {
			start = r.host.next
		}
		r.host.next = start.Add(delay)
		waitFor := start.Sub(now) + delay
		r.host.mu.Unlock()
		if waitFor > 0 {
			if waitErr := r.wait(r.ctx, waitFor); waitErr != nil {
				return n, fmt.Errorf("media: bandwidth wait: %w", waitErr)
			}
		}
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return n, fmt.Errorf("media: CDN body read: %w", err)
	}
	if err != nil {
		return n, io.EOF
	}
	return n, nil
}
