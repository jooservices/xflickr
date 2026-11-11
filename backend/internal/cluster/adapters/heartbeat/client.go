// Package heartbeat sends one node heartbeat to a cluster control node.
package heartbeat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

const maxPayloadBytes = 64 * 1024

// Client sends a single heartbeat request to a control node.
type Client struct {
	baseURL    *url.URL
	token      string
	httpClient *http.Client
}

// Input contains the node metadata collected by the caller.
type Input struct {
	Version  string
	EgressIP string
	Metrics  json.RawMessage
}

// Result contains the control node's heartbeat response.
type Result struct {
	Name            string
	State           string
	LastHeartbeatAt time.Time
}

// New validates the control URL and token, and copies client before changing
// its redirect policy.
func New(baseURL, token string, client *http.Client) (*Client, error) {
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, errors.New("heartbeat: invalid control URL")
	}
	if !parsedURL.IsAbs() || (!strings.EqualFold(parsedURL.Scheme, "http") && !strings.EqualFold(parsedURL.Scheme, "https")) ||
		parsedURL.Host == "" || parsedURL.Hostname() == "" || parsedURL.Opaque != "" || parsedURL.User != nil ||
		parsedURL.Path != "" && parsedURL.Path != "/" || parsedURL.EscapedPath() != "" && parsedURL.EscapedPath() != "/" ||
		parsedURL.RawQuery != "" || parsedURL.ForceQuery || parsedURL.Fragment != "" || parsedURL.RawFragment != "" || strings.Contains(baseURL, "#") {
		return nil, errors.New("heartbeat: invalid control URL")
	}
	if token == "" || strings.IndexFunc(token, unicode.IsSpace) >= 0 {
		return nil, errors.New("heartbeat: node token is required and cannot contain whitespace")
	}
	if client == nil {
		return nil, errors.New("heartbeat: HTTP client is required")
	}

	parsedURL.Path = ""
	parsedURL.RawPath = ""
	clientCopy := *client
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Client{baseURL: parsedURL, token: token, httpClient: &clientCopy}, nil
}

// Send posts one heartbeat and returns the validated control-node response.
func (client *Client) Send(ctx context.Context, input Input) (Result, error) {
	if client == nil || client.baseURL == nil || client.httpClient == nil || ctx == nil {
		return Result{}, errors.New("heartbeat: client and context are required")
	}
	if len(input.Version) > 64 {
		return Result{}, errors.New("heartbeat: version exceeds 64 bytes")
	}
	if input.EgressIP != "" && net.ParseIP(input.EgressIP) == nil {
		return Result{}, errors.New("heartbeat: egress IP is invalid")
	}
	metrics, err := validateMetrics(input.Metrics)
	if err != nil {
		return Result{}, err
	}

	requestBody, err := json.Marshal(struct {
		Version  string          `json:"version"`
		EgressIP string          `json:"egress_ip"`
		Metrics  json.RawMessage `json:"metrics"`
	}{Version: input.Version, EgressIP: input.EgressIP, Metrics: metrics})
	if err != nil {
		return Result{}, errors.New("heartbeat: encode request")
	}

	endpoint := *client.baseURL
	endpoint.Path = "/internal/nodes/heartbeat"
	endpoint.RawPath = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return Result{}, errors.New("heartbeat: create request")
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Content-Type", "application/json")

	response, err := client.httpClient.Do(request)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return Result{}, fmt.Errorf("heartbeat: request canceled: %w", contextErr)
		}
		return Result{}, errors.New("heartbeat: send request")
	}
	if response == nil || response.Body == nil {
		return Result{}, errors.New("heartbeat: empty HTTP response")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("heartbeat: unexpected HTTP status %d", response.StatusCode)
	}

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxPayloadBytes+1))
	if err != nil {
		return Result{}, errors.New("heartbeat: read response")
	}
	if len(responseBody) > maxPayloadBytes {
		return Result{}, errors.New("heartbeat: response exceeds 64 KiB")
	}

	var payload struct {
		Name            *string    `json:"name"`
		State           *string    `json:"state"`
		LastHeartbeatAt *time.Time `json:"last_heartbeat_at"`
	}
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return Result{}, errors.New("heartbeat: invalid response JSON")
	}
	if payload.Name == nil || strings.TrimSpace(*payload.Name) == "" || payload.State == nil || !validState(*payload.State) || payload.LastHeartbeatAt == nil {
		return Result{}, errors.New("heartbeat: response is missing required fields")
	}
	return Result{Name: *payload.Name, State: *payload.State, LastHeartbeatAt: *payload.LastHeartbeatAt}, nil
}

func validateMetrics(metrics json.RawMessage) (json.RawMessage, error) {
	if len(metrics) == 0 {
		return json.RawMessage(`{}`), nil
	}
	if len(metrics) > maxPayloadBytes || !json.Valid(metrics) {
		return nil, errors.New("heartbeat: metrics must be a JSON object no larger than 64 KiB")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(metrics, &object); err != nil || object == nil {
		return nil, errors.New("heartbeat: metrics must be a JSON object no larger than 64 KiB")
	}
	return metrics, nil
}

func validState(state string) bool {
	switch state {
	case "online", "warning", "cooling", "offline", "disabled":
		return true
	default:
		return false
	}
}
