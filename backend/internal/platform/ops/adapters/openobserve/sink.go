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
		return errors.New("ops: build openobserve request failed")
	}
	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth(s.Username, s.Password)
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		// Transport errors may include the full request URL, including any
		// credentials embedded in a privately configured URL. Keep logs useful
		// without exposing that configuration.
		return errors.New("ops: send events failed")
	}
	defer response.Body.Close()
	ackBody, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<16))
	if readErr != nil {
		return errors.New("ops: read openobserve acknowledgement failed")
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("ops: openobserve responded %d", response.StatusCode)
	}
	return checkAcknowledgement(ackBody)
}

func checkAcknowledgement(body []byte) error {
	if len(body) == 0 {
		return nil
	}
	var ack struct {
		Code   int `json:"code"`
		Status []struct {
			Failed int `json:"failed"`
		} `json:"status"`
	}
	if json.Unmarshal(body, &ack) != nil || ack.Code >= 400 {
		return errors.New("ops: openobserve rejected events")
	}
	for _, result := range ack.Status {
		if result.Failed > 0 {
			return errors.New("ops: openobserve rejected events")
		}
	}
	return nil
}
