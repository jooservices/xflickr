package elasticsearch_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/search/adapters/elasticsearch"
	"github.com/jooservices/xflickr/internal/search/app"
)

func TestNewIndexValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("constructor validation sent an HTTP request")
	}))
	t.Cleanup(server.Close)

	client := server.Client()
	validPrefix := "xflickr_wtjooxf134_"
	maxPrefix := "a" + strings.Repeat("a", 244) + "_"
	tooLongPrefix := "a" + strings.Repeat("a", 245) + "_"

	tests := []struct {
		name     string
		baseURL  string
		username string
		password string
		prefix   string
		client   *http.Client
		wantErr  bool
	}{
		{name: "valid", baseURL: server.URL, prefix: validPrefix, client: client},
		{name: "valid HTTPS URL", baseURL: "https://search.example", prefix: validPrefix, client: client},
		{name: "valid punctuation", baseURL: server.URL, prefix: "xflickr-test_", client: client},
		{name: "maximum physical name", baseURL: server.URL, prefix: maxPrefix, client: client},
		{name: "empty password", baseURL: server.URL, username: "indexer", prefix: validPrefix, client: client},
		{name: "empty URL", baseURL: "", prefix: validPrefix, client: client, wantErr: true},
		{name: "relative URL", baseURL: "/search", prefix: validPrefix, client: client, wantErr: true},
		{name: "unsupported scheme", baseURL: "ftp://search.example", prefix: validPrefix, client: client, wantErr: true},
		{name: "malformed URL", baseURL: "http://[::1", prefix: validPrefix, client: client, wantErr: true},
		{name: "missing host", baseURL: "http:///search", prefix: validPrefix, client: client, wantErr: true},
		{name: "userinfo", baseURL: "http://user:secret@search.example", prefix: validPrefix, client: client, wantErr: true},
		{name: "query", baseURL: "http://search.example/?token=secret", prefix: validPrefix, client: client, wantErr: true},
		{name: "empty query", baseURL: "http://search.example?", prefix: validPrefix, client: client, wantErr: true},
		{name: "fragment", baseURL: "http://search.example/#secret", prefix: validPrefix, client: client, wantErr: true},
		{name: "empty fragment", baseURL: "http://search.example/#", prefix: validPrefix, client: client, wantErr: true},
		{name: "nil client", baseURL: server.URL, prefix: validPrefix, wantErr: true},
		{name: "empty prefix", baseURL: server.URL, client: client, wantErr: true},
		{name: "prefix without trailing underscore", baseURL: server.URL, prefix: "xflickr", client: client, wantErr: true},
		{name: "prefix starts with underscore", baseURL: server.URL, prefix: "_xflickr_", client: client, wantErr: true},
		{name: "uppercase prefix", baseURL: server.URL, prefix: "XFlickr_", client: client, wantErr: true},
		{name: "prefix with space", baseURL: server.URL, prefix: "x flickr_", client: client, wantErr: true},
		{name: "prefix with unicode", baseURL: server.URL, prefix: "xflickr_é_", client: client, wantErr: true},
		{name: "physical name over 255 bytes", baseURL: server.URL, prefix: tooLongPrefix, client: client, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			index, err := elasticsearch.NewIndex(test.baseURL, test.username, test.password, test.prefix, test.client)
			if test.wantErr {
				if err == nil {
					t.Fatal("NewIndex() error = nil, want an error")
				}
				for _, secret := range []string{"secret", test.password} {
					if secret != "" && strings.Contains(err.Error(), secret) {
						t.Fatalf("NewIndex() error exposed %q", secret)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("NewIndex() error = %v", err)
			}
			if index == nil {
				t.Fatal("NewIndex() returned a nil index")
			}
		})
	}

	if got := len(maxPrefix) + len("photos-v1"); got != 255 {
		t.Fatalf("test maximum physical name length = %d, want 255", got)
	}
	if got := len(tooLongPrefix) + len("photos-v1"); got != 256 {
		t.Fatalf("test overlong physical name length = %d, want 256", got)
	}
}

func TestCreateIndex(t *testing.T) {
	for _, test := range []struct {
		name       string
		basePath   string
		username   string
		password   string
		statusCode int
	}{
		{name: "without authentication", statusCode: http.StatusOK},
		{name: "preserves endpoint base path", basePath: "/search-gateway/v1", statusCode: http.StatusOK},
		{name: "with basic authentication", username: "indexer", password: "password", statusCode: http.StatusCreated},
		{name: "basic authentication with empty password", username: "indexer", statusCode: http.StatusCreated},
		{name: "no content response", statusCode: http.StatusNoContent},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodPut {
					t.Errorf("method = %q, want PUT", r.Method)
				}
				wantPath := test.basePath + "/xflickr_wtjooxf134_photos-v1"
				if r.URL.Path != wantPath {
					t.Errorf("path = %q, want %s", r.URL.Path, wantPath)
				}
				if got := r.Header.Get("Content-Type"); got != "application/json" {
					t.Errorf("Content-Type = %q, want application/json", got)
				}
				gotUsername, gotPassword, hasAuth := r.BasicAuth()
				if test.username == "" {
					if hasAuth || r.Header.Get("Authorization") != "" {
						t.Errorf("unexpected authorization header")
					}
				} else if !hasAuth || gotUsername != test.username || gotPassword != test.password {
					t.Errorf("basic auth = (%q, %q, %t), want (%q, %q, true)", gotUsername, gotPassword, hasAuth, test.username, test.password)
				}

				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("decode request body: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				want := expectedIndexDefinition("xflickr_wtjooxf134_photos")
				if !reflect.DeepEqual(payload, want) {
					t.Errorf("mapping payload = %#v, want %#v", payload, want)
				}
				w.WriteHeader(test.statusCode)
			}))
			t.Cleanup(server.Close)

			index, err := elasticsearch.NewIndex(server.URL+test.basePath, test.username, test.password, "xflickr_wtjooxf134_", server.Client())
			if err != nil {
				t.Fatalf("NewIndex() error = %v", err)
			}
			if err := index.Create(t.Context()); err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			if requests != 1 {
				t.Fatalf("HTTP requests = %d, want exactly one", requests)
			}
		})
	}
}

func TestCreateIndexErrors(t *testing.T) {
	t.Run("transport error", func(t *testing.T) {
		cause := errors.New("transport-secret")
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, cause
		})}
		index, err := elasticsearch.NewIndex("http://search.example", "indexer", "password-secret", "xflickr_", client)
		if err != nil {
			t.Fatal(err)
		}
		err = index.Create(t.Context())
		if !errors.Is(err, cause) {
			t.Fatalf("Create() error = %v, want wrapped transport error", err)
		}
		assertErrorOmits(t, err, "password-secret", "transport-secret")
	})

	t.Run("canceled context", func(t *testing.T) {
		var requests int
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
		t.Cleanup(server.Close)
		index, err := elasticsearch.NewIndex(server.URL, "indexer", "password-secret", "xflickr_", server.Client())
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		err = index.Create(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Create() error = %v, want context.Canceled", err)
		}
		if requests != 0 {
			t.Fatalf("HTTP requests = %d, want 0 for canceled context", requests)
		}
		assertErrorOmits(t, err, "password-secret")
	})

	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict, http.StatusInternalServerError} {
		t.Run(fmt.Sprintf("status %d", status), func(t *testing.T) {
			var methods []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				methods = append(methods, r.Method)
				w.WriteHeader(status)
				_, _ = w.Write([]byte("response-body-secret"))
			}))
			t.Cleanup(server.Close)
			index, err := elasticsearch.NewIndex(server.URL, "indexer", "password-secret", "xflickr_", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			err = index.Create(t.Context())
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%d", status)) {
				t.Fatalf("Create() error = %v, want status %d", err, status)
			}
			assertErrorOmits(t, err, "password-secret", "response-body-secret")
			if !reflect.DeepEqual(methods, []string{http.MethodPut}) {
				t.Fatalf("HTTP methods = %v, want one PUT and no index mutation follow-up", methods)
			}
		})
	}
}

func TestUpsertDocument(t *testing.T) {
	const basePath = "/search-gateway/v1"
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPut || r.URL.Path != basePath+"/xflickr_wtjooxf134_photos/_doc/42" || r.URL.RawQuery != "" {
			t.Errorf("request = %s %s?%s, want PUT to the write alias without a query", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		if user, password, ok := r.BasicAuth(); !ok || user != "indexer" || password != "password" {
			t.Errorf("basic authentication = (%q, %q, %t), want configured credentials", user, password, ok)
		}
		var source map[string]any
		if err := json.NewDecoder(r.Body).Decode(&source); err != nil {
			t.Errorf("decode source: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		embedding := make([]any, 1024)
		for i := range embedding {
			embedding[i] = float64(0)
		}
		embedding[0], embedding[1023] = float64(0.25), float64(1.25)
		want := map[string]any{
			"title": "Old title", "description": "A photo description", "tags": []any{"dog", "tree"},
			"caption": "A generated caption", "embedding": embedding, "owner": "owner-7", "kind": "photo",
			"sizes_status": "ready", "ai_status": "complete", "uploaded_at": "2026-11-11T12:13:14.123456789Z",
			"visible_to": []any{"7", "11"}, "visibility": "private", "publicly_shared": true,
		}
		if !reflect.DeepEqual(source, want) {
			t.Error("source does not match the complete mapped photo payload")
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	index := newTestIndex(t, server.URL+basePath, "indexer", "password", "xflickr_wtjooxf134_", server.Client())
	if err := index.Upsert(t.Context(), testDocument()); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("HTTP requests = %d, want exactly one", requests)
	}

	for _, status := range []int{http.StatusNoContent, 299} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
			})}
			index := newTestIndex(t, "http://search.example", "", "", "xflickr_", client)
			if err := index.Upsert(t.Context(), app.Document{ID: 42}); err != nil {
				t.Fatalf("Upsert() error = %v", err)
			}
		})
	}

	t.Run("omits optional values and retains nil slices", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var source map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&source); err != nil {
				t.Errorf("decode source: %v", err)
				return
			}
			if _, exists := source["embedding"]; exists {
				t.Error("empty embedding is present in source")
			}
			if _, exists := source["uploaded_at"]; exists {
				t.Error("nil uploaded_at is present in source")
			}
			if string(source["tags"]) != "null" || string(source["visible_to"]) != "null" {
				t.Errorf("nil slices = tags %s, visible_to %s; want JSON null", source["tags"], source["visible_to"])
			}
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(server.Close)
		index := newTestIndex(t, server.URL, "", "", "xflickr_", server.Client())
		if err := index.Upsert(t.Context(), app.Document{ID: 42}); err != nil {
			t.Fatalf("Upsert() error = %v", err)
		}
	})
}

func TestUpsertInvalidDocument(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	t.Cleanup(server.Close)
	index := newTestIndex(t, server.URL, "", "", "xflickr_", server.Client())

	for _, test := range []struct {
		name         string
		document     app.Document
		wantSentinel bool
	}{
		{name: "zero ID", document: app.Document{}, wantSentinel: true},
		{name: "negative ID", document: app.Document{ID: -1}, wantSentinel: true},
		{name: "short embedding", document: app.Document{ID: 1, Embedding: []float32{0.5}}, wantSentinel: true},
		{name: "long embedding", document: app.Document{ID: 1, Embedding: make([]float32, 1025)}, wantSentinel: true},
		{name: "NaN embedding", document: app.Document{ID: 1, Embedding: invalidEmbedding(float32(math.NaN()))}},
		{name: "infinite embedding", document: app.Document{ID: 1, Embedding: invalidEmbedding(float32(math.Inf(1)))}},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := requests
			err := index.Upsert(t.Context(), test.document)
			if err == nil {
				t.Fatal("Upsert() error = nil, want an error")
			}
			if test.wantSentinel && !errors.Is(err, app.ErrInvalidDocument) {
				t.Fatalf("Upsert() error = %v, want ErrInvalidDocument", err)
			}
			if requests != before {
				t.Fatalf("HTTP requests = %d, want no request for invalid document", requests-before)
			}
		})
	}
}

func TestUpsertFailures(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
	}{
		{name: "redirect is not followed", status: http.StatusTemporaryRedirect},
		{name: "gateway failure", status: http.StatusBadGateway},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests int
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				requests++
				header := make(http.Header)
				if test.status == http.StatusTemporaryRedirect {
					header.Set("Location", "/redirected")
				}
				return &http.Response{StatusCode: test.status, Header: header, Body: io.NopCloser(strings.NewReader("response-body-secret"))}, nil
			})}
			index := newTestIndex(t, "http://search.example", "indexer", "password-secret", "xflickr_", client)
			err := index.Upsert(t.Context(), testDocument())
			if err == nil || !strings.Contains(err.Error(), fmt.Sprint(test.status)) {
				t.Fatalf("Upsert() error = %v, want status %d", err, test.status)
			}
			assertErrorOmits(t, err, "password-secret", "response-body-secret")
			if requests != 1 {
				t.Fatalf("HTTP requests = %d, want one", requests)
			}
		})
	}

	t.Run("transport error is sanitized and unwraps its cause", func(t *testing.T) {
		cause := errors.New("transport-secret")
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, cause })}
		index := newTestIndex(t, "http://search.example", "indexer", "password-secret", "xflickr_", client)
		err := index.Upsert(t.Context(), testDocument())
		if !errors.Is(err, cause) {
			t.Fatalf("Upsert() error = %v, want wrapped transport error", err)
		}
		assertErrorOmits(t, err, "password-secret", "transport-secret")
	})

	t.Run("canceled context is preserved without a request", func(t *testing.T) {
		var requests int
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
		t.Cleanup(server.Close)
		index := newTestIndex(t, server.URL, "indexer", "password-secret", "xflickr_", server.Client())
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		err := index.Upsert(ctx, testDocument())
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Upsert() error = %v, want context.Canceled", err)
		}
		if requests != 0 {
			t.Fatalf("HTTP requests = %d, want 0 for canceled context", requests)
		}
	})

	t.Run("closes response body", func(t *testing.T) {
		body := &closeTrackingBody{Reader: strings.NewReader("")}
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: body}, nil
		})}
		index := newTestIndex(t, "http://search.example", "", "", "xflickr_", client)
		if err := index.Upsert(t.Context(), testDocument()); err != nil {
			t.Fatalf("Upsert() error = %v", err)
		}
		if !body.closed {
			t.Error("response body was not closed")
		}
	})
}

func testDocument() app.Document {
	uploadedAt := time.Date(2026, time.November, 11, 12, 13, 14, 123456789, time.UTC)
	embedding := make([]float32, 1024)
	embedding[0] = 0.25
	embedding[1023] = 1.25
	return app.Document{
		ID: 42, Title: "Old title", Description: "A photo description", Tags: []string{"dog", "tree"},
		Caption: "A generated caption", Embedding: embedding, Owner: "owner-7", Kind: "photo",
		SizesStatus: "ready", AIStatus: "complete", UploadedAt: &uploadedAt,
		VisibleTo: []string{"7", "11"}, Visibility: "private", PubliclyShared: true,
	}
}

func invalidEmbedding(first float32) []float32 {
	embedding := make([]float32, 1024)
	embedding[0] = first
	return embedding
}

type closeTrackingBody struct {
	io.Reader
	closed bool
}

func (body *closeTrackingBody) Close() error {
	body.closed = true
	return nil
}

func newTestIndex(t *testing.T, baseURL, username, password, prefix string, client *http.Client) *elasticsearch.Index {
	t.Helper()
	index, err := elasticsearch.NewIndex(baseURL, username, password, prefix, client)
	if err != nil {
		t.Fatalf("NewIndex() error = %v", err)
	}
	return index
}

func expectedIndexDefinition(alias string) map[string]any {
	text := map[string]any{"type": "text", "analyzer": "standard"}
	properties := map[string]any{
		"title":       text,
		"description": text,
		"tags":        text,
		"caption":     text,
		"embedding": map[string]any{
			"type": "dense_vector", "dims": float64(1024), "index": true,
			"similarity": "cosine", "index_options": map[string]any{"type": "hnsw"},
		},
		"owner":           map[string]any{"type": "keyword"},
		"kind":            map[string]any{"type": "keyword"},
		"sizes_status":    map[string]any{"type": "keyword"},
		"ai_status":       map[string]any{"type": "keyword"},
		"uploaded_at":     map[string]any{"type": "date"},
		"visible_to":      map[string]any{"type": "keyword"},
		"visibility":      map[string]any{"type": "keyword"},
		"publicly_shared": map[string]any{"type": "boolean"},
	}
	return map[string]any{
		"mappings": map[string]any{"dynamic": "strict", "properties": properties},
		"aliases":  map[string]any{alias: map[string]any{"is_write_index": true}},
	}
}

func assertErrorOmits(t *testing.T, err error, secrets ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want an error")
	}
	for _, secret := range secrets {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error exposed %q", secret)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
