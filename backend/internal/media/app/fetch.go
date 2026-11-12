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

// Fetch downloads url, optionally checking an expected byte count. The blob
// key is stable for the requested URL and the checksum is lowercase hex SHA-256.
func (f *Fetcher) Fetch(ctx context.Context, source string, expected ...int64) (string, string, int64, error) {
	job, err := f.newFetchJob(ctx, source, expected)
	if err != nil {
		return "", "", 0, err
	}
	releaseKey, err := f.acquireKey(ctx, job.blobKey)
	if err != nil {
		return "", "", 0, err
	}
	defer releaseKey()
	if err := f.swapHost(ctx, job, f.hostLimit(job.host)); err != nil {
		return "", "", 0, err
	}
	defer f.releaseJobHost(job)
	return f.attemptLoop(ctx, job)
}

// fetchJob is the state one Fetch call carries across its attempts.
type fetchJob struct {
	source    string     // current URL; replaced once when the CDN refreshes it
	host      string     // lower-case host of the requested URL
	want      int64      // expected byte count, or -1 when unchecked
	blobKey   string     // stable blob key of the requested URL
	refreshed bool       // the size URL has already been refreshed
	limit     *hostLimit // host slot for the current URL
	hostHeld  bool
	offset    int64  // bytes already stored when the attempt started
	checksum  string // set once the blob is verified
	bytes     int64
	lastErr   error // reason the latest counted attempt failed
}

// verdict is the outcome of one step of a fetch attempt.
type verdict int

const (
	// verdictRetry ends a counted attempt that failed; the error says why.
	verdictRetry verdict = iota
	// verdictRestart means the URL was refreshed; the attempt is not counted.
	verdictRestart
	// verdictStored means the blob was stored and verified.
	verdictStored
	// verdictFailed stops the fetch and returns the error.
	verdictFailed
)

// newFetchJob validates the request and derives the blob key and host.
func (f *Fetcher) newFetchJob(ctx context.Context, source string, expected []int64) (*fetchJob, error) {
	if !f.requestValid(ctx, source, expected) {
		return nil, errors.New("media: invalid fetch request")
	}
	parsed, ok := absoluteHTTPURL(source)
	if !ok {
		return nil, errors.New("media: fetch URL must be absolute http or https")
	}
	job := &fetchJob{source: source, host: strings.ToLower(parsed.Host), want: -1, blobKey: originalKey(source)}
	if len(expected) == 1 {
		job.want = expected[0]
	}
	return job, nil
}

// requestValid reports whether the fetcher is wired and the arguments are usable.
func (f *Fetcher) requestValid(ctx context.Context, source string, expected []int64) bool {
	if f == nil || ctx == nil || f.Blobs == nil || f.HTTP == nil || strings.TrimSpace(source) == "" {
		return false
	}
	return len(expected) == 0 || (len(expected) == 1 && expected[0] >= 0)
}

// absoluteHTTPURL parses raw and reports whether it is an absolute http or
// https URL with a host.
func absoluteHTTPURL(raw string) (*url.URL, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, false
	}
	return parsed, true
}

// originalKey is the blob key of an original download. It depends only on the
// requested URL, so a refreshed URL keeps the same partial blob.
func originalKey(source string) string {
	return fmt.Sprintf("originals/%x", sha256.Sum256([]byte(source)))
}

// attemptLoop makes attempts until one stores a verified blob. A URL refresh
// does not count as an attempt, so the refreshed URL gets all of them.
func (f *Fetcher) attemptLoop(ctx context.Context, job *fetchJob) (string, string, int64, error) {
	for attempts := 0; attempts < FetchMaxAttempts; {
		result, err := f.attempt(ctx, job)
		switch result {
		case verdictStored:
			return job.blobKey, job.checksum, job.bytes, nil
		case verdictFailed:
			return "", "", 0, err
		case verdictRestart:
			continue
		}
		job.lastErr = err
		attempts++
	}
	return "", "", 0, fmt.Errorf("media: fetch failed after %d attempts: %w", FetchMaxAttempts, job.lastErr)
}

// attempt sends one request and hands the response to answer.
func (f *Fetcher) attempt(ctx context.Context, job *fetchJob) (verdict, error) {
	if err := ctx.Err(); err != nil {
		return verdictFailed, err
	}
	size, err := f.partialSize(ctx, job.blobKey)
	if err != nil {
		return verdictFailed, err
	}
	job.offset = size
	request := CDNRequest{URL: job.source}
	if size > 0 {
		request.Range = fmt.Sprintf("bytes=%d-", size)
	}
	response, err := f.HTTP.Get(ctx, request)
	if err != nil {
		return verdictRetry, err
	}
	if response == nil {
		return verdictRetry, errors.New("media: CDN returned no response")
	}
	if response.Body == nil {
		return verdictRetry, errors.New("media: CDN returned an empty response body")
	}
	return f.answer(ctx, job, response)
}

// answer decides what a CDN response means for the attempt.
func (f *Fetcher) answer(ctx context.Context, job *fetchJob, response *CDNResponse) (verdict, error) {
	status := response.StatusCode
	switch {
	case status == 416:
		return f.unsatisfiableRange(ctx, job, response)
	case (status == 403 || status == 404) && !job.refreshed && f.Refresh != nil:
		return f.refreshURL(ctx, job, response)
	case status == 206:
		return f.partialRange(ctx, job, response)
	case status == 200:
		job.offset = 0 // The server ignored Range; this response starts over.
		return f.store(ctx, job, response)
	default:
		return verdictRetry, withCloseError(response, fmt.Errorf("media: CDN returned HTTP %d", status))
	}
}

// unsatisfiableRange handles a 416. A rejected resume range discards the
// partial blob so the next attempt starts from byte zero.
func (f *Fetcher) unsatisfiableRange(ctx context.Context, job *fetchJob, response *CDNResponse) (verdict, error) {
	if closeErr := closeResponse(response); closeErr != nil {
		return verdictFailed, fmt.Errorf("media: close unsatisfiable range response: %w", closeErr)
	}
	if job.offset > 0 {
		return f.discard(ctx, job.blobKey, "discard unsatisfiable partial blob", errors.New("media: CDN rejected resume range; restarting download"))
	}
	return verdictRetry, errors.New("media: CDN rejected empty range request")
}

// partialRange accepts a 206 only when it continues exactly where the partial
// blob ends.
func (f *Fetcher) partialRange(ctx context.Context, job *fetchJob, response *CDNResponse) (verdict, error) {
	if start, ok := rangeStart(response.ContentRange); ok && start == job.offset {
		return f.store(ctx, job, response)
	}
	reason := withCloseError(response, errors.New("media: CDN returned an invalid range response"))
	return f.discard(ctx, job.blobKey, "discard invalid partial blob", reason)
}

// refreshURL asks the crawl engine for a fresh size URL, once, after the CDN
// reports the recorded URL unavailable.
func (f *Fetcher) refreshURL(ctx context.Context, job *fetchJob, response *CDNResponse) (verdict, error) {
	if closeErr := closeResponse(response); closeErr != nil {
		return verdictFailed, fmt.Errorf("media: close unavailable CDN response: %w", closeErr)
	}
	newURL, newExpected, err := f.Refresh(ctx, job.source)
	if err != nil {
		return verdictFailed, fmt.Errorf("media: refresh CDN size URL: %w", err)
	}
	if strings.TrimSpace(newURL) == "" || newExpected < 0 {
		return verdictFailed, errors.New("media: size refresh returned invalid URL or size")
	}
	if job.offset > 0 {
		if err := f.Blobs.Delete(ctx, job.blobKey); err != nil {
			return verdictFailed, fmt.Errorf("media: discard stale partial blob: %w", err)
		}
	}
	job.source, job.want, job.refreshed = newURL, newExpected, true
	return f.adoptURL(ctx, job)
}

// adoptURL checks the refreshed URL and moves the host slot to its host.
func (f *Fetcher) adoptURL(ctx context.Context, job *fetchJob) (verdict, error) {
	parsed, ok := absoluteHTTPURL(job.source)
	if !ok {
		return verdictFailed, errors.New("media: refreshed URL must be absolute http or https")
	}
	if err := f.moveHost(ctx, job, strings.ToLower(parsed.Host)); err != nil {
		return verdictFailed, err
	}
	return verdictRestart, nil
}

// moveHost switches to the host slot of a refreshed URL unless it is the one
// already held.
func (f *Fetcher) moveHost(ctx context.Context, job *fetchJob, host string) error {
	next := f.hostLimit(host)
	if next == job.limit {
		return nil
	}
	return f.swapHost(ctx, job, next)
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

// withCloseError closes the response and joins any close failure to reason.
func withCloseError(response *CDNResponse, reason error) error {
	if closeErr := closeResponse(response); closeErr != nil {
		return errors.Join(reason, closeErr)
	}
	return reason
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
