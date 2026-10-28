// Package ollama implements the AI application ports with Ollama's HTTP API.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	aiapp "github.com/jooservices/xflickr/internal/ai/app"
	"github.com/jooservices/xflickr/internal/platform/breaker"
)

const (
	maxResponseBytes    = 4 << 20
	defaultFailureLimit = 5
	defaultOpenDuration = 60 * time.Second
)

// Config holds the deployment settings supplied to this adapter. Timeout
// bounds one description, including queue wait; Concurrency limits requests to
// Ollama.
type Config struct {
	URL         string
	Model       string
	Timeout     time.Duration
	Concurrency int
	HTTPClient  *http.Client
}

// Client describes images using a configured Ollama vision model.
type Client struct {
	baseURL    *url.URL
	model      string
	timeout    time.Duration
	httpClient *http.Client
	semaphore  chan struct{}
	breaker    *breaker.Breaker

	metadataMu sync.Mutex
	version    string
}

// New validates adapter settings and creates the default five-failure,
// sixty-second circuit breaker.
func New(config Config) (*Client, error) {
	baseURL, err := url.Parse(strings.TrimSpace(config.URL))
	if err != nil || baseURL == nil || !baseURL.IsAbs() || baseURL.Host == "" || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, fmt.Errorf("%w: Ollama URL must be an absolute HTTP URL without credentials or query", aiapp.ErrInvalidConfiguration)
	}
	if strings.TrimSpace(config.Model) == "" || config.Timeout <= 0 || config.Concurrency < 1 {
		return nil, fmt.Errorf("%w: model, positive timeout and positive concurrency are required", aiapp.ErrInvalidConfiguration)
	}
	providerBreaker, err := breaker.New(breaker.Config{FailureThreshold: defaultFailureLimit, ResetTimeout: defaultOpenDuration})
	if err != nil {
		return nil, fmt.Errorf("create Ollama circuit breaker: %w", err)
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{
		baseURL:    baseURL,
		model:      strings.TrimSpace(config.Model),
		timeout:    config.Timeout,
		httpClient: httpClient,
		semaphore:  make(chan struct{}, config.Concurrency),
		breaker:    providerBreaker,
	}, nil
}

// Describe requests a structured caption and object list from Ollama. Invalid
// JSON is retried once; transport and provider failures are not retried here.
func (c *Client) Describe(parent context.Context, image []byte) (aiapp.Caption, error) {
	if parent == nil {
		return aiapp.Caption{}, fmt.Errorf("%w: context is required", aiapp.ErrInvalidConfiguration)
	}
	if len(image) == 0 {
		return aiapp.Caption{}, aiapp.ErrInvalidImage
	}
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()
	select {
	case c.semaphore <- struct{}{}:
		defer func() { <-c.semaphore }()
	case <-ctx.Done():
		return aiapp.Caption{}, ctx.Err()
	}
	if !c.breaker.Allow() {
		return aiapp.Caption{}, aiapp.ErrCircuitOpen
	}

	caption, err := c.describe(ctx, image)
	if err != nil {
		if parent.Err() != nil {
			c.breaker.RecordAbandoned()
		} else {
			c.breaker.RecordFailure()
		}
		return aiapp.Caption{}, err
	}
	c.breaker.RecordSuccess()
	return caption, nil
}

func (c *Client) describe(ctx context.Context, image []byte) (aiapp.Caption, error) {
	version, err := c.modelVersion(ctx)
	if err != nil {
		return aiapp.Caption{}, err
	}
	request := chatRequest{
		Model: c.model,
		Messages: []chatMessage{{
			Role:    "user",
			Content: captionPrompt,
			Images:  []string{encodeImage(image)},
		}},
		Stream:  false,
		Format:  captionSchema,
		Options: map[string]any{"temperature": 0},
	}
	for attempt := 0; attempt < 2; attempt++ {
		var response chatResponse
		if err := c.post(ctx, "api/chat", request, &response); err != nil {
			return aiapp.Caption{}, err
		}
		caption, err := parseCaption(response.Message.Content, c.model, version)
		if err == nil {
			return caption, nil
		}
		if attempt == 1 {
			return aiapp.Caption{}, err
		}
	}
	return aiapp.Caption{}, aiapp.ErrInvalidResponse
}

func (c *Client) modelVersion(ctx context.Context) (string, error) {
	c.metadataMu.Lock()
	defer c.metadataMu.Unlock()
	if c.version != "" {
		return c.version, nil
	}
	var response showResponse
	if err := c.post(ctx, "api/show", showRequest{Model: c.model}, &response); err != nil {
		return "", err
	}
	if strings.TrimSpace(response.ModifiedAt) == "" {
		return "", fmt.Errorf("%w: Ollama /api/show omitted modified_at", aiapp.ErrInvalidResponse)
	}
	c.version = response.ModifiedAt
	return c.version, nil
}

func (c *Client) post(ctx context.Context, endpoint string, payload, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode Ollama %s request: %w", endpoint, err)
	}
	requestURL := c.baseURL.JoinPath(endpoint).String()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Ollama %s request: %w", endpoint, err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("ollama %s request: %w", endpoint, err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read Ollama %s response: %w", endpoint, err)
	}
	if len(responseBody) > maxResponseBytes {
		return fmt.Errorf("%w: Ollama %s response exceeds %d bytes", aiapp.ErrInvalidResponse, endpoint, maxResponseBytes)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		var providerError struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(responseBody, &providerError)
		if strings.TrimSpace(providerError.Error) == "" {
			return fmt.Errorf("ollama %s returned HTTP %d", endpoint, response.StatusCode)
		}
		return fmt.Errorf("ollama %s: %s", endpoint, providerError.Error)
	}
	if err := json.Unmarshal(responseBody, result); err != nil {
		return fmt.Errorf("%w: decode Ollama %s response: %w", aiapp.ErrInvalidResponse, endpoint, err)
	}
	return nil
}

var _ aiapp.Captioner = (*Client)(nil)
