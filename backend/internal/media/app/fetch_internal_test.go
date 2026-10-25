package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestRangeStart(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int64
		ok    bool
	}{
		{"bytes 8-15/16", 8, true}, {"bytes 0-1/*", 0, true}, {"items 8-15/16", 0, false}, {"bytes 8/16", 0, false}, {"bytes nope-15/16", 0, false},
	} {
		got, ok := rangeStart(tc.value)
		if got != tc.want || ok != tc.ok {
			t.Errorf("rangeStart(%q) = %d, %t", tc.value, got, ok)
		}
	}
}

func TestFetchReaders(t *testing.T) {
	reader := &resumableReader{reader: errorReader{err: errors.New("wire error")}}
	if _, err := reader.Read(make([]byte, 2)); !errors.Is(err, io.EOF) || !errors.Is(reader.err, errInterruptedBody) {
		t.Fatalf("read error=%v stored=%v", err, reader.err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&fetchContextReader{ctx: ctx, reader: strings.NewReader("x")}).Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatalf("context read error = %v", err)
	}
	if n, err := (&fetchContextReader{ctx: context.Background(), reader: strings.NewReader("x")}).Read(make([]byte, 1)); n != 1 || err != nil {
		t.Fatalf("normal context read = %d, %v", n, err)
	}
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

func TestFetchHostSemaphoreAndWait(t *testing.T) {
	fetcher := &Fetcher{Limits: FetchLimits{PerHostConcurrency: 1}}
	limit := fetcher.hostLimit("cdn.test")
	if fetcher.hostLimit("cdn.test") != limit {
		t.Fatal("host limiter was not reused")
	}
	if err := fetcher.acquire(context.Background(), limit); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := fetcher.acquire(ctx, limit); !errors.Is(err, context.Canceled) {
		t.Fatalf("blocked acquire error = %v", err)
	}
	fetcher.release(limit)
	unlimited := fetcher.hostLimit("other.test")
	if err := fetcher.acquire(context.Background(), unlimited); err != nil {
		t.Fatal(err)
	}
	fetcher.release(unlimited)
	waited := time.Duration(0)
	fetcher.Wait = func(_ context.Context, d time.Duration) error { waited += d; return nil }
	if err := fetcher.wait(context.Background(), 3*time.Millisecond); err != nil || waited != 3*time.Millisecond {
		t.Fatalf("wait = %s, %v", waited, err)
	}
	fetcher.Wait = nil
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if err := fetcher.wait(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled wait = %v", err)
	}
}

func TestAcquireKeyCancellationReleasesWaiterReference(t *testing.T) {
	fetcher := &Fetcher{}
	release, err := fetcher.acquireKey(context.Background(), "blob")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fetcher.acquireKey(ctx, "blob"); !errors.Is(err, context.Canceled) {
		t.Fatalf("blocked key acquire error = %v", err)
	}
	release()
	fetcher.mu.Lock()
	defer fetcher.mu.Unlock()
	if len(fetcher.keys) != 0 {
		t.Fatalf("key lock map retained entries: %d", len(fetcher.keys))
	}
}

func TestLimitedBodyAppliesSharedBandwidthWait(t *testing.T) {
	limit := &hostLimit{}
	var waits []time.Duration
	body := &limitedBody{ctx: context.Background(), body: strings.NewReader("abcd"), host: limit, rate: 4, wait: func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }}
	buf := make([]byte, 2)
	if n, err := body.Read(buf); n != 2 || err != nil {
		t.Fatalf("read = %d, %v", n, err)
	}
	if n, err := body.Read(buf); n != 2 || err != nil {
		t.Fatalf("read = %d, %v", n, err)
	}
	if len(waits) != 2 || waits[0] <= 0 || waits[1] <= 0 {
		t.Fatalf("waits = %v", waits)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&limitedBody{ctx: ctx, body: strings.NewReader("x")}).Read(buf); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read = %v", err)
	}
}

type fetchHTTPFunc func(context.Context, CDNRequest) (*CDNResponse, error)

func (f fetchHTTPFunc) Get(ctx context.Context, request CDNRequest) (*CDNResponse, error) {
	return f(ctx, request)
}

type missingBlobStore struct{}

func (missingBlobStore) Put(context.Context, string, io.Reader) error       { return nil }
func (missingBlobStore) Get(context.Context, string) (io.ReadCloser, error) { return nil, ErrNotFound }
func (missingBlobStore) Delete(context.Context, string) error               { return nil }

func (missingBlobStore) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", nil
}

func (missingBlobStore) Stat(context.Context, string) (BlobInfo, error) {
	return BlobInfo{}, ErrNotFound
}

func (missingBlobStore) Usage(context.Context) (int64, error) { return 0, nil }
func (missingBlobStore) Full(context.Context) (bool, error)   { return false, nil }

func TestFetcherTransportFailuresAndNilResponse(t *testing.T) {
	for _, do := range []fetchHTTPFunc{
		func(context.Context, CDNRequest) (*CDNResponse, error) { return nil, errors.New("connection reset") },
		func(context.Context, CDNRequest) (*CDNResponse, error) { return nil, nil },
	} {
		fetcher := &Fetcher{Blobs: missingBlobStore{}, HTTP: do}
		if _, _, _, err := fetcher.Fetch(context.Background(), "https://cdn.test/original"); err == nil {
			t.Fatal("transport failure succeeded")
		}
	}
	var fetcher *Fetcher
	if _, _, _, err := fetcher.Fetch(context.Background(), "https://cdn.test/original"); err == nil {
		t.Fatal("nil fetcher succeeded")
	}
}

func TestFetcherRejectsInvalidInitialPartialResponse(t *testing.T) {
	calls := 0
	fetcher := &Fetcher{
		Blobs: missingBlobStore{},
		HTTP: fetchHTTPFunc(func(context.Context, CDNRequest) (*CDNResponse, error) {
			calls++
			return &CDNResponse{StatusCode: 206, ContentRange: "bytes 4-7/8", Body: io.NopCloser(strings.NewReader("data")), ContentLength: 4}, nil
		}),
	}
	if _, _, _, err := fetcher.Fetch(context.Background(), "https://cdn.test/original"); err == nil || calls != FetchMaxAttempts {
		t.Fatalf("Fetch() calls=%d, err=%v", calls, err)
	}
}

func TestFetcherRejectsResponseWithoutBody(t *testing.T) {
	fetcher := &Fetcher{
		Blobs: missingBlobStore{},
		HTTP: fetchHTTPFunc(func(context.Context, CDNRequest) (*CDNResponse, error) {
			return &CDNResponse{StatusCode: 200}, nil
		}),
	}
	if _, _, _, err := fetcher.Fetch(context.Background(), "https://cdn.test/original"); err == nil {
		t.Fatal("response without body succeeded")
	}
	if closeResponse(nil) != nil || closeResponse(&CDNResponse{}) != nil {
		t.Fatal("closing an absent response body failed")
	}
}

type failedStatStore struct{ missingBlobStore }

func (failedStatStore) Stat(context.Context, string) (BlobInfo, error) {
	return BlobInfo{}, errors.New("storage unavailable")
}

func TestFetcherSurfacesBlobInspectionFailure(t *testing.T) {
	fetcher := &Fetcher{Blobs: failedStatStore{}, HTTP: fetchHTTPFunc(func(context.Context, CDNRequest) (*CDNResponse, error) {
		t.Fatal("HTTP called before blob inspection")
		return nil, nil
	})}
	if _, _, _, err := fetcher.Fetch(context.Background(), "https://cdn.test/original"); err == nil || !strings.Contains(err.Error(), "inspect partial blob") {
		t.Fatalf("Fetch() error = %v", err)
	}
}
