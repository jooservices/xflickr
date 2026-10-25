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
	"testing"

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
