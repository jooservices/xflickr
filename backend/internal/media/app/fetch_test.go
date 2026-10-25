package app_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/media/adapters/cdn"
	"github.com/jooservices/xflickr/internal/media/adapters/local"
	"github.com/jooservices/xflickr/internal/media/app"
)

func fetchRig(t *testing.T, handler http.HandlerFunc) (*app.Fetcher, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	store, err := local.New(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	return &app.Fetcher{Blobs: store, HTTP: cdn.New(server.Client().Transport)}, server
}

func TestFetcherResumesInterruptedResponseAndChecksChecksum(t *testing.T) {
	content := "partial-response-resumed"
	requests := 0
	fetcher, server := fetchRig(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.Header().Set("Content-Length", "24")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "partial-")
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			return
		}
		if r.Header.Get("Range") != "bytes=8-" {
			t.Errorf("Range = %q", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Range", "bytes 8-23/24")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)-8))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, content[8:])
	})
	defer server.Close()
	key, checksum, size, err := fetcher.Fetch(context.Background(), server.URL+"/asset", int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
	if key == "" || checksum != want || size != int64(len(content)) || requests != 2 {
		t.Fatalf("Fetch() = %q, %q, %d, requests=%d", key, checksum, size, requests)
	}
	blob, err := fetcher.Blobs.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer blob.Close()
	data, err := io.ReadAll(blob)
	if err != nil || string(data) != content {
		t.Fatalf("stored bytes = %q, %v", data, err)
	}
}

func TestFetcherRetriesLengthMismatchThenSucceeds(t *testing.T) {
	calls := 0
	fetcher, server := fetchRig(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Content-Length", "10")
			_, _ = io.WriteString(w, "short")
			return
		}
		w.Header().Set("Content-Length", "5")
		_, _ = io.WriteString(w, "bytes")
	})
	defer server.Close()
	_, _, size, err := fetcher.Fetch(context.Background(), server.URL+"/asset", int64(5))
	if err != nil || size != 5 || calls != 2 {
		t.Fatalf("Fetch() size=%d calls=%d err=%v", size, calls, err)
	}
}

func TestFetcherRestartsAfterUnsatisfiableResume(t *testing.T) {
	calls := 0
	fetcher, server := fetchRig(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_, _ = io.WriteString(w, "complete")
			return
		}
		if calls == 2 {
			if r.Header.Get("Range") == "" {
				t.Errorf("expected resume Range header")
			}
			http.Error(w, "range not satisfiable", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if r.Header.Get("Range") != "" {
			t.Errorf("restart Range = %q", r.Header.Get("Range"))
		}
		_, _ = io.WriteString(w, "complete")
	})
	defer server.Close()
	_, _, size, err := fetcher.Fetch(context.Background(), server.URL+"/asset")
	if err != nil || size != 8 || calls != 1 {
		t.Fatalf("initial fetch size=%d calls=%d err=%v", size, calls, err)
	}
	_, _, size, err = fetcher.Fetch(context.Background(), server.URL+"/asset")
	if err != nil || size != 8 || calls != 3 {
		t.Fatalf("retry fetch size=%d calls=%d err=%v", size, calls, err)
	}
}

func TestFetcherSerializesConcurrentFetchesForSameBlob(t *testing.T) {
	var mu sync.Mutex
	calls, active, maxActive := 0, 0, 0
	started := make(chan struct{})
	continueFirst := make(chan struct{})
	fetcher, server := fetchRig(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		active++
		if active > maxActive {
			maxActive = active
		}
		call := calls
		mu.Unlock()
		defer func() { mu.Lock(); active--; mu.Unlock() }()
		if call == 1 {
			close(started)
			<-continueFirst
		}
		if r.Header.Get("Range") != "" {
			http.Error(w, "range not satisfiable", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		_, _ = io.WriteString(w, "content")
	})
	defer server.Close()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key, _, size, err := fetcher.Fetch(context.Background(), server.URL+"/same")
			if err == nil && (key == "" || size != 7) {
				err = fmt.Errorf("unexpected fetch result key=%q size=%d", key, size)
			}
			results <- err
		}()
		if i == 0 {
			<-started
		}
	}
	close(continueFirst)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if maxActive != 1 {
		t.Fatalf("concurrent requests for one blob = %d", maxActive)
	}
}

func TestFetcherRefreshesUnavailableURLOnce(t *testing.T) {
	calls := 0
	fetcher, server := fetchRig(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if strings.HasSuffix(r.URL.Path, "/old") {
			http.Error(w, "expired", http.StatusForbidden)
			return
		}
		_, _ = io.WriteString(w, "fresh")
	})
	defer server.Close()
	refreshes := 0
	fetcher.Refresh = func(context.Context, string) (string, int64, error) {
		refreshes++
		return server.URL + "/new", 5, nil
	}
	_, _, size, err := fetcher.Fetch(context.Background(), server.URL+"/old")
	if err != nil || size != 5 || refreshes != 1 || calls != 2 {
		t.Fatalf("Fetch() size=%d refreshes=%d calls=%d err=%v", size, refreshes, calls, err)
	}
}

func TestFetcherRefreshMovesHostAndReleasesBothHostSlots(t *testing.T) {
	oldServer, newServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Error(w, "expired", http.StatusForbidden)
			return
		}
		_, _ = io.WriteString(w, "old")
	})), httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "fresh")
	}))
	defer oldServer.Close()
	defer newServer.Close()
	store, err := local.New(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	fetcher := &app.Fetcher{
		Blobs:  store,
		HTTP:   cdn.New(http.DefaultTransport),
		Limits: app.FetchLimits{PerHostConcurrency: 1},
		Refresh: func(context.Context, string) (string, int64, error) {
			return newServer.URL + "/fresh", 5, nil
		},
	}

	fetchWithTimeout := func(source string) error {
		result := make(chan error, 1)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, _, _, err := fetcher.Fetch(ctx, source)
			result <- err
		}()
		select {
		case err := <-result:
			return err
		case <-time.After(2 * time.Second):
			return errors.New("Fetch() did not return")
		}
	}
	if err := fetchWithTimeout(oldServer.URL + "/old"); err != nil {
		t.Fatalf("Fetch() after cross-host refresh: %v", err)
	}
	if err := fetchWithTimeout(oldServer.URL + "/healthy"); err != nil {
		t.Fatalf("old host slot was not released: %v", err)
	}
	if err := fetchWithTimeout(newServer.URL + "/another"); err != nil {
		t.Fatalf("new host slot was not released: %v", err)
	}
}

func TestFetcherRefreshStillRetriesAfterTwoTransportFailures(t *testing.T) {
	calls := 0
	fetcher, server := fetchRig(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			http.NotFound(w, nil)
			return
		}
		_, _ = io.WriteString(w, "fresh")
	})
	defer server.Close()
	fetcher.HTTP = &failingThenCDN{transport: cdn.New(server.Client().Transport), failures: 2}
	refreshes := 0
	fetcher.Refresh = func(context.Context, string) (string, int64, error) {
		refreshes++
		return server.URL + "/refreshed", 5, nil
	}
	_, _, size, err := fetcher.Fetch(context.Background(), server.URL+"/old")
	if err != nil || size != 5 || calls != 2 || refreshes != 1 || fetcher.HTTP.(*failingThenCDN).calls != 4 {
		t.Fatalf("Fetch() size=%d requests=%d refreshes=%d HTTP calls=%d err=%v", size, calls, refreshes, fetcher.HTTP.(*failingThenCDN).calls, err)
	}
}

type failingThenCDN struct {
	transport app.CDNClient
	failures  int
	calls     int
}

func (c *failingThenCDN) Get(ctx context.Context, request app.CDNRequest) (*app.CDNResponse, error) {
	c.calls++
	if c.calls <= c.failures {
		return nil, errors.New("temporary transport failure")
	}
	return c.transport.Get(ctx, request)
}

func TestFetcherRejectsInvalidRequestAndRetriesFailure(t *testing.T) {
	fetcher, server := fetchRig(t, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "missing", http.StatusNotFound) })
	defer server.Close()
	if _, _, _, err := fetcher.Fetch(nil, server.URL); err == nil {
		t.Fatal("nil context accepted")
	}
	if _, _, _, err := fetcher.Fetch(context.Background(), "file:///tmp/nope"); err == nil {
		t.Fatal("non-http URL accepted")
	}
	if _, _, _, err := fetcher.Fetch(context.Background(), server.URL, int64(-1)); err == nil {
		t.Fatal("negative expected size accepted")
	}
	if _, _, _, err := fetcher.Fetch(context.Background(), server.URL, int64(1), int64(2)); err == nil {
		t.Fatal("multiple expected sizes accepted")
	}
	if _, _, _, err := fetcher.Fetch(context.Background(), server.URL); err == nil || !strings.Contains(err.Error(), "3 attempts") {
		t.Fatalf("Fetch() err = %v", err)
	}
}

func TestFetcherRefreshErrorsAndInvalidRefreshAreReported(t *testing.T) {
	for _, tc := range []struct {
		name    string
		refresh app.SizeRefresher
	}{
		{name: "error", refresh: func(context.Context, string) (string, int64, error) { return "", 0, errors.New("refresh failed") }},
		{name: "invalid", refresh: func(context.Context, string) (string, int64, error) { return "", -1, nil }},
		{name: "bad url", refresh: func(context.Context, string) (string, int64, error) { return "ftp://cdn.invalid/x", 3, nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fetcher, server := fetchRig(t, func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) })
			defer server.Close()
			fetcher.Refresh = tc.refresh
			if _, _, _, err := fetcher.Fetch(context.Background(), server.URL); err == nil {
				t.Fatal("Fetch() succeeded")
			}
		})
	}
}

func TestFetcherHonorsCanceledContextAndInvalidRange(t *testing.T) {
	fetcher, server := fetchRig(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			w.Header().Set("Content-Range", "invalid")
			w.WriteHeader(http.StatusPartialContent)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "a")
	})
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := fetcher.Fetch(ctx, server.URL); err == nil {
		t.Fatal("canceled fetch succeeded")
	}
}
