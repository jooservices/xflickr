package gdrive

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/sync/app"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

func TestEnsureAlbumFindsExistingFolder(t *testing.T) {
	client := newFolderClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/files" {
			t.Errorf("request = %s %s; want GET /files", r.Method, r.URL.Path)
		}
		if got, want := r.URL.Query().Get("q"), "'root' in parents and mimeType = 'application/vnd.google-apps.folder' and trashed = false and name = 'family'"; got != want {
			t.Errorf("q = %q; want %q", got, want)
		}
		if got := r.URL.Query().Get("pageSize"); got != "1" {
			t.Errorf("pageSize = %q; want 1", got)
		}
		if got := r.URL.Query().Get("fields"); got != "files(id),nextPageToken" {
			t.Errorf("fields = %q; want files(id),nextPageToken", got)
		}
		writeJSON(t, w, map[string]any{"files": []map[string]string{{"id": "folder-existing"}}})
	}))

	id, err := client.EnsureAlbum(t.Context(), " family ")
	if err != nil || id != "folder-existing" {
		t.Fatalf("EnsureAlbum() = %q, %v; want folder-existing, nil", id, err)
	}
}

func TestEnsureAlbumFindsFolderAfterEmptyPage(t *testing.T) {
	requests := 0
	client := newFolderClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("request = %s %s; want another list page", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusMethodNotAllowed)
			return
		}
		requests++
		if got, want := r.URL.Query().Get("fields"), "files(id),nextPageToken"; got != want {
			t.Errorf("fields = %q; want %q", got, want)
		}
		if got := r.URL.Query().Get("pageSize"); got != "1" {
			t.Errorf("pageSize = %q; want 1", got)
		}
		switch token := r.URL.Query().Get("pageToken"); token {
		case "":
			writeJSON(t, w, map[string]any{"files": []any{}, "nextPageToken": "page-2"})
		case "page-2":
			writeJSON(t, w, map[string]any{"files": []map[string]string{{"id": "folder-later-page"}}})
		default:
			t.Errorf("pageToken = %q; want empty token or page-2", token)
			http.Error(w, "unexpected page token", http.StatusBadRequest)
		}
	}))

	id, err := client.EnsureAlbum(t.Context(), "family")
	if err != nil || id != "folder-later-page" {
		t.Fatalf("EnsureAlbum() = %q, %v; want folder-later-page, nil", id, err)
	}
	if requests != 2 {
		t.Fatalf("list request count = %d; want 2", requests)
	}
}

func TestEnsureAlbumCreatesThenReusesFolder(t *testing.T) {
	folders := make(map[string]string)
	createCount := 0
	client := newFolderClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if got := r.URL.Query().Get("pageSize"); got != "1" {
				t.Errorf("pageSize = %q; want 1", got)
			}
			name := queryName(t, r.URL.Query().Get("q"))
			files := []map[string]string{}
			if id, ok := folders[name]; ok {
				files = append(files, map[string]string{"id": id})
			}
			writeJSON(t, w, map[string]any{"files": files})
		case http.MethodPost:
			var got drive.File
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Errorf("decode create request: %v", err)
				return
			}
			if got.Name != "family" || got.MimeType != "application/vnd.google-apps.folder" || strings.Join(got.Parents, ",") != "root" {
				t.Errorf("create metadata = %+v; want normalized root folder", got)
			}
			if r.URL.Query().Get("fields") != "id" {
				t.Errorf("create fields = %q; want id", r.URL.Query().Get("fields"))
			}
			createCount++
			folders[got.Name] = "folder-created"
			writeJSON(t, w, map[string]string{"id": "folder-created"})
		default:
			t.Errorf("unexpected method %s", r.Method)
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))

	first, err := client.EnsureAlbum(t.Context(), "  family  ")
	if err != nil || first != "folder-created" {
		t.Fatalf("first EnsureAlbum() = %q, %v", first, err)
	}
	second, err := client.EnsureAlbum(t.Context(), "family")
	if err != nil || second != first {
		t.Fatalf("second EnsureAlbum() = %q, %v; want %q, nil", second, err, first)
	}
	if createCount != 1 {
		t.Fatalf("create count = %d; want 1", createCount)
	}
}

func TestEnsureAlbumEscapesAndValidatesName(t *testing.T) {
	requests := 0
	client := newFolderClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method == http.MethodGet {
			if got, want := r.URL.Query().Get("q"), "'root' in parents and mimeType = 'application/vnd.google-apps.folder' and trashed = false and name = 'O\\'Brien\\\\Archive'"; got != want {
				t.Errorf("q = %q; want escaped query %q", got, want)
			}
			writeJSON(t, w, map[string]any{"files": []any{}})
			return
		}
		var got drive.File
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode create request: %v", err)
			return
		}
		if got.Name != "O'Brien\\Archive" {
			t.Errorf("created name = %q; want trimmed name", got.Name)
		}
		writeJSON(t, w, map[string]string{"id": "folder-escaped"})
	}))

	id, err := client.EnsureAlbum(t.Context(), "  O'Brien\\Archive  ")
	if err != nil || id != "folder-escaped" {
		t.Fatalf("EnsureAlbum(escaped name) = %q, %v", id, err)
	}
	if requests != 2 {
		t.Fatalf("request count = %d; want list and create", requests)
	}

	if _, err := client.EnsureAlbum(t.Context(), " \t\n "); !errors.Is(err, app.ErrInvalidAlbum) {
		t.Fatalf("EnsureAlbum(blank) error = %v; want ErrInvalidAlbum", err)
	}
	if requests != 2 {
		t.Fatalf("blank name sent a request; count = %d", requests)
	}
}

func TestEnsureAlbumMapsFailures(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		want       error
		wantStatus string
	}{
		{name: "authentication", status: http.StatusUnauthorized, want: app.ErrAuthentication},
		{name: "rate limited", status: http.StatusTooManyRequests, want: app.ErrRateLimited},
		{name: "other response is safe", status: http.StatusInternalServerError, wantStatus: "500"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newFolderClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"error":{"message":"private response-secret-token"}}`, tt.status)
			}))
			_, err := client.EnsureAlbum(t.Context(), "family")
			if err == nil || !strings.HasPrefix(err.Error(), "gdrive:") {
				t.Fatalf("EnsureAlbum() error = %v; want sanitized gdrive error", err)
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Errorf("EnsureAlbum() error = %v; want %v", err, tt.want)
			}
			if strings.Contains(err.Error(), "response-secret-token") || strings.Contains(err.Error(), "private response") {
				t.Errorf("error leaked Drive response: %v", err)
			}
			if tt.wantStatus != "" && !strings.Contains(err.Error(), tt.wantStatus) {
				t.Errorf("error = %v; want status %s", err, tt.wantStatus)
			}
		})
	}
}

func TestEnsureAlbumRejectsEmptyIDs(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "existing folder",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("method = %s; empty listed ID must not trigger create", r.Method)
				}
				writeJSON(t, w, map[string]any{"files": []map[string]string{{"id": ""}}})
			},
		},
		{
			name: "created folder",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					writeJSON(t, w, map[string]any{"files": []any{}})
					return
				}
				writeJSON(t, w, map[string]string{"id": ""})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newFolderClient(t, tt.handler)
			if id, err := client.EnsureAlbum(t.Context(), "family"); err == nil || id.Valid() || !strings.HasPrefix(err.Error(), "gdrive:") {
				t.Fatalf("EnsureAlbum() = %q, %v; want a prefixed error", id, err)
			}
		})
	}
}

func TestEnsureAlbumHonorsContext(t *testing.T) {
	requests := 0
	client := newFolderClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	if _, err := client.EnsureAlbum(nil, "family"); err == nil || !strings.HasPrefix(err.Error(), "gdrive:") {
		t.Fatalf("EnsureAlbum(nil, ...) error = %v; want explicit error", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.EnsureAlbum(ctx, "family"); !errors.Is(err, context.Canceled) {
		t.Fatalf("EnsureAlbum(canceled, ...) error = %v; want context.Canceled", err)
	}
	if requests != 0 {
		t.Fatalf("invalid context sent %d requests", requests)
	}
}

func TestEnsureAlbumCancelsInFlightRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	client := newFolderClient(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() { close(release) })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		_, err := client.EnsureAlbum(ctx, "family")
		errCh <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("Drive request did not reach the fake server")
	}
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("EnsureAlbum() error = %v; want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("EnsureAlbum() did not stop after context cancellation")
	}
}

func TestNewFolderClientRejectsNil(t *testing.T) {
	if client, err := NewFolderClient(nil); client != nil || err == nil || !strings.HasPrefix(err.Error(), "gdrive:") {
		t.Fatalf("NewFolderClient(nil) = %v, %v; want nil client and prefixed error", client, err)
	}
}

func newFolderClient(t *testing.T, handler http.Handler) *FolderClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	service, err := drive.NewService(t.Context(),
		option.WithEndpoint(server.URL+"/"),
		option.WithHTTPClient(server.Client()),
		option.WithoutAuthentication(),
	)
	if err != nil {
		t.Fatalf("create Drive service: %v", err)
	}
	client, err := NewFolderClient(service)
	if err != nil {
		t.Fatalf("create folder client: %v", err)
	}
	return client
}

func queryName(t *testing.T, query string) string {
	t.Helper()
	const prefix = "'root' in parents and mimeType = 'application/vnd.google-apps.folder' and trashed = false and name = '"
	if !strings.HasPrefix(query, prefix) || !strings.HasSuffix(query, "'") {
		t.Fatalf("unexpected folder query %q", query)
	}
	return strings.TrimSuffix(strings.TrimPrefix(query, prefix), "'")
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode response: %v", err)
	}
}
