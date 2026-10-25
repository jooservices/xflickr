// Package cdn provides the HTTP client used by the media fetcher.
package cdn

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jooservices/xflickr/internal/media/app"
)

// Client implements the media application's CDNClient port with bounded
// connection setup and response-header timeouts. Response bodies remain
// streaming and are closed by the application service.
type Client struct {
	client *http.Client
}

// New creates a CDN client. A nil transport uses the Go default transport.
func New(transport http.RoundTripper) *Client {
	if transport == nil {
		if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
			cloned := defaultTransport.Clone()
			cloned.ResponseHeaderTimeout = ResponseHeaderTimeout
			transport = cloned
		} else {
			transport = http.DefaultTransport
		}
	}
	return &Client{client: &http.Client{
		Transport: transport,
		Timeout:   0, // Large originals must be governed by request context, not a whole-body timeout.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

// Get executes a streaming request. It requires a caller context and supports
// the open-ended range used when resuming a partial original.
func (c *Client) Get(ctx context.Context, request app.CDNRequest) (*app.CDNResponse, error) {
	if c == nil || c.client == nil || ctx == nil || request.URL == "" {
		return nil, errors.New("cdn: client, context, and URL are required")
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, request.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("cdn: create request: %w", err)
	}
	if request.Range != "" {
		httpRequest.Header.Set("Range", request.Range)
	}
	response, err := c.client.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	return &app.CDNResponse{
		StatusCode: response.StatusCode, ContentLength: response.ContentLength,
		ContentRange: response.Header.Get("Content-Range"), Body: response.Body,
	}, nil
}

// ResponseHeaderTimeout is the default CDN response-header budget.
const ResponseHeaderTimeout = 30 * time.Second
