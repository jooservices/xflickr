// Package dropbox checks Dropbox file metadata for cloud sync.
package dropbox

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/jooservices/xflickr/internal/sync/app"
)

const (
	metadataEndpoint     = "https://api.dropboxapi.com/2/files/get_metadata"
	maxResponseBodyBytes = 1 << 20
)

// Client verifies Dropbox content hashes at known App-folder paths.
type Client struct {
	httpClient  *http.Client
	accessToken string
}

// New creates a Dropbox metadata client with an injected HTTP client and token.
func New(httpClient *http.Client, accessToken string) (*Client, error) {
	if httpClient == nil {
		return nil, errors.New("dropbox: HTTP client is required")
	}
	if strings.TrimSpace(accessToken) == "" {
		return nil, errors.New("dropbox: access token is required")
	}
	return &Client{httpClient: httpClient, accessToken: accessToken}, nil
}

// CheckContent reports whether a Dropbox file at path has expectedHash.
func (c *Client) CheckContent(ctx context.Context, path, expectedHash string) (bool, error) {
	if ctx == nil {
		return false, errors.New("dropbox: context is required")
	}
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("dropbox: context is canceled: %w", err)
	}
	if c == nil || c.httpClient == nil || strings.TrimSpace(c.accessToken) == "" {
		return false, errors.New("dropbox: client is not initialized")
	}
	if err := validatePath(path); err != nil {
		return false, err
	}
	wantHash, err := hex.DecodeString(expectedHash)
	if err != nil || len(wantHash) != 32 {
		return false, errors.New("dropbox: expected content hash must be 32-byte hexadecimal")
	}

	requestBody, err := json.Marshal(struct {
		Path           string `json:"path"`
		IncludeDeleted bool   `json:"include_deleted"`
	}{Path: path})
	if err != nil {
		return false, errors.New("dropbox: encode metadata request")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, metadataEndpoint, bytes.NewReader(requestBody))
	if err != nil {
		return false, errors.New("dropbox: create metadata request")
	}
	request.Header.Set("Authorization", "Bearer "+c.accessToken)
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return false, fmt.Errorf("dropbox: metadata request canceled: %w", contextErr)
		}
		return false, errors.New("dropbox: send metadata request")
	}
	if response == nil {
		return false, errors.New("dropbox: metadata request returned no response")
	}
	responseBody, err := readResponseBody(response.Body)
	if err != nil {
		return false, err
	}

	switch response.StatusCode {
	case http.StatusOK:
		return matchesContentHash(responseBody, wantHash)
	case http.StatusConflict:
		return pathNotFound(responseBody)
	case http.StatusUnauthorized:
		return false, fmt.Errorf("dropbox: metadata request authentication failed: %w", app.ErrAuthentication)
	case http.StatusTooManyRequests:
		return false, fmt.Errorf("dropbox: metadata request rate limited: %w", app.ErrRateLimited)
	default:
		return false, errors.New("dropbox: metadata request failed")
	}
}

func validatePath(path string) error {
	if len(path) < 2 || path[0] != '/' {
		return errors.New("dropbox: path must be an absolute App-folder path")
	}
	if strings.ContainsRune(path, '\x00') {
		return errors.New("dropbox: path contains a NUL byte")
	}
	for _, component := range strings.Split(path[1:], "/") {
		if component == "." || component == ".." {
			return errors.New("dropbox: path contains a dot component")
		}
	}
	return nil
}

func readResponseBody(body io.ReadCloser) ([]byte, error) {
	if body == nil {
		return nil, errors.New("dropbox: metadata response has no body")
	}
	content, readErr := io.ReadAll(io.LimitReader(body, maxResponseBodyBytes+1))
	closeErr := body.Close()
	if readErr != nil {
		return nil, errors.New("dropbox: read metadata response")
	}
	if closeErr != nil {
		return nil, errors.New("dropbox: close metadata response")
	}
	if len(content) > maxResponseBodyBytes {
		return nil, errors.New("dropbox: metadata response exceeds 1 MiB")
	}
	return content, nil
}

func matchesContentHash(content []byte, expected []byte) (bool, error) {
	var metadata struct {
		Tag         string `json:".tag"`
		ContentHash string `json:"content_hash"`
	}
	if err := json.Unmarshal(content, &metadata); err != nil {
		return false, errors.New("dropbox: decode metadata response")
	}
	if metadata.Tag != "file" {
		return false, errors.New("dropbox: metadata response is not a file")
	}
	actual, err := hex.DecodeString(metadata.ContentHash)
	if err != nil || len(actual) != 32 {
		return false, errors.New("dropbox: metadata content hash is invalid")
	}
	return bytes.Equal(actual, expected), nil
}

func pathNotFound(content []byte) (bool, error) {
	var response struct {
		Error struct {
			Tag  string `json:".tag"`
			Path struct {
				Tag string `json:".tag"`
			} `json:"path"`
		} `json:"error"`
	}
	if err := json.Unmarshal(content, &response); err != nil {
		return false, errors.New("dropbox: decode error response")
	}
	if response.Error.Tag == "path" && response.Error.Path.Tag == "not_found" {
		return false, nil
	}
	return false, errors.New("dropbox: metadata request failed with HTTP 409")
}
