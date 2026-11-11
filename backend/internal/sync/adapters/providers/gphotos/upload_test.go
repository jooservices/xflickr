package gphotos

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jooservices/xflickr/internal/sync/app"
)

const testUploadURL = "https://photoslibrary.googleapis.com/v1/uploads"

func TestUploadBytesSuccess(t *testing.T) {
	content := "photo bytes"
	ctx := context.WithValue(t.Context(), contextKey{}, "request context")
	requests := 0
	client, err := NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", request.Method)
		}
		if request.URL.String() != testUploadURL {
			t.Errorf("URL = %q, want %q", request.URL, testUploadURL)
		}
		if got := request.Header.Get("Content-Type"); got != "application/octet-stream" {
			t.Errorf("Content-Type = %q", got)
		}
		if got := request.Header.Get("X-Goog-Upload-Content-Type"); got != "image/jpeg" {
			t.Errorf("X-Goog-Upload-Content-Type = %q", got)
		}
		if got := request.Header.Get("X-Goog-Upload-Protocol"); got != "raw" {
			t.Errorf("X-Goog-Upload-Protocol = %q", got)
		}
		if request.Context().Value(contextKey{}) != "request context" {
			t.Error("request context value was not forwarded")
		}
		if request.ContentLength != int64(len(content)) {
			t.Errorf("ContentLength = %d, want %d", request.ContentLength, len(content))
		}
		body, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			t.Fatalf("read request body: %v", readErr)
		}
		if string(body) != content {
			t.Errorf("body = %q, want %q", body, content)
		}
		return response(request, http.StatusOK, strings.NewReader("temporary-upload-token")), nil
	})}, testUploadURL)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	token, err := client.UploadBytes(ctx, testAsset(content))
	if err != nil || token != "temporary-upload-token" {
		t.Fatalf("UploadBytes() = %q, %v", token, err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestUploadBytesRejectsInvalidAsset(t *testing.T) {
	requests := 0
	client := mustNewClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return response(nil, http.StatusOK, strings.NewReader("unexpected-token")), nil
	}))

	token, err := client.UploadBytes(t.Context(), app.Asset{})
	if token != "" || !errors.Is(err, app.ErrInvalidAsset) {
		t.Fatalf("UploadBytes() = %q, %v; want ErrInvalidAsset", token, err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestUploadBytesRejectsSizeAndDigestMismatch(t *testing.T) {
	tests := []struct {
		name   string
		change func(*app.Asset)
	}{
		{name: "size", change: func(asset *app.Asset) { asset.Size++ }},
		{name: "digest", change: func(asset *app.Asset) { asset.SHA256 = strings.Repeat("0", 64) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			client := mustNewClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requests++
				if _, err := io.Copy(io.Discard, request.Body); err != nil {
					t.Fatalf("consume request body: %v", err)
				}
				return response(request, http.StatusOK, strings.NewReader("temporary-upload-token")), nil
			}))
			asset := testAsset("photo bytes")
			test.change(&asset)

			token, err := client.UploadBytes(t.Context(), asset)
			if token != "" || !errors.Is(err, app.ErrInvalidAsset) {
				t.Fatalf("UploadBytes() = %q, %v; want ErrInvalidAsset", token, err)
			}
			if strings.Contains(err.Error(), "temporary-upload-token") {
				t.Fatalf("error exposed upload token: %v", err)
			}
			if requests != 1 {
				t.Fatalf("requests = %d, want 1", requests)
			}
		})
	}
}

func TestUploadBytesMapsProviderErrors(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		want       error
		wantMapped bool
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, want: app.ErrAuthentication, wantMapped: true},
		{name: "forbidden", status: http.StatusForbidden, want: app.ErrAuthentication, wantMapped: true},
		{name: "rate limited", status: http.StatusTooManyRequests, want: app.ErrRateLimited, wantMapped: true},
		{name: "server error", status: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := &trackingBody{reader: strings.NewReader("private response body")}
			client := mustNewClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if _, err := io.Copy(io.Discard, request.Body); err != nil {
					t.Fatalf("consume request body: %v", err)
				}
				return responseWithBody(request, test.status, body), nil
			}))

			token, err := client.UploadBytes(t.Context(), testAsset("photo bytes"))
			if token != "" || err == nil {
				t.Fatalf("UploadBytes() = %q, %v; want error", token, err)
			}
			if strings.Contains(err.Error(), "private response body") {
				t.Fatalf("error exposed response body: %v", err)
			}
			if test.wantMapped && !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if !test.wantMapped && (errors.Is(err, app.ErrAuthentication) || errors.Is(err, app.ErrRateLimited)) {
				t.Fatalf("error = %v, unexpectedly mapped", err)
			}
			if !body.closed {
				t.Fatal("response body was not closed")
			}
		})
	}
}

func TestUploadBytesHandlesTransportAndTokenErrors(t *testing.T) {
	t.Run("transport error does not retry or expose details", func(t *testing.T) {
		requests := 0
		client := mustNewClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			return nil, errors.New("upload-token private response body transport detail")
		}))

		token, err := client.UploadBytes(t.Context(), testAsset("photo bytes"))
		if token != "" || err == nil {
			t.Fatalf("UploadBytes() = %q, %v; want error", token, err)
		}
		if strings.Contains(err.Error(), "upload-token") || strings.Contains(err.Error(), "private response body") {
			t.Fatalf("error exposed transport details: %v", err)
		}
		if requests != 1 {
			t.Fatalf("requests = %d, want 1", requests)
		}
	})

	t.Run("response read error is sanitized and closed", func(t *testing.T) {
		body := &trackingBody{reader: errorReader{err: errors.New("upload-token private response body")}}
		client := mustNewClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if _, err := io.Copy(io.Discard, request.Body); err != nil {
				t.Fatalf("consume request body: %v", err)
			}
			return responseWithBody(request, http.StatusOK, body), nil
		}))

		token, err := client.UploadBytes(t.Context(), testAsset("photo bytes"))
		if token != "" || err == nil {
			t.Fatalf("UploadBytes() = %q, %v; want error", token, err)
		}
		if strings.Contains(err.Error(), "upload-token") || strings.Contains(err.Error(), "private response body") {
			t.Fatalf("error exposed response details: %v", err)
		}
		if !body.closed {
			t.Fatal("response body was not closed")
		}
	})

	t.Run("canceled context is preserved", func(t *testing.T) {
		requests := 0
		client := mustNewClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			return nil, errors.New("unexpected transport call")
		}))
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		token, err := client.UploadBytes(ctx, testAsset("photo bytes"))
		if token != "" || !errors.Is(err, context.Canceled) {
			t.Fatalf("UploadBytes() = %q, %v; want context.Canceled", token, err)
		}
		if requests != 0 {
			t.Fatalf("requests = %d, want 0", requests)
		}
	})

	t.Run("empty token", func(t *testing.T) {
		client := mustNewClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if _, err := io.Copy(io.Discard, request.Body); err != nil {
				t.Fatalf("consume request body: %v", err)
			}
			return response(request, http.StatusOK, strings.NewReader(" \n\t")), nil
		}))

		token, err := client.UploadBytes(t.Context(), testAsset("photo bytes"))
		if token != "" || err == nil {
			t.Fatalf("UploadBytes() = %q, %v; want empty token error", token, err)
		}
	})

	t.Run("token over four kibibytes", func(t *testing.T) {
		body := &trackingBody{reader: strings.NewReader(strings.Repeat("x", 4097))}
		client := mustNewClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if _, err := io.Copy(io.Discard, request.Body); err != nil {
				t.Fatalf("consume request body: %v", err)
			}
			return responseWithBody(request, http.StatusOK, body), nil
		}))

		token, err := client.UploadBytes(t.Context(), testAsset("photo bytes"))
		if token != "" || err == nil {
			t.Fatalf("UploadBytes() = %q, %v; want oversized token error", token, err)
		}
		if body.read != 4097 {
			t.Fatalf("read %d response bytes, want at most 4097", body.read)
		}
		if strings.Contains(err.Error(), strings.Repeat("x", 32)) {
			t.Fatalf("error exposed token bytes: %v", err)
		}
		if !body.closed {
			t.Fatal("response body was not closed")
		}
	})
}

func TestNewClientRejectsInvalidArguments(t *testing.T) {
	if _, err := NewClient(nil, testUploadURL); err == nil {
		t.Fatal("NewClient(nil, ...) succeeded")
	}
	for _, uploadURL := range []string{"", "relative/path", "https://", "://invalid"} {
		t.Run(fmt.Sprintf("url %q", uploadURL), func(t *testing.T) {
			if _, err := NewClient(&http.Client{}, uploadURL); err == nil {
				t.Fatalf("NewClient(..., %q) succeeded", uploadURL)
			}
		})
	}
}

type contextKey struct{}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type trackingBody struct {
	reader io.Reader
	read   int
	closed bool
}

func (b *trackingBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	b.read += n
	return n, err
}

func (b *trackingBody) Close() error {
	b.closed = true
	return nil
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

func response(request *http.Request, status int, body io.Reader) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(body), Header: make(http.Header), Request: request}
}

func responseWithBody(request *http.Request, status int, body io.ReadCloser) *http.Response {
	return &http.Response{StatusCode: status, Body: body, Header: make(http.Header), Request: request}
}

func mustNewClient(t *testing.T, transport http.RoundTripper) *Client {
	t.Helper()
	client, err := NewClient(&http.Client{Transport: transport}, testUploadURL)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

func testAsset(content string) app.Asset {
	digest := sha256.Sum256([]byte(content))
	return app.Asset{
		Name: "photo.jpg", ContentType: "image/jpeg", Size: int64(len(content)),
		SHA256: hex.EncodeToString(digest[:]), Content: bytes.NewReader([]byte(content)),
	}
}
