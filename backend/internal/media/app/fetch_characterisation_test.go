package app_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/media/adapters/local"
	"github.com/jooservices/xflickr/internal/media/app"
)

// charCDN scripts one CDN response per call.
type charCDN func(context.Context, app.CDNRequest) (*app.CDNResponse, error)

func (c charCDN) Get(ctx context.Context, request app.CDNRequest) (*app.CDNResponse, error) {
	return c(ctx, request)
}

// faultBlobs wraps a real blob store and injects one failure per operation.
type faultBlobs struct {
	app.BlobStore
	putErr     error
	getErr     error
	getBody    func() io.ReadCloser
	deleteErr  error
	statFailAt int
	stats      int
}

func (f *faultBlobs) Put(ctx context.Context, key string, src io.Reader) error {
	if f.putErr != nil {
		return f.putErr
	}
	return f.BlobStore.Put(ctx, key, src)
}

func (f *faultBlobs) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.getBody != nil {
		return f.getBody(), nil
	}
	return f.BlobStore.Get(ctx, key)
}

func (f *faultBlobs) Delete(ctx context.Context, key string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	return f.BlobStore.Delete(ctx, key)
}

func (f *faultBlobs) Stat(ctx context.Context, key string) (app.BlobInfo, error) {
	f.stats++
	if f.stats == f.statFailAt {
		return app.BlobInfo{}, errors.New("storage unavailable")
	}
	return f.BlobStore.Stat(ctx, key)
}

// charCloseErr is a body whose Close fails after the content is read.
type charCloseErr struct{ io.Reader }

func (charCloseErr) Close() error { return errors.New("connection close failed") }

// charErrReader fails on every read.
type charErrReader struct{ err error }

func (r charErrReader) Read([]byte) (int, error) { return 0, r.err }

// charCancelReader cancels the fetch context on its first read and then
// yields one byte, so the next checksum read observes the cancellation.
type charCancelReader struct{ cancel context.CancelFunc }

func (r charCancelReader) Read(p []byte) (int, error) {
	r.cancel()
	p[0] = 'x'
	return 1, nil
}

func charStore(t *testing.T) app.BlobStore {
	t.Helper()
	store, err := local.New(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func charFetcher(t *testing.T, do charCDN) *app.Fetcher {
	t.Helper()
	return &app.Fetcher{Blobs: charStore(t), HTTP: do}
}

func charSeed(t *testing.T, store app.BlobStore, source, content string) {
	t.Helper()
	key := fmt.Sprintf("originals/%x", sha256.Sum256([]byte(source)))
	if err := store.Put(context.Background(), key, strings.NewReader(content)); err != nil {
		t.Fatal(err)
	}
}

func charStatus(code int, body io.ReadCloser) *app.CDNResponse {
	return &app.CDNResponse{StatusCode: code, ContentLength: -1, Body: body}
}

func charBody(content string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(content))
}

func charOK(content string) *app.CDNResponse {
	return &app.CDNResponse{StatusCode: http.StatusOK, ContentLength: int64(len(content)), Body: charBody(content)}
}

func charExpectErr(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("Fetch() succeeded")
	}
	for _, part := range parts {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("Fetch() error = %q, want it to contain %q", err, part)
		}
	}
}

const charSource = "https://cdn.test/original"

func TestFetchStopsBetweenAttemptsWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	fetcher := charFetcher(t, func(context.Context, app.CDNRequest) (*app.CDNResponse, error) {
		calls++
		cancel()
		return charStatus(http.StatusInternalServerError, charBody("")), nil
	})
	if _, _, _, err := fetcher.Fetch(ctx, charSource); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("Fetch() calls=%d, err=%v", calls, err)
	}
}

func TestFetchReportsCloseFailureAfterUnsatisfiableRange(t *testing.T) {
	calls := 0
	fetcher := charFetcher(t, func(context.Context, app.CDNRequest) (*app.CDNResponse, error) {
		calls++
		return charStatus(http.StatusRequestedRangeNotSatisfiable, charCloseErr{strings.NewReader("")}), nil
	})
	_, _, _, err := fetcher.Fetch(context.Background(), charSource)
	charExpectErr(t, err, "close unsatisfiable range response", "connection close failed")
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestFetchKeepsPartialBlobWhenUnsatisfiableRangeDiscardFails(t *testing.T) {
	store := charStore(t)
	charSeed(t, store, charSource, "abc")
	fetcher := &app.Fetcher{
		Blobs: &faultBlobs{BlobStore: store, deleteErr: errors.New("disk busy")},
		HTTP: charCDN(func(_ context.Context, request app.CDNRequest) (*app.CDNResponse, error) {
			if request.Range != "bytes=3-" {
				t.Errorf("Range = %q", request.Range)
			}
			return charStatus(http.StatusRequestedRangeNotSatisfiable, charBody("")), nil
		}),
	}
	_, _, _, err := fetcher.Fetch(context.Background(), charSource)
	charExpectErr(t, err, "discard unsatisfiable partial blob", "disk busy")
}

func TestFetchRetriesEmptyRangeRejectionUntilAttemptsRunOut(t *testing.T) {
	calls := 0
	fetcher := charFetcher(t, func(context.Context, app.CDNRequest) (*app.CDNResponse, error) {
		calls++
		return charStatus(http.StatusRequestedRangeNotSatisfiable, charBody("")), nil
	})
	_, _, _, err := fetcher.Fetch(context.Background(), charSource)
	charExpectErr(t, err, "3 attempts", "CDN rejected empty range request")
	if calls != app.FetchMaxAttempts {
		t.Fatalf("calls = %d, want %d", calls, app.FetchMaxAttempts)
	}
}

func TestFetchReportsCloseFailureBeforeRefresh(t *testing.T) {
	fetcher := charFetcher(t, func(context.Context, app.CDNRequest) (*app.CDNResponse, error) {
		return charStatus(http.StatusForbidden, charCloseErr{strings.NewReader("")}), nil
	})
	fetcher.Refresh = func(context.Context, string) (string, int64, error) {
		t.Error("refresh ran after the unavailable response failed to close")
		return "", 0, nil
	}
	_, _, _, err := fetcher.Fetch(context.Background(), charSource)
	charExpectErr(t, err, "close unavailable CDN response", "connection close failed")
}

func TestFetchKeepsPartialBlobWhenStaleDiscardFailsBeforeRefresh(t *testing.T) {
	store := charStore(t)
	charSeed(t, store, charSource, "abc")
	fetcher := &app.Fetcher{
		Blobs: &faultBlobs{BlobStore: store, deleteErr: errors.New("disk busy")},
		HTTP: charCDN(func(context.Context, app.CDNRequest) (*app.CDNResponse, error) {
			return charStatus(http.StatusNotFound, charBody("")), nil
		}),
		Refresh: func(context.Context, string) (string, int64, error) {
			return "https://cdn.test/fresh", 5, nil
		},
	}
	_, _, _, err := fetcher.Fetch(context.Background(), charSource)
	charExpectErr(t, err, "discard stale partial blob", "disk busy")
}

func TestFetchRetriesInvalidStatusAndReportsCloseFailure(t *testing.T) {
	calls := 0
	fetcher := charFetcher(t, func(context.Context, app.CDNRequest) (*app.CDNResponse, error) {
		calls++
		return charStatus(http.StatusInternalServerError, charCloseErr{strings.NewReader("")}), nil
	})
	_, _, _, err := fetcher.Fetch(context.Background(), charSource)
	charExpectErr(t, err, "CDN returned HTTP 500", "connection close failed")
	if calls != app.FetchMaxAttempts {
		t.Fatalf("calls = %d, want %d", calls, app.FetchMaxAttempts)
	}
}

func TestFetchRetriesInvalidRangeAndReportsCloseFailure(t *testing.T) {
	calls := 0
	fetcher := charFetcher(t, func(context.Context, app.CDNRequest) (*app.CDNResponse, error) {
		calls++
		return &app.CDNResponse{StatusCode: http.StatusPartialContent, ContentRange: "invalid", ContentLength: -1, Body: charCloseErr{strings.NewReader("")}}, nil
	})
	_, _, _, err := fetcher.Fetch(context.Background(), charSource)
	charExpectErr(t, err, "invalid range response", "connection close failed")
	if calls != app.FetchMaxAttempts {
		t.Fatalf("calls = %d, want %d", calls, app.FetchMaxAttempts)
	}
}

func TestFetchReportsInvalidRangeDiscardFailure(t *testing.T) {
	calls := 0
	store := charStore(t)
	fetcher := &app.Fetcher{
		Blobs: &faultBlobs{BlobStore: store, deleteErr: errors.New("disk busy")},
		HTTP: charCDN(func(context.Context, app.CDNRequest) (*app.CDNResponse, error) {
			calls++
			return &app.CDNResponse{StatusCode: http.StatusPartialContent, ContentRange: "invalid", ContentLength: -1, Body: charBody("x")}, nil
		}),
	}
	_, _, _, err := fetcher.Fetch(context.Background(), charSource)
	charExpectErr(t, err, "discard invalid partial blob", "disk busy")
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestFetchReportsCloseFailureAfterStoringBody(t *testing.T) {
	calls := 0
	fetcher := charFetcher(t, func(context.Context, app.CDNRequest) (*app.CDNResponse, error) {
		calls++
		return &app.CDNResponse{StatusCode: http.StatusOK, ContentLength: 2, Body: charCloseErr{strings.NewReader("ok")}}, nil
	})
	_, _, _, err := fetcher.Fetch(context.Background(), charSource)
	charExpectErr(t, err, "close CDN response", "connection close failed")
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestFetchStopsWhenStoringBodyFails(t *testing.T) {
	calls := 0
	fetcher := &app.Fetcher{
		Blobs: &faultBlobs{BlobStore: charStore(t), putErr: errors.New("disk full")},
		HTTP: charCDN(func(context.Context, app.CDNRequest) (*app.CDNResponse, error) {
			calls++
			return charOK("ok"), nil
		}),
	}
	_, _, _, err := fetcher.Fetch(context.Background(), charSource)
	charExpectErr(t, err, "store CDN response", "disk full")
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestFetchReportsStatFailureAfterStoringBody(t *testing.T) {
	calls := 0
	fetcher := &app.Fetcher{
		Blobs: &faultBlobs{BlobStore: charStore(t), statFailAt: 2},
		HTTP: charCDN(func(context.Context, app.CDNRequest) (*app.CDNResponse, error) {
			calls++
			return charOK("ok"), nil
		}),
	}
	_, _, _, err := fetcher.Fetch(context.Background(), charSource)
	charExpectErr(t, err, "storage unavailable")
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestFetchRetriesContentLengthMismatchWithoutStreamError(t *testing.T) {
	calls := 0
	fetcher := charFetcher(t, func(context.Context, app.CDNRequest) (*app.CDNResponse, error) {
		calls++
		return &app.CDNResponse{StatusCode: http.StatusOK, ContentLength: 10, Body: charBody("short")}, nil
	})
	_, _, _, err := fetcher.Fetch(context.Background(), charSource)
	charExpectErr(t, err, "Content-Length mismatch: received 5 bytes, expected 10")
	if calls != app.FetchMaxAttempts {
		t.Fatalf("calls = %d, want %d", calls, app.FetchMaxAttempts)
	}
}

func TestFetchStopsWhenMismatchedBlobCannotBeDiscarded(t *testing.T) {
	calls := 0
	fetcher := &app.Fetcher{
		Blobs: &faultBlobs{BlobStore: charStore(t), deleteErr: errors.New("disk busy")},
		HTTP: charCDN(func(context.Context, app.CDNRequest) (*app.CDNResponse, error) {
			calls++
			return &app.CDNResponse{StatusCode: http.StatusOK, ContentLength: 10, Body: charBody("short")}, nil
		}),
	}
	_, _, _, err := fetcher.Fetch(context.Background(), charSource)
	charExpectErr(t, err, "discard mismatched blob", "disk busy")
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestFetchRetriesUnexpectedSizeUntilAttemptsRunOut(t *testing.T) {
	calls := 0
	fetcher := charFetcher(t, func(context.Context, app.CDNRequest) (*app.CDNResponse, error) {
		calls++
		return charOK("short"), nil
	})
	_, _, _, err := fetcher.Fetch(context.Background(), charSource, 6)
	charExpectErr(t, err, "expected 6 bytes, received 5")
	if calls != app.FetchMaxAttempts {
		t.Fatalf("calls = %d, want %d", calls, app.FetchMaxAttempts)
	}
}

func TestFetchStopsWhenUnexpectedSizeBlobCannotBeDiscarded(t *testing.T) {
	calls := 0
	fetcher := &app.Fetcher{
		Blobs: &faultBlobs{BlobStore: charStore(t), deleteErr: errors.New("disk busy")},
		HTTP: charCDN(func(context.Context, app.CDNRequest) (*app.CDNResponse, error) {
			calls++
			return charOK("short"), nil
		}),
	}
	_, _, _, err := fetcher.Fetch(context.Background(), charSource, 6)
	charExpectErr(t, err, "discard unexpected-size blob", "disk busy")
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestFetchReportsChecksumReadFailure(t *testing.T) {
	fetcher := &app.Fetcher{
		Blobs: &faultBlobs{BlobStore: charStore(t), getErr: errors.New("disk offline")},
		HTTP: charCDN(func(context.Context, app.CDNRequest) (*app.CDNResponse, error) {
			return charOK("ok"), nil
		}),
	}
	_, _, _, err := fetcher.Fetch(context.Background(), charSource)
	charExpectErr(t, err, "read stored blob for checksum", "disk offline")
}

func TestFetchReportsChecksumStreamFailure(t *testing.T) {
	fetcher := &app.Fetcher{
		Blobs: &faultBlobs{BlobStore: charStore(t), getBody: func() io.ReadCloser {
			return io.NopCloser(charErrReader{err: errors.New("sector unreadable")})
		}},
		HTTP: charCDN(func(context.Context, app.CDNRequest) (*app.CDNResponse, error) {
			return charOK("ok"), nil
		}),
	}
	_, _, _, err := fetcher.Fetch(context.Background(), charSource)
	charExpectErr(t, err, "checksum blob", "sector unreadable")
}

func TestFetchStopsChecksumWhenContextIsCanceledMidRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fetcher := &app.Fetcher{
		Blobs: &faultBlobs{BlobStore: charStore(t), getBody: func() io.ReadCloser {
			return io.NopCloser(charCancelReader{cancel: cancel})
		}},
		HTTP: charCDN(func(context.Context, app.CDNRequest) (*app.CDNResponse, error) {
			return charOK("x"), nil
		}),
	}
	_, _, _, err := fetcher.Fetch(ctx, charSource)
	charExpectErr(t, err, "checksum context", "context canceled")
}

func TestFetchReportsPartialBlobReadFailureBeforeResume(t *testing.T) {
	store := charStore(t)
	charSeed(t, store, charSource, "abc")
	calls := 0
	fetcher := &app.Fetcher{
		Blobs: &faultBlobs{BlobStore: store, getErr: errors.New("disk offline")},
		HTTP: charCDN(func(_ context.Context, request app.CDNRequest) (*app.CDNResponse, error) {
			calls++
			if request.Range != "bytes=3-" {
				t.Errorf("Range = %q", request.Range)
			}
			return &app.CDNResponse{StatusCode: http.StatusPartialContent, ContentRange: "bytes 3-4/5", ContentLength: 2, Body: charBody("de")}, nil
		}),
	}
	_, _, _, err := fetcher.Fetch(context.Background(), charSource)
	charExpectErr(t, err, "read partial blob", "disk offline")
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestFetchRetriesWhenBandwidthWaitFails(t *testing.T) {
	calls := 0
	fetcher := charFetcher(t, func(context.Context, app.CDNRequest) (*app.CDNResponse, error) {
		calls++
		return charOK("x"), nil
	})
	fetcher.Limits = app.FetchLimits{BytesPerSecond: 1}
	fetcher.Wait = func(context.Context, time.Duration) error { return errors.New("pause refused") }
	_, _, _, err := fetcher.Fetch(context.Background(), charSource)
	charExpectErr(t, err, "3 attempts", "bandwidth wait", "pause refused")
	if calls != app.FetchMaxAttempts {
		t.Fatalf("calls = %d, want %d", calls, app.FetchMaxAttempts)
	}
}
