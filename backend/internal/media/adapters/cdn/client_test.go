package cdn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jooservices/xflickr/internal/media/app"
)

func TestClientStreamsResponsesAndStopsRedirects(t *testing.T) {
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "http://127.0.0.1:1/redirected")
		w.WriteHeader(http.StatusFound)
	}))
	defer redirect.Close()
	client := New(nil)
	response, err := client.Get(context.Background(), app.CDNRequest{URL: redirect.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("status = %d", response.StatusCode)
	}
}

func TestClientRejectsNilInputs(t *testing.T) {
	var client *Client
	if _, err := client.Get(context.Background(), app.CDNRequest{URL: "http://example.test"}); err == nil {
		t.Fatal("nil client accepted")
	}
	if _, err := New(nil).Get(nil, app.CDNRequest{URL: "http://example.test"}); err == nil {
		t.Fatal("nil context accepted")
	}
	if _, err := New(nil).Get(context.Background(), app.CDNRequest{}); err == nil {
		t.Fatal("empty URL accepted")
	}
	if ResponseHeaderTimeout <= 0 {
		t.Fatalf("header timeout = %s", ResponseHeaderTimeout)
	}
}

func TestClientForwardsRangeAndUsesDefaultHeaderTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=4-" {
			t.Errorf("Range = %q", got)
		}
		w.Header().Set("Content-Range", "bytes 4-7/8")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("data"))
	}))
	defer server.Close()
	client := New(nil)
	response, err := client.Get(context.Background(), app.CDNRequest{URL: server.URL, Range: "bytes=4-"})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusPartialContent || response.ContentLength != 4 || response.ContentRange != "bytes 4-7/8" {
		t.Fatalf("CDN response = %+v", response)
	}
	transport, ok := client.client.Transport.(*http.Transport)
	if !ok || transport.ResponseHeaderTimeout != ResponseHeaderTimeout {
		t.Fatalf("transport timeout = %#v", client.client.Transport)
	}
	if _, err := New(nil).Get(context.Background(), app.CDNRequest{URL: ":bad"}); err == nil {
		t.Fatal("invalid URL accepted")
	}
}
