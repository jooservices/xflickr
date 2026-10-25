// Package openobserve ships ops events to OpenObserve's JSON ingestion API.
package openobserve

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

	"github.com/jooservices/xflickr/internal/platform/ops"
)

// Sink posts events to {URL}/api/{Org}/{Stream}/_json with basic auth.
type Sink struct {
	URL      string
	Org      string
	Stream   string
	Username string
	Password string
	Client   *http.Client
}

var _ ops.Sink = Sink{}

// Send delivers events in one request. Any non-2xx response is an error.
func (s Sink) Send(ctx context.Context, events []ops.Event) error {
	if ctx == nil || strings.TrimSpace(s.URL) == "" || s.Org == "" || s.Stream == "" {
		return errors.New("ops: openobserve sink is not configured")
	}
	if len(events) == 0 {
		return nil
	}
	body, err := json.Marshal(events)
	if err != nil {
		return fmt.Errorf("ops: encode events: %w", err)
	}
	endpoint := strings.TrimRight(s.URL, "/") + "/api/" + url.PathEscape(s.Org) + "/" + url.PathEscape(s.Stream) + "/_json"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ops: build openobserve request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth(s.Username, s.Password)
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("ops: send events: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<16))
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("ops: openobserve responded %d", response.StatusCode)
	}
	return nil
}
