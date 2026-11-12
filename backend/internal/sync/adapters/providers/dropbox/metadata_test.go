package dropbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jooservices/xflickr/internal/sync/app"
)

const testToken = "dropbox-test-token"

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type trackedBody struct {
	reader io.Reader
	closed bool
}

func (b *trackedBody) Read(content []byte) (int, error) { return b.reader.Read(content) }

func (b *trackedBody) Close() error {
	b.closed = true
	return nil
}

func newClient(t *testing.T, transport http.RoundTripper) *Client {
	t.Helper()
	client, err := New(&http.Client{Transport: transport}, testToken)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client
}

func response(status int, body io.Reader) *http.Response {
	resp, _ := trackedResponse(status, body)
	return resp
}

func trackedResponse(status int, body io.Reader) (*http.Response, *trackedBody) {
	tracked := &trackedBody{reader: body}
	return &http.Response{StatusCode: status, Body: tracked}, tracked
}

func TestCheckContentMatchesFile(t *testing.T) {
	wantHash := strings.Repeat("aB", 32)
	contentHash := strings.ToLower(wantHash)
	var responseBody *trackedBody
	client := newClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", request.Method)
		}
		if request.URL.String() != "https://api.dropboxapi.com/2/files/get_metadata" {
			t.Errorf("URL = %q, want Dropbox metadata endpoint", request.URL)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer "+testToken {
			t.Errorf("Authorization = %q, want injected token", got)
		}
		if got := request.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if got, want := string(body), `{"path":"/folder/photo.jpg","include_deleted":false}`; got != want {
			t.Errorf("request body = %s, want %s", got, want)
		}
		if request.Context() == nil {
			t.Error("request context is nil")
		}
		resp, tracked := trackedResponse(http.StatusOK, strings.NewReader(fmt.Sprintf(`{".tag":"file","content_hash":%q}`, contentHash)))
		responseBody = tracked
		return resp, nil
	}))

	matched, err := client.CheckContent(t.Context(), "/folder/photo.jpg", wantHash)
	if err != nil || !matched {
		t.Fatalf("CheckContent() = (%t, %v), want (true, nil)", matched, err)
	}
	if responseBody == nil || !responseBody.closed {
		t.Fatal("metadata response body was not closed")
	}
}

func TestCheckContentMismatchAndMissing(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "hash mismatch", status: http.StatusOK, body: `{".tag":"file","content_hash":"` + strings.Repeat("0", 64) + `"}`},
		{name: "path not found", status: http.StatusConflict, body: `{"error":{".tag":"path","path":{".tag":"not_found"}}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				return response(test.status, strings.NewReader(test.body)), nil
			}))
			matched, err := client.CheckContent(t.Context(), "/photo.jpg", strings.Repeat("a", 64))
			if err != nil || matched {
				t.Fatalf("CheckContent() = (%t, %v), want (false, nil)", matched, err)
			}
		})
	}
}

func TestCheckContentRejectsInvalidInput(t *testing.T) {
	calls := 0
	client := newClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return response(http.StatusOK, strings.NewReader(`{}`)), nil
	}))
	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	tests := []struct {
		name   string
		client *Client
		ctx    context.Context
		path   string
		hash   string
	}{
		{name: "nil context", client: client, path: "/photo.jpg", hash: strings.Repeat("a", 64)},
		{name: "canceled context", client: client, ctx: canceled, path: "/photo.jpg", hash: strings.Repeat("a", 64)},
		{name: "nil client", ctx: t.Context(), path: "/photo.jpg", hash: strings.Repeat("a", 64)},
		{name: "uninitialized client", client: &Client{}, ctx: t.Context(), path: "/photo.jpg", hash: strings.Repeat("a", 64)},
		{name: "missing token", client: &Client{httpClient: &http.Client{}}, ctx: t.Context(), path: "/photo.jpg", hash: strings.Repeat("a", 64)},
		{name: "empty path", client: client, ctx: t.Context(), hash: strings.Repeat("a", 64)},
		{name: "root path", client: client, ctx: t.Context(), path: "/", hash: strings.Repeat("a", 64)},
		{name: "relative path", client: client, ctx: t.Context(), path: "photo.jpg", hash: strings.Repeat("a", 64)},
		{name: "dot component", client: client, ctx: t.Context(), path: "/folder/./photo.jpg", hash: strings.Repeat("a", 64)},
		{name: "parent component", client: client, ctx: t.Context(), path: "/folder/../photo.jpg", hash: strings.Repeat("a", 64)},
		{name: "nul path", client: client, ctx: t.Context(), path: "/photo\x00.jpg", hash: strings.Repeat("a", 64)},
		{name: "short hash", client: client, ctx: t.Context(), path: "/photo.jpg", hash: "abc"},
		{name: "wrong hash byte length", client: client, ctx: t.Context(), path: "/photo.jpg", hash: strings.Repeat("a", 62)},
		{name: "non hex hash", client: client, ctx: t.Context(), path: "/photo.jpg", hash: strings.Repeat("g", 64)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.client.CheckContent(test.ctx, test.path, test.hash); err == nil {
				t.Fatal("CheckContent() error = nil, want validation error")
			}
		})
	}
	if calls != 0 {
		t.Fatalf("transport called %d times for invalid input, want none", calls)
	}
}

func TestCheckContentRejectsInvalidMetadata(t *testing.T) {
	invalidHash := strings.Repeat("x", 64)
	tests := []struct {
		name string
		body string
	}{
		{name: "folder", body: `{".tag":"folder","content_hash":"` + strings.Repeat("a", 64) + `"}`},
		{name: "missing content hash", body: `{".tag":"file"}`},
		{name: "wrong content hash byte length", body: `{".tag":"file","content_hash":"` + strings.Repeat("a", 62) + `"}`},
		{name: "malformed content hash", body: `{".tag":"file","content_hash":"` + invalidHash + `"}`},
		{name: "invalid JSON", body: `{".tag":"file"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				return response(http.StatusOK, strings.NewReader(test.body)), nil
			}))
			if _, err := client.CheckContent(t.Context(), "/photo.jpg", strings.Repeat("a", 64)); err == nil {
				t.Fatal("CheckContent() error = nil, want metadata error")
			}
		})
	}

	t.Run("oversized body", func(t *testing.T) {
		client := newClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusOK, strings.NewReader(strings.Repeat(" ", maxResponseBodyBytes+1))), nil
		}))
		if _, err := client.CheckContent(t.Context(), "/photo.jpg", strings.Repeat("a", 64)); err == nil {
			t.Fatal("CheckContent() error = nil, want oversized-body error")
		}
	})
}

func TestCheckContentMapsProviderErrors(t *testing.T) {
	secretBody := "sensitive-response-body"
	tests := []struct {
		name      string
		status    int
		body      string
		wantError error
		transport error
	}{
		{name: "authentication", status: http.StatusUnauthorized, body: secretBody, wantError: app.ErrAuthentication},
		{name: "rate limited", status: http.StatusTooManyRequests, body: secretBody, wantError: app.ErrRateLimited},
		{name: "other conflict", status: http.StatusConflict, body: `{"error":{".tag":"too_many_write_operations"}}`},
		{name: "server error", status: http.StatusInternalServerError, body: secretBody},
		{name: "transport error", transport: errors.New("transport failed with " + testToken + " and " + secretBody)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				if test.transport != nil {
					return nil, test.transport
				}
				return response(test.status, strings.NewReader(test.body)), nil
			}))
			_, err := client.CheckContent(t.Context(), "/photo.jpg", strings.Repeat("a", 64))
			if err == nil {
				t.Fatal("CheckContent() error = nil, want provider error")
			}
			if test.wantError != nil && !errors.Is(err, test.wantError) {
				t.Errorf("CheckContent() error = %v, want errors.Is(_, %v)", err, test.wantError)
			}
			if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), secretBody) {
				t.Errorf("error leaked credential or response body: %v", err)
			}
		})
	}
}

func TestCheckContentCancelsRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	client := newClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		cancel()
		<-request.Context().Done()
		return nil, request.Context().Err()
	}))

	_, err := client.CheckContent(ctx, "/photo.jpg", strings.Repeat("a", 64))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CheckContent() error = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Fatalf("transport calls = %d, want 1", calls)
	}
}

func TestNewRejectsInvalidDependencies(t *testing.T) {
	tests := []struct {
		name   string
		client *http.Client
		token  string
	}{
		{name: "nil HTTP client", token: testToken},
		{name: "empty token", client: &http.Client{}},
		{name: "whitespace token", client: &http.Client{}, token: " \t\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := New(test.client, test.token); err == nil {
				t.Fatal("New() error = nil, want invalid dependency error")
			}
		})
	}
}
