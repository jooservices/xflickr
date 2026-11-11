package onedrive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jooservices/xflickr/internal/sync/app"
)

const testToken = "fake-access-token"

func TestEnsureAlbumCreatesFolder(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.EscapedPath())
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		switch len(calls) {
		case 1:
			if r.Method != http.MethodGet || r.URL.Path != "/v1.0/me/drive/special/approot" {
				t.Errorf("App Folder request = %s %s", r.Method, r.URL.Path)
			}
			writeJSON(t, w, http.StatusOK, `{"id":"root /1","folder":{}}`)
		case 2:
			if r.Method != http.MethodGet || r.URL.EscapedPath() != "/v1.0/me/drive/items/root%20%2F1:/Album%20%23%20%E6%9D%B1%E4%BA%AC" {
				t.Errorf("lookup request = %s %s", r.Method, r.URL.EscapedPath())
			}
			w.WriteHeader(http.StatusNotFound)
		case 3:
			if r.Method != http.MethodPost || r.URL.Path != "/v1.0/me/drive/items/root /1/children" {
				t.Errorf("create request = %s %s", r.Method, r.URL.Path)
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
			}
			body, err := io.ReadAll(r.Body)
			if err != nil || string(body) != `{"name":"Album # 東京","folder":{},"@microsoft.graph.conflictBehavior":"fail"}` {
				t.Errorf("create body = %s, read error = %v", body, err)
			}
			writeJSON(t, w, http.StatusCreated, `{"id":"album-1","folder":{}}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	client := mustClient(t, server.Client(), server.URL+"/v1.0", testToken)
	got, err := client.EnsureAlbum(t.Context(), "  Album # 東京  ")
	if err != nil || got != "album-1" {
		t.Fatalf("EnsureAlbum() = %q, %v; want album-1", got, err)
	}
	if len(calls) != 3 {
		t.Fatalf("request sequence = %v", calls)
	}
}

func TestEnsureAlbumReturnsExistingFolder(t *testing.T) {
	var roots, posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1.0/me/drive/special/approot" {
			roots.Add(1)
			writeJSON(t, w, http.StatusOK, `{"id":"root-1","folder":{}}`)
			return
		}
		writeJSON(t, w, http.StatusOK, `{"id":"album-1","folder":{}}`)
		if r.Method == http.MethodPost {
			posts.Add(1)
		}
	}))
	t.Cleanup(server.Close)
	client := mustClient(t, server.Client(), server.URL+"/v1.0", testToken)
	first, err := client.EnsureAlbum(t.Context(), " family ")
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.EnsureAlbum(t.Context(), "family")
	if err != nil || second != first || second != "album-1" {
		t.Fatalf("second EnsureAlbum() = %q, %v; want %q", second, err, first)
	}
	if roots.Load() != 2 || posts.Load() != 0 {
		t.Fatalf("App Folder GET count = %d, POST count = %d; want 2 and 0", roots.Load(), posts.Load())
	}
}

func TestEnsureAlbumResolvesCreateConflict(t *testing.T) {
	var lookups, creates atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1.0/me/drive/special/approot":
			writeJSON(t, w, http.StatusOK, `{"id":"root-1","folder":{}}`)
		case r.Method == http.MethodGet:
			if lookups.Add(1) == 1 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			writeJSON(t, w, http.StatusOK, `{"id":"winner","folder":{}}`)
		case r.Method == http.MethodPost:
			creates.Add(1)
			w.WriteHeader(http.StatusConflict)
		}
	}))
	t.Cleanup(server.Close)
	client := mustClient(t, server.Client(), server.URL+"/v1.0", testToken)
	got, err := client.EnsureAlbum(t.Context(), "family")
	if err != nil || got != "winner" || lookups.Load() != 2 || creates.Load() != 1 {
		t.Fatalf("EnsureAlbum() = %q, %v; lookups=%d creates=%d", got, err, lookups.Load(), creates.Load())
	}
}

func TestEnsureAlbumRejectsUnresolvedCreateConflict(t *testing.T) {
	for _, response := range []string{"", `{"id":"file-1"}`} {
		var lookups atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/me/drive/special/approot":
				writeJSON(t, w, http.StatusOK, `{"id":"root-1","folder":{}}`)
			case r.Method == http.MethodGet:
				if lookups.Add(1) == 1 {
					w.WriteHeader(http.StatusNotFound)
				} else if response == "" {
					w.WriteHeader(http.StatusNotFound)
				} else {
					writeJSON(t, w, http.StatusOK, response)
				}
			case r.Method == http.MethodPost:
				w.WriteHeader(http.StatusConflict)
			}
		}))
		t.Cleanup(server.Close)
		client := mustClient(t, server.Client(), server.URL, testToken)
		_, err := client.EnsureAlbum(t.Context(), "album")
		if err == nil || !strings.Contains(err.Error(), "status") {
			t.Fatalf("unresolved conflict error = %v", err)
		}
	}
}

func TestEnsureAlbumRejectsInvalidNames(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	t.Cleanup(server.Close)
	client := mustClient(t, server.Client(), server.URL, testToken)
	for _, name := range []string{"", " \t ", ".", "..", "bad/name", `bad\name`, "bad*name", "bad<name", "bad>name", "bad?name", "bad:name", "bad|name", `bad"name`, "bad\nname", "trailing."} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			if _, err := client.EnsureAlbum(t.Context(), name); !errors.Is(err, app.ErrInvalidAlbum) {
				t.Fatalf("EnsureAlbum() error = %v, want ErrInvalidAlbum", err)
			}
		})
	}
	if requests.Load() != 0 {
		t.Fatalf("invalid names sent %d HTTP requests", requests.Load())
	}
}

func TestEnsureAlbumMapsErrors(t *testing.T) {
	for _, test := range []struct {
		name, stage, response string
		status                int
		want                  error
	}{
		{name: "unauthorized", stage: "root", status: http.StatusUnauthorized, response: testToken, want: app.ErrAuthentication},
		{name: "rate limited", stage: "lookup", status: http.StatusTooManyRequests, response: testToken, want: app.ErrRateLimited},
		{name: "forbidden", stage: "create", status: http.StatusForbidden, response: testToken},
		{name: "bad JSON", stage: "root", status: http.StatusOK, response: "not json"},
		{name: "missing ID", stage: "root", status: http.StatusOK, response: `{"folder":{}}`},
		{name: "missing folder", stage: "root", status: http.StatusOK, response: `{"id":"id"}`},
		{name: "item is a file", stage: "lookup", status: http.StatusOK, response: `{"id":"file-1"}`},
		{name: "created item is a file", stage: "create", status: http.StatusCreated, response: `{"id":"file-1"}`},
		{name: "oversized JSON", stage: "root", status: http.StatusOK, response: `{"id":"` + strings.Repeat("x", 64*1024) + `","folder":{}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/me/drive/special/approot" {
					if test.stage == "root" {
						respond(t, w, test.status, test.response)
					} else {
						writeJSON(t, w, http.StatusOK, `{"id":"root-1","folder":{}}`)
					}
				} else if r.Method == http.MethodGet && test.stage == "lookup" {
					respond(t, w, test.status, test.response)
				} else if r.Method == http.MethodPost && test.stage == "create" {
					respond(t, w, test.status, test.response)
				} else {
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			t.Cleanup(server.Close)
			client := mustClient(t, server.Client(), server.URL, testToken)
			_, err := client.EnsureAlbum(t.Context(), "album")
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("EnsureAlbum() error = %v, want %v", err, test.want)
			}
			if test.name == "forbidden" && errors.Is(err, app.ErrAuthentication) {
				t.Fatalf("403 error was incorrectly classified as authentication: %v", err)
			}
			if err == nil || strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), test.response) {
				t.Fatalf("unsafe or missing error: %v", err)
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("status %d", test.status)) {
				t.Fatalf("error %v does not include status %d", err, test.status)
			}
		})
	}
}

func TestEnsureAlbumHonorsCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	client := mustClient(t, server.Client(), server.URL, testToken)
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { _, err := client.EnsureAlbum(ctx, "album"); result <- err }()
	<-started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("EnsureAlbum() error = %v, want context.Canceled", err)
	}
}

func TestNewRejectsInvalidDependencies(t *testing.T) {
	for _, test := range []struct {
		client         *http.Client
		baseURL, token string
	}{
		{client: nil, baseURL: "https://graph.microsoft.com/v1.0", token: testToken},
		{client: http.DefaultClient, baseURL: "not a URL", token: testToken},
		{client: http.DefaultClient, baseURL: "ftp://graph.microsoft.com", token: testToken},
		{client: http.DefaultClient, baseURL: "https://graph.microsoft.com?query=1", token: testToken},
		{client: http.DefaultClient, baseURL: "https://user@graph.microsoft.com", token: testToken},
		{client: http.DefaultClient, baseURL: "https://graph.microsoft.com", token: " "},
	} {
		if _, err := New(test.client, test.baseURL, test.token); err == nil {
			t.Fatalf("New(%v, %q, %q) unexpectedly succeeded", test.client, test.baseURL, test.token)
		}
	}
}

func TestEnsureAlbumDoesNotFollowRedirects(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Redirect(w, nil, "/redirect-target", http.StatusFound)
	}))
	t.Cleanup(server.Close)
	client := mustClient(t, server.Client(), server.URL, testToken)
	if _, err := client.EnsureAlbum(t.Context(), "album"); err == nil || requests.Load() != 1 {
		t.Fatalf("EnsureAlbum() error = %v, request count = %d", err, requests.Load())
	}
}

func mustClient(t *testing.T, client *http.Client, baseURL, token string) *Client {
	t.Helper()
	got, err := New(client, baseURL, token)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write([]byte(body)); err != nil {
		t.Errorf("write response: %v", err)
	}
}

func respond(t *testing.T, w http.ResponseWriter, status int, body string) {
	t.Helper()
	w.WriteHeader(status)
	if _, err := w.Write([]byte(body)); err != nil {
		t.Errorf("write response: %v", err)
	}
}
