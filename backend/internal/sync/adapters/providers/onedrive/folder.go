// Package onedrive implements the OneDrive App Folder adapter.
package onedrive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode"

	"github.com/jooservices/xflickr/internal/sync/app"
)

const maxGraphResponseBytes = 64 * 1024

// Client creates and finds folders in the signed-in user's OneDrive App Folder.
type Client struct {
	httpClient  *http.Client
	baseURL     string
	accessToken string
}

// New validates the HTTP client, Graph base URL, and delegated access token.
// The base URL must be https: the bearer token is never sent over an unencrypted connection.
func New(client *http.Client, baseURL, accessToken string) (*Client, error) {
	if client == nil {
		return nil, fmt.Errorf("onedrive: HTTP client is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" ||
		strings.Contains(baseURL, "#") || parsed.Opaque != "" {
		return nil, fmt.Errorf("onedrive: invalid Graph base URL")
	}
	if strings.TrimSpace(accessToken) == "" || strings.IndexFunc(accessToken, unicode.IsControl) >= 0 {
		return nil, fmt.Errorf("onedrive: access token is required")
	}

	clientCopy := *client
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{
		httpClient:  &clientCopy,
		baseURL:     strings.TrimRight(parsed.String(), "/"),
		accessToken: accessToken,
	}, nil
}

// EnsureAlbum returns an existing folder ID or creates a direct child of App Folder.
func (c *Client) EnsureAlbum(ctx context.Context, name string) (app.RemoteAlbumID, error) {
	name = strings.TrimSpace(name)
	if !validFolderName(name) {
		return "", fmt.Errorf("%w: OneDrive folder name is invalid", app.ErrInvalidAlbum)
	}

	root, err := c.appFolder(ctx)
	if err != nil {
		return "", err
	}
	id, found, err := c.findFolder(ctx, root, name)
	if err != nil {
		return "", err
	}
	if found {
		return app.RemoteAlbumID(id), nil
	}

	body, err := json.Marshal(createFolderRequest{
		Name: name, Folder: struct{}{}, ConflictBehavior: "fail",
	})
	if err != nil {
		return "", fmt.Errorf("onedrive: encode create folder request")
	}
	resp, err := c.request(ctx, http.MethodPost, "/me/drive/items/"+url.PathEscape(root)+"/children", body)
	if err != nil {
		return "", err
	}
	switch resp.StatusCode {
	case http.StatusCreated:
		defer closeBody(resp)
		item, err := readFolder(resp, "create album")
		if err != nil {
			return "", err
		}
		return app.RemoteAlbumID(item.ID), nil
	case http.StatusConflict:
		closeBody(resp)
		id, found, err = c.findFolder(ctx, root, name)
		if err != nil {
			return "", err
		}
		if found {
			return app.RemoteAlbumID(id), nil
		}
		return "", fmt.Errorf("onedrive: resolve create conflict: Graph status %d: winning folder not found", http.StatusConflict)
	default:
		closeBody(resp)
		return "", graphStatusError("create album", resp.StatusCode)
	}
}

func (c *Client) appFolder(ctx context.Context) (string, error) {
	resp, err := c.request(ctx, http.MethodGet, "/me/drive/special/approot", nil)
	if err != nil {
		return "", err
	}
	defer closeBody(resp)
	if resp.StatusCode != http.StatusOK {
		return "", graphStatusError("get App Folder", resp.StatusCode)
	}
	item, err := readFolder(resp, "get App Folder")
	if err != nil {
		return "", err
	}
	return item.ID, nil
}

func (c *Client) findFolder(ctx context.Context, root, name string) (string, bool, error) {
	path := "/me/drive/items/" + url.PathEscape(root) + ":/" + url.PathEscape(name)
	resp, err := c.request(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", false, err
	}
	defer closeBody(resp)
	if resp.StatusCode == http.StatusNotFound {
		return "", false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, graphStatusError("find album", resp.StatusCode)
	}
	item, err := readFolder(resp, "find album")
	if err != nil {
		return "", false, err
	}
	return item.ID, true, nil
}

func (c *Client) request(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	if ctx == nil {
		return nil, fmt.Errorf("onedrive: %s request: context is required", strings.ToLower(method))
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("onedrive: %s request: invalid request", strings.ToLower(method))
	}
	request.Header.Set("Authorization", "Bearer "+c.accessToken)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("onedrive: %s request: %w", strings.ToLower(method), ctx.Err())
		}
		return nil, fmt.Errorf("onedrive: %s request failed", strings.ToLower(method))
	}
	return resp, nil
}

func readFolder(resp *http.Response, operation string) (graphFolder, error) {
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxGraphResponseBytes+1))
	if err != nil {
		readErr := err
		if resp.Request != nil {
			if contextErr := resp.Request.Context().Err(); contextErr != nil {
				readErr = contextErr
			}
		}
		if errors.Is(readErr, context.Canceled) {
			readErr = context.Canceled
		} else if errors.Is(readErr, context.DeadlineExceeded) {
			readErr = context.DeadlineExceeded
		} else {
			return graphFolder{}, fmt.Errorf("onedrive: %s: could not read Graph response (status %d)", operation, resp.StatusCode)
		}
		return graphFolder{}, fmt.Errorf("onedrive: %s: could not read Graph response (status %d): %w", operation, resp.StatusCode, readErr)
	}
	if len(data) > maxGraphResponseBytes {
		return graphFolder{}, fmt.Errorf("onedrive: %s: Graph response exceeds 64 KiB (status %d)", operation, resp.StatusCode)
	}
	var item graphFolder
	if err := json.Unmarshal(data, &item); err != nil {
		return graphFolder{}, fmt.Errorf("onedrive: %s: invalid Graph JSON (status %d)", operation, resp.StatusCode)
	}
	if strings.TrimSpace(item.ID) == "" {
		return graphFolder{}, fmt.Errorf("onedrive: %s: missing item ID (status %d)", operation, resp.StatusCode)
	}
	if item.Folder == nil {
		return graphFolder{}, fmt.Errorf("onedrive: %s: item is not a folder (status %d)", operation, resp.StatusCode)
	}
	return item, nil
}

func graphStatusError(operation string, status int) error {
	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("onedrive: %s: %w (status %d)", operation, app.ErrAuthentication, status)
	case http.StatusTooManyRequests:
		return fmt.Errorf("onedrive: %s: %w (status %d)", operation, app.ErrRateLimited, status)
	default:
		return fmt.Errorf("onedrive: %s: Graph status %d", operation, status)
	}
}

func validFolderName(name string) bool {
	if name == "" || name == "." || name == ".." ||
		strings.HasPrefix(name, "~") || strings.HasSuffix(name, ".") {
		return false
	}
	for _, char := range name {
		if unicode.IsControl(char) || strings.ContainsRune(`/\\*<>?:|"`, char) {
			return false
		}
	}
	return true
}

func closeBody(resp *http.Response) { _ = resp.Body.Close() }

type graphFolder struct {
	ID     string    `json:"id"`
	Folder *struct{} `json:"folder"`
}

type createFolderRequest struct {
	Name             string   `json:"name"`
	Folder           struct{} `json:"folder"`
	ConflictBehavior string   `json:"@microsoft.graph.conflictBehavior"`
}
