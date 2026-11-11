// Package elasticsearch creates the versioned XFlickr photo index.
package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/jooservices/xflickr/internal/search/app"
)

var _ app.Upserter = (*Index)(nil)

// Index creates the versioned photo index and its write alias.
type Index struct {
	baseURL  string
	username string
	password string
	prefix   string
	client   *http.Client
}

// NewIndex validates the Elasticsearch endpoint, worktree prefix, and client.
func NewIndex(baseURL, username, password, prefix string, client *http.Client) (*Index, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || !parsed.IsAbs() || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" || containsFragmentDelimiter(baseURL) {
		return nil, errors.New("elasticsearch: invalid base URL")
	}
	if client == nil {
		return nil, errors.New("elasticsearch: HTTP client is required")
	}
	if !validPrefix(prefix) {
		return nil, errors.New("elasticsearch: invalid index prefix")
	}
	if len(prefix)+len("photos-v1") > 255 {
		return nil, errors.New("elasticsearch: physical index name exceeds 255 bytes")
	}

	endpoint := strings.TrimRight(parsed.String(), "/")
	return &Index{
		baseURL: endpoint, username: username, password: password,
		prefix: prefix, client: client,
	}, nil
}

// Create creates the physical index and write alias with one Elasticsearch PUT.
func (i *Index) Create(ctx context.Context) error {
	body, err := json.Marshal(indexDefinition(i.prefix))
	if err != nil {
		return fmt.Errorf("elasticsearch: encode index mapping: %w", err)
	}

	target := i.baseURL + "/" + i.prefix + "photos-v1"
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, target, bytes.NewReader(body))
	if err != nil {
		return requestError{operation: "create index", cause: err}
	}
	request.Header.Set("Content-Type", "application/json")
	if i.username != "" {
		request.SetBasicAuth(i.username, i.password)
	}

	response, err := i.client.Do(request)
	if err != nil {
		return requestError{operation: "create index", cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("elasticsearch: create index returned HTTP status %d", response.StatusCode)
	}
	return nil
}

// Upsert writes or replaces one photo document through the configured write alias.
func (i *Index) Upsert(ctx context.Context, doc app.Document) error {
	if doc.ID <= 0 || (len(doc.Embedding) != 0 && len(doc.Embedding) != 1024) {
		return fmt.Errorf("%w: ID must be positive and embedding must be empty or 1024-dimensional", app.ErrInvalidDocument)
	}

	body, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("elasticsearch: encode document: %w", err)
	}

	target := i.baseURL + "/" + i.prefix + "photos/_doc/" +
		strconv.FormatInt(doc.ID, 10) + "?require_alias=true"
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, target, bytes.NewReader(body))
	if err != nil {
		return requestError{operation: "upsert document", cause: err}
	}
	request.Header.Set("Content-Type", "application/json")
	if i.username != "" {
		request.SetBasicAuth(i.username, i.password)
	}

	client := *i.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	response, err := client.Do(request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return requestError{operation: "upsert document", cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("elasticsearch: upsert document returned HTTP status %d", response.StatusCode)
	}
	return nil
}

func indexDefinition(prefix string) map[string]any {
	text := map[string]any{"type": "text", "analyzer": "standard"}
	return map[string]any{
		"mappings": map[string]any{
			"dynamic": "strict",
			"properties": map[string]any{
				"title":       text,
				"description": text,
				"tags":        text,
				"caption":     text,
				"embedding": map[string]any{
					"type": "dense_vector", "dims": 1024, "index": true,
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
			},
		},
		"aliases": map[string]any{
			prefix + "photos": map[string]any{"is_write_index": true},
		},
	}
}

func validPrefix(prefix string) bool {
	if len(prefix) < 2 || prefix[len(prefix)-1] != '_' || !lowerAlphaNumeric(prefix[0]) {
		return false
	}
	for _, character := range []byte(prefix[:len(prefix)-1]) {
		if !lowerAlphaNumeric(character) && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func lowerAlphaNumeric(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= '0' && character <= '9'
}

func containsFragmentDelimiter(value string) bool {
	for i := range value {
		if value[i] == '#' {
			return true
		}
	}
	return false
}

type requestError struct {
	operation string
	cause     error
}

func (e requestError) Error() string { return "elasticsearch: " + e.operation + " request failed" }

func (e requestError) Unwrap() error { return e.cause }
