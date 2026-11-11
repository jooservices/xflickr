package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	aiapp "github.com/jooservices/xflickr/internal/ai/app"
)

func TestEmbedSuccess(t *testing.T) {
	texts := []string{"  a cat in a garden  ", "A mountain lake"}
	wantEmbeddings := [][]float32{embedding(0.25), embedding(0.75)}
	var showCalls int
	var paths []string
	server := testOllama(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/api/show":
			var request showRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode show request: %v", err)
			}
			if request.Model != "bge-m3" {
				t.Errorf("show model = %q, want bge-m3", request.Model)
			}
			showCalls++
			writeJSON(t, w, map[string]string{"modified_at": fmt.Sprintf("version-%d", showCalls)})
		case "/api/embed":
			var request embedRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode embed request: %v", err)
			}
			if request.Model != "bge-m3" || !slices.Equal(request.Input, texts) {
				t.Errorf("embed request = %#v", request)
			}
			writeJSON(t, w, embedResponse{Embeddings: wantEmbeddings})
		default:
			http.NotFound(w, r)
		}
	})
	client := newClient(t, server, time.Second, 2)

	for call := 1; call <= 2; call++ {
		got, info, err := client.Embed(t.Context(), texts)
		if err != nil {
			t.Fatalf("Embed() error = %v", err)
		}
		if info.Model != "bge-m3" || info.Version != fmt.Sprintf("version-%d", call) {
			t.Fatalf("Embed() model info = %#v", info)
		}
		if len(got) != len(wantEmbeddings) {
			t.Fatalf("Embed() returned %d vectors, want %d", len(got), len(wantEmbeddings))
		}
		for index := range got {
			if len(got[index]) != embeddingDimensions || got[index][0] != wantEmbeddings[index][0] || got[index][embeddingDimensions-1] != wantEmbeddings[index][embeddingDimensions-1] {
				t.Fatalf("Embed() vector %d has wrong order or shape", index)
			}
		}
	}
	if len(paths) != 4 || paths[0] != "/api/show" || paths[1] != "/api/embed" || paths[2] != "/api/show" || paths[3] != "/api/embed" {
		t.Fatalf("request paths = %v, want show/embed for each call", paths)
	}
}

func TestEmbedRejectsInputBeyondContextWindow(t *testing.T) {
	const simulatedContextWindow = 8 * 1024
	text := strings.Repeat("word ", 20_000)
	var providerRejected atomic.Bool
	server := testOllama(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			writeJSON(t, w, map[string]string{"modified_at": "version-1"})
			return
		}
		var request struct {
			Input    []string `json:"input"`
			Truncate *bool    `json:"truncate"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode embed request: %v", err)
			return
		}
		if len(request.Input) != 1 || request.Input[0] != text {
			t.Errorf("embed input was not sent in full")
			return
		}
		if len(request.Input[0]) > simulatedContextWindow {
			if request.Truncate != nil && !*request.Truncate {
				providerRejected.Store(true)
				http.Error(w, `{"error":"input exceeds the context window"}`, http.StatusBadRequest)
				return
			}
			writeJSON(t, w, embedResponse{Embeddings: [][]float32{embedding(0.5)}})
			return
		}
		writeJSON(t, w, embedResponse{Embeddings: [][]float32{embedding(0.5)}})
	})
	client := newClient(t, server, time.Second, 1)

	assertEmbedError(t, client, t.Context(), []string{text})
	if !providerRejected.Load() {
		t.Fatal("provider did not reject the input beyond its context window")
	}
}

func TestEmbedRejectsInvalidTexts(t *testing.T) {
	var calls atomic.Int32
	server := testOllama(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeJSON(t, w, map[string]string{"modified_at": "version-1"})
	})
	client := newClient(t, server, time.Second, 1)
	tests := []struct {
		name  string
		texts []string
	}{
		{name: "nil"},
		{name: "empty", texts: []string{}},
		{name: "too many", texts: make([]string, 17)},
		{name: "empty text", texts: []string{""}},
		{name: "whitespace text", texts: []string{" \t\n "}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			vectors, info, err := client.Embed(t.Context(), test.texts)
			if !errors.Is(err, aiapp.ErrInvalidTexts) || vectors != nil || info != (aiapp.ModelInfo{}) {
				t.Fatalf("Embed() = %v, %#v, %v; want invalid texts and no result", vectors, info, err)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("provider calls = %d, want 0 for invalid input", calls.Load())
	}
	if vectors, info, err := client.Embed(nil, []string{"text"}); !errors.Is(err, aiapp.ErrInvalidConfiguration) || vectors != nil || info != (aiapp.ModelInfo{}) {
		t.Fatalf("Embed(nil context) = %v, %#v, %v", vectors, info, err)
	}
}

func TestEmbedRejectsInvalidResponses(t *testing.T) {
	validEmbedding, err := json.Marshal(embedResponse{Embeddings: [][]float32{embedding(0.5), embedding(0.75)}})
	if err != nil {
		t.Fatal(err)
	}
	shortEmbedding, err := json.Marshal(embedResponse{Embeddings: [][]float32{embedding(0.5)[:1023], embedding(0.75)}})
	if err != nil {
		t.Fatal(err)
	}
	wrongCountEmbedding, err := json.Marshal(embedResponse{Embeddings: [][]float32{embedding(0.5)}})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		showBody  string
		embedBody string
	}{
		{name: "missing version", showBody: `{}`, embedBody: string(validEmbedding)},
		{name: "blank version", showBody: `{"modified_at":" \t "}`, embedBody: string(validEmbedding)},
		{name: "malformed show JSON", showBody: `{`, embedBody: string(validEmbedding)},
		{name: "malformed embed JSON", showBody: `{"modified_at":"version-1"}`, embedBody: `{`},
		{name: "missing vectors", showBody: `{"modified_at":"version-1"}`, embedBody: `{}`},
		{name: "wrong vector count", showBody: `{"modified_at":"version-1"}`, embedBody: string(wrongCountEmbedding)},
		{name: "wrong vector dimension", showBody: `{"modified_at":"version-1"}`, embedBody: string(shortEmbedding)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := testOllama(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/show" {
					_, _ = fmt.Fprint(w, test.showBody)
					return
				}
				_, _ = fmt.Fprint(w, test.embedBody)
			})
			client := newClient(t, server, time.Second, 1)
			vectors, info, err := client.Embed(t.Context(), []string{"first", "second"})
			if !errors.Is(err, aiapp.ErrInvalidResponse) || vectors != nil || info != (aiapp.ModelInfo{}) {
				t.Fatalf("Embed() = %v, %#v, %v; want invalid response and no partial result", vectors, info, err)
			}
		})
	}
}

func TestEmbedProviderFailureAndCancellation(t *testing.T) {
	t.Run("show HTTP failure", func(t *testing.T) {
		server := testOllama(t, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":"unavailable"}`, http.StatusServiceUnavailable)
		})
		client := newClient(t, server, time.Second, 1)
		assertEmbedError(t, client, t.Context(), []string{"text"})
	})

	t.Run("embed HTTP failure", func(t *testing.T) {
		server := testOllama(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/show" {
				writeJSON(t, w, map[string]string{"modified_at": "version-1"})
				return
			}
			http.Error(w, `{"error":"unavailable"}`, http.StatusServiceUnavailable)
		})
		client := newClient(t, server, time.Second, 1)
		assertEmbedError(t, client, t.Context(), []string{"text"})
	})

	t.Run("timeout", func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		var releaseOnce sync.Once
		t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
		server := testOllama(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/show" {
				writeJSON(t, w, map[string]string{"modified_at": "version-1"})
				return
			}
			close(started)
			<-release
			writeJSON(t, w, embedResponse{Embeddings: [][]float32{embedding(0.5)}})
		})
		client := newClient(t, server, 20*time.Millisecond, 1)
		vectors, info, err := client.Embed(t.Context(), []string{"text"})
		releaseOnce.Do(func() { close(release) })
		if !errors.Is(err, context.DeadlineExceeded) || vectors != nil || info != (aiapp.ModelInfo{}) {
			t.Fatalf("Embed() = %v, %#v, %v; want timeout and no result", vectors, info, err)
		}
		select {
		case <-started:
		default:
			t.Fatal("timeout request did not reach provider")
		}
	})

	t.Run("caller cancellation", func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		var releaseOnce sync.Once
		t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
		server := testOllama(t, func(w http.ResponseWriter, _ *http.Request) {
			close(started)
			<-release
			writeJSON(t, w, map[string]string{"modified_at": "late"})
		})
		client := newClient(t, server, time.Second, 1)
		ctx, cancel := context.WithCancel(t.Context())
		result := make(chan error, 1)
		go func() {
			vectors, info, err := client.Embed(ctx, []string{"text"})
			if vectors != nil || info != (aiapp.ModelInfo{}) {
				result <- fmt.Errorf("canceled Embed() returned a partial result: %v, %#v", vectors, info)
				return
			}
			result <- err
		}()
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("request did not reach provider")
		}
		cancel()
		select {
		case err := <-result:
			releaseOnce.Do(func() { close(release) })
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Embed() error = %v, want caller cancellation", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Embed() did not return after caller cancellation")
		}
	})
}

func TestEmbedSharesBreakerWithCaptioner(t *testing.T) {
	var showCalls atomic.Int32
	fifthStarted := make(chan struct{})
	releaseFifth := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseFifth) }) })
	server := testOllama(t, func(w http.ResponseWriter, _ *http.Request) {
		if showCalls.Add(1) == 5 {
			close(fifthStarted)
			<-releaseFifth
			http.Error(w, `{"error":"unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		http.Error(w, `{"error":"unavailable"}`, http.StatusServiceUnavailable)
	})
	client := newClient(t, server, time.Second, 1)
	for range 4 {
		assertEmbedError(t, client, t.Context(), []string{"text"})
	}

	ctx, cancel := context.WithCancel(t.Context())
	canceled := make(chan error, 1)
	go func() {
		vectors, info, err := client.Embed(ctx, []string{"text"})
		if vectors != nil || info != (aiapp.ModelInfo{}) {
			canceled <- errors.New("canceled Embed() returned a partial result")
			return
		}
		canceled <- err
	}()
	select {
	case <-fifthStarted:
	case <-time.After(time.Second):
		t.Fatal("fifth request did not reach provider")
	}
	cancel()
	select {
	case err := <-canceled:
		releaseOnce.Do(func() { close(releaseFifth) })
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled Embed() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled Embed() did not return")
	}
	assertEmbedError(t, client, t.Context(), []string{"text"})
	if _, err := client.Describe(t.Context(), []byte("image")); !errors.Is(err, aiapp.ErrCircuitOpen) {
		t.Fatalf("Describe() error = %v, want shared open circuit", err)
	}
	if _, _, err := client.Embed(t.Context(), []string{"text"}); !errors.Is(err, aiapp.ErrCircuitOpen) {
		t.Fatalf("Embed() error = %v, want shared open circuit", err)
	}
	if showCalls.Load() != 6 {
		t.Fatalf("show calls = %d, want 6 and no request from blocked Describe", showCalls.Load())
	}
}

func TestEmbedSharesConcurrencyWithCaptioner(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	started := make(chan struct{})
	var pathsMu sync.Mutex
	var paths []string
	server := testOllama(t, func(w http.ResponseWriter, r *http.Request) {
		pathsMu.Lock()
		paths = append(paths, r.URL.Path)
		pathsMu.Unlock()
		if r.URL.Path == "/api/show" {
			writeJSON(t, w, map[string]string{"modified_at": "version-1"})
			return
		}
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		writeJSON(t, w, embedResponse{Embeddings: [][]float32{embedding(0.5)}})
	})
	client := newClient(t, server, time.Second, 1)
	firstResult := make(chan error, 1)
	go func() {
		_, _, err := client.Embed(t.Context(), []string{"text"})
		firstResult <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("embedding request did not start")
	}

	embedWait, cancelEmbedWait := context.WithTimeout(t.Context(), 30*time.Millisecond)
	if _, _, err := client.Embed(embedWait, []string{"waiting"}); !errors.Is(err, context.DeadlineExceeded) {
		cancelEmbedWait()
		t.Fatalf("queued Embed() error = %v, want deadline", err)
	}
	cancelEmbedWait()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := client.Describe(ctx, []byte("image")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Describe() while Embed holds semaphore = %v, want deadline", err)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-firstResult:
		if err != nil {
			t.Fatalf("first Embed() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Embed() did not finish after provider release")
	}
	pathsMu.Lock()
	defer pathsMu.Unlock()
	if len(paths) != 2 || paths[0] != "/api/show" || paths[1] != "/api/embed" {
		t.Fatalf("provider paths = %v, want only Embed requests", paths)
	}
}

func assertEmbedError(t *testing.T, client *Client, ctx context.Context, texts []string) {
	t.Helper()
	vectors, info, err := client.Embed(ctx, texts)
	if err == nil || vectors != nil || info != (aiapp.ModelInfo{}) {
		t.Fatalf("Embed() = %v, %#v, %v; want error and no result", vectors, info, err)
	}
}

func embedding(value float32) []float32 {
	vector := make([]float32, 1024)
	for index := range vector {
		vector[index] = value
	}
	return vector
}
