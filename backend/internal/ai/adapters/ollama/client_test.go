package ollama

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	aiapp "github.com/jooservices/xflickr/internal/ai/app"
)

func TestNewValidatesConfiguration(t *testing.T) {
	base := Config{URL: "http://127.0.0.1:11434", Model: "vision-model", Timeout: time.Second, Concurrency: 2}
	tests := []struct {
		name   string
		change func(*Config)
		valid  bool
	}{
		{name: "valid", valid: true},
		{name: "invalid URL", change: func(c *Config) { c.URL = "file:///tmp/ollama" }},
		{name: "URL credentials", change: func(c *Config) { c.URL = "http://user:password@127.0.0.1:11434" }},
		{name: "URL query", change: func(c *Config) { c.URL = "http://127.0.0.1:11434?token=secret" }},
		{name: "missing model", change: func(c *Config) { c.Model = " " }},
		{name: "zero timeout", change: func(c *Config) { c.Timeout = 0 }},
		{name: "zero concurrency", change: func(c *Config) { c.Concurrency = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := base
			if test.change != nil {
				test.change(&config)
			}
			client, err := New(config)
			if test.valid {
				if err != nil || client == nil {
					t.Fatalf("New() = %v, %v; want non-nil client", client, err)
				}
				return
			}
			if client != nil || !errors.Is(err, aiapp.ErrInvalidConfiguration) {
				t.Fatalf("New() = %v, %v; want ErrInvalidConfiguration", client, err)
			}
		})
	}
}

func TestDescribeSuccessUsesVisionChatAndCachesModelVersion(t *testing.T) {
	image := []byte("fake image bytes")
	var showCalls atomic.Int32
	var chatCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		switch r.URL.Path {
		case "/api/show":
			showCalls.Add(1)
			var request showRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode show request: %v", err)
			}
			if request.Model != "vision-model" {
				t.Errorf("show model = %q", request.Model)
			}
			writeJSON(t, w, map[string]string{"modified_at": "2026-10-27T12:00:00Z"})
		case "/api/chat":
			chatCalls.Add(1)
			var request chatRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode chat request: %v", err)
			}
			if request.Model != "vision-model" || request.Stream || len(request.Messages) != 1 || request.Options["temperature"] != float64(0) {
				t.Errorf("chat request settings = %#v", request)
			}
			if !strings.Contains(request.Messages[0].Content, "strict JSON") || len(request.Messages[0].Images) != 1 || request.Messages[0].Images[0] != base64.StdEncoding.EncodeToString(image) {
				t.Errorf("chat prompt/image mismatch: %#v", request.Messages)
			}
			if request.Format["type"] != "object" {
				t.Errorf("chat format = %#v, want JSON schema", request.Format)
			}
			writeJSON(t, w, map[string]any{"message": map[string]string{"content": `{"text":" A cat on a chair. ","objects":["cat"," chair "]}`}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newClient(t, server, time.Second, 2)

	for range 2 {
		caption, err := client.Describe(context.Background(), image)
		if err != nil {
			t.Fatalf("Describe() error = %v", err)
		}
		if caption.Text != "A cat on a chair." || caption.Model != "vision-model" || caption.Version != "2026-10-27T12:00:00Z" || len(caption.Objects) != 2 || caption.Objects[1] != "chair" {
			t.Fatalf("Describe() = %#v", caption)
		}
	}
	if showCalls.Load() != 1 || chatCalls.Load() != 2 {
		t.Fatalf("Ollama calls: show=%d chat=%d; want 1 and 2", showCalls.Load(), chatCalls.Load())
	}
}

func TestDescribeRetriesInvalidCaptionJSONOnce(t *testing.T) {
	var chatCalls atomic.Int32
	server := testOllama(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			writeJSON(t, w, map[string]string{"modified_at": "version-1"})
			return
		}
		if chatCalls.Add(1) == 1 {
			writeChatContent(t, w, "not-json")
			return
		}
		writeChatContent(t, w, `{"text":"A dog","objects":["dog"]}`)
	})
	client := newClient(t, server, time.Second, 1)
	caption, err := client.Describe(context.Background(), []byte("image"))
	if err != nil || caption.Text != "A dog" || chatCalls.Load() != 2 {
		t.Fatalf("Describe() = %#v, %v; chat calls = %d", caption, err, chatCalls.Load())
	}
}

func TestDescribeReturnsInvalidResponseAfterRetry(t *testing.T) {
	var chatCalls atomic.Int32
	server := testOllama(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			writeJSON(t, w, map[string]string{"modified_at": "version-1"})
			return
		}
		chatCalls.Add(1)
		writeChatContent(t, w, "not-json")
	})
	client := newClient(t, server, time.Second, 1)
	if _, err := client.Describe(context.Background(), []byte("image")); !errors.Is(err, aiapp.ErrInvalidResponse) {
		t.Fatalf("Describe() error = %v, want ErrInvalidResponse", err)
	}
	if chatCalls.Load() != 2 {
		t.Fatalf("chat calls = %d, want 2", chatCalls.Load())
	}
}

func TestDescribeValidatesInputAndModelMetadata(t *testing.T) {
	server := testOllama(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]string{"modified_at": " "})
	})
	client := newClient(t, server, time.Second, 1)
	if _, err := client.Describe(nil, []byte("image")); !errors.Is(err, aiapp.ErrInvalidConfiguration) {
		t.Fatalf("Describe(nil context) error = %v", err)
	}
	if _, err := client.Describe(context.Background(), nil); !errors.Is(err, aiapp.ErrInvalidImage) {
		t.Fatalf("Describe(empty image) error = %v", err)
	}
	if _, err := client.Describe(context.Background(), []byte("image")); !errors.Is(err, aiapp.ErrInvalidResponse) {
		t.Fatalf("Describe(missing version) error = %v", err)
	}
}

func TestDescribeTimeoutUsesConfiguredSetting(t *testing.T) {
	server := testOllama(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			writeJSON(t, w, map[string]string{"modified_at": "version-1"})
			return
		}
		time.Sleep(100 * time.Millisecond)
		writeChatContent(t, w, `{"text":"late","objects":[]}`)
	})
	client := newClient(t, server, 20*time.Millisecond, 1)
	if _, err := client.Describe(context.Background(), []byte("image")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Describe() error = %v, want configured timeout", err)
	}
}

func TestDescribeConcurrencyUsesConfiguredLimit(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	var calls atomic.Int32
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	server := testOllama(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			writeJSON(t, w, map[string]string{"modified_at": "version-1"})
			return
		}
		current := active.Add(1)
		for seen := maximum.Load(); current > seen && !maximum.CompareAndSwap(seen, current); seen = maximum.Load() {
		}
		calls.Add(1)
		started <- struct{}{}
		<-release
		active.Add(-1)
		writeChatContent(t, w, `{"text":"image","objects":[]}`)
	})
	client := newClient(t, server, time.Second, 1)
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := client.Describe(context.Background(), []byte("image"))
			results <- err
		}()
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first Ollama chat did not start")
	}
	select {
	case <-started:
		t.Fatal("second Ollama chat exceeded the configured concurrency of one")
	case <-time.After(25 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(release) })
	for range 2 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatalf("Describe() error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Describe() did not complete after releasing the fake provider")
		}
	}
	if calls.Load() != 2 || maximum.Load() != 1 {
		t.Fatalf("chat calls=%d max concurrency=%d, want 2 and 1", calls.Load(), maximum.Load())
	}
}

func TestDescribeCircuitOpensAfterFiveProviderFailures(t *testing.T) {
	var calls atomic.Int32
	server := testOllama(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, `{"error":"model unavailable"}`, http.StatusServiceUnavailable)
	})
	client := newClient(t, server, time.Second, 1)
	for range defaultFailureLimit {
		if _, err := client.Describe(context.Background(), []byte("image")); err == nil || errors.Is(err, aiapp.ErrCircuitOpen) {
			t.Fatalf("Describe() error = %v, want provider failure", err)
		}
	}
	if _, err := client.Describe(context.Background(), []byte("image")); !errors.Is(err, aiapp.ErrCircuitOpen) {
		t.Fatalf("sixth Describe() error = %v, want ErrCircuitOpen", err)
	}
	if calls.Load() != defaultFailureLimit {
		t.Fatalf("HTTP calls = %d, want %d before circuit opens", calls.Load(), defaultFailureLimit)
	}
}

func TestCallerCancellationDoesNotCountAsProviderFailure(t *testing.T) {
	var calls atomic.Int32
	canceledRequest := make(chan struct{})
	releaseCanceledRequest := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseCanceledRequest) }) })
	server := testOllama(t, func(w http.ResponseWriter, _ *http.Request) {
		current := calls.Add(1)
		if current == 5 {
			close(canceledRequest)
			<-releaseCanceledRequest
		}
		http.Error(w, `{"error":"unavailable"}`, http.StatusServiceUnavailable)
	})
	client := newClient(t, server, time.Second, 1)
	for range 4 {
		if _, err := client.Describe(context.Background(), []byte("image")); err == nil {
			t.Fatal("Describe() unexpectedly succeeded")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := client.Describe(ctx, []byte("image"))
		result <- err
	}()
	select {
	case <-canceledRequest:
	case <-time.After(time.Second):
		t.Fatal("fifth request did not reach the fake provider")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled Describe() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Describe() did not return after caller cancellation")
	}
	releaseOnce.Do(func() { close(releaseCanceledRequest) })
	if _, err := client.Describe(context.Background(), []byte("image")); err == nil {
		t.Fatal("fifth provider failure unexpectedly succeeded")
	}
	if _, err := client.Describe(context.Background(), []byte("image")); !errors.Is(err, aiapp.ErrCircuitOpen) {
		t.Fatalf("next Describe() error = %v, want ErrCircuitOpen", err)
	}
	if calls.Load() != 6 {
		t.Fatalf("HTTP calls = %d, want 6; cancellation should not count as provider failure", calls.Load())
	}
}

func TestPostReportsProviderAndResponseErrors(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		wantError bool
	}{
		{name: "provider error with message", status: http.StatusServiceUnavailable, body: `{"error":"offline"}`, wantError: true},
		{name: "provider error without message", status: http.StatusBadGateway, body: `not-json`, wantError: true},
		{name: "malformed success JSON", status: http.StatusOK, body: `{`, wantError: true},
		{name: "valid success JSON", status: http.StatusOK, body: `{"modified_at":"version-1"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			client := newClient(t, server, time.Second, 1)
			var result showResponse
			err := client.post(context.Background(), "api/show", showRequest{Model: "vision-model"}, &result)
			if test.wantError && err == nil {
				t.Fatal("post() succeeded, want error")
			}
			if !test.wantError && (err != nil || result.ModifiedAt != "version-1") {
				t.Fatalf("post() = %#v, %v", result, err)
			}
		})
	}
}

func TestPostHandlesTransportReadAndOversizedResponseErrors(t *testing.T) {
	t.Run("transport", func(t *testing.T) {
		client := newClientWithHTTPClient(t, Config{
			URL: "http://ollama.invalid", Model: "vision-model", Timeout: time.Second, Concurrency: 1,
			HTTPClient: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("offline")
			})},
		})
		if err := client.post(context.Background(), "api/show", showRequest{Model: "vision-model"}, &showResponse{}); err == nil {
			t.Fatal("post() succeeded despite transport failure")
		}
	})
	t.Run("read error", func(t *testing.T) {
		client := newClientWithHTTPClient(t, Config{
			URL: "http://ollama.invalid", Model: "vision-model", Timeout: time.Second, Concurrency: 1,
			HTTPClient: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: errorBody{}, Header: make(http.Header)}, nil
			})},
		})
		if err := client.post(context.Background(), "api/show", showRequest{Model: "vision-model"}, &showResponse{}); err == nil {
			t.Fatal("post() succeeded despite response read error")
		}
	})
	t.Run("oversized body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, strings.Repeat("x", maxResponseBytes+1))
		}))
		defer server.Close()
		client := newClient(t, server, time.Second, 1)
		if err := client.post(context.Background(), "api/show", showRequest{Model: "vision-model"}, &showResponse{}); !errors.Is(err, aiapp.ErrInvalidResponse) {
			t.Fatalf("post() error = %v, want ErrInvalidResponse", err)
		}
	})
}

func TestPostRejectsUnencodableRequest(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client := newClient(t, server, time.Second, 1)
	if err := client.post(context.Background(), "api/show", make(chan int), &showResponse{}); err == nil {
		t.Fatal("post() succeeded with an unencodable request")
	}
}

func testOllama(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func newClient(t *testing.T, server *httptest.Server, timeout time.Duration, concurrency int) *Client {
	t.Helper()
	return newClientWithHTTPClient(t, Config{
		URL: server.URL, Model: "vision-model", Timeout: timeout, Concurrency: concurrency, HTTPClient: server.Client(),
	})
}

func newClientWithHTTPClient(t *testing.T, config Config) *Client {
	t.Helper()
	client, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("write JSON response: %v", err)
	}
}

func writeChatContent(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
	writeJSON(t, w, map[string]any{"message": map[string]string{"content": content}})
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type errorBody struct{}

func (errorBody) Read([]byte) (int, error) { return 0, errors.New("body read failed") }
func (errorBody) Close() error             { return nil }
