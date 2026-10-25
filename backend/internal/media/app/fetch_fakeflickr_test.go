package app_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jooservices/xflickr/internal/fakeflickr"
	"github.com/jooservices/xflickr/internal/media/adapters/cdn"
	"github.com/jooservices/xflickr/internal/media/adapters/local"
	"github.com/jooservices/xflickr/internal/media/app"
)

func TestFetcherDownloadsFakeFlickrCDNOriginal(t *testing.T) {
	server := httptest.NewServer(fakeflickr.NewServer(fakeflickr.Dataset{
		Photos: []fakeflickr.Photo{{ID: "52000000001", Owner: "owner@example.test", Title: "Original"}},
	}, nil))
	defer server.Close()
	blobs, err := local.New(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	fetcher := &app.Fetcher{Blobs: blobs, HTTP: cdn.New(nil)}
	key, checksum, size, err := fetcher.Fetch(context.Background(), server.URL+"/images/52000000001.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if key == "" || len(checksum) != 64 || size == 0 {
		t.Fatalf("fetch result = %q, %q, %d", key, checksum, size)
	}
	blob, err := blobs.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer blob.Close()
	if _, err := io.Copy(io.Discard, blob); err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(server.URL + "/images/missing.jpg")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("missing photo status = %d", response.StatusCode)
	}
}
