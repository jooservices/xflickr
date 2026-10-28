package flickr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jooservices/xflickr/pkg/flickr/oauth"
)

const DefaultBaseURL = "https://www.flickr.com/services/rest"

// Credentials identifies the Flickr application and, once the member has
// authorized it, the OAuth token used to sign calls to private data.
type Credentials struct {
	APIKey      string
	APISecret   string
	Token       string
	TokenSecret string
}

// EventObserver receives transport events without coupling this library to an
// application package. Applications can adapt it to their obs.Event API.
type EventObserver interface {
	Event(name string, fields map[string]any)
}

// ContextEventObserver receives transport events with the request context so
// tracing and request-scoped logging fields are preserved.
type ContextEventObserver interface {
	EventContext(ctx context.Context, name string, fields map[string]any)
}

// EventObserverFunc adapts a function to EventObserver.
type EventObserverFunc func(name string, fields map[string]any)

func (f EventObserverFunc) Event(name string, fields map[string]any) {
	if f != nil {
		f(name, fields)
	}
}

// ContextEventObserverFunc adapts a context-aware function to
// ContextEventObserver.
type ContextEventObserverFunc func(context.Context, string, map[string]any)

func (f ContextEventObserverFunc) EventContext(ctx context.Context, name string, fields map[string]any) {
	if f != nil {
		f(ctx, name, fields)
	}
}

// Client sends Flickr REST calls and classifies Flickr and HTTP failures.
type Client struct {
	baseURL         string
	httpClient      *http.Client
	observer        EventObserver
	contextObserver ContextEventObserver
	now             func() time.Time
	newNonce        func() (string, error)
}

// NewClient creates a client that targets baseURL and uses httpClient. Empty
// values select Flickr's REST endpoint and http.DefaultClient. An optional
// observer receives each call event; without one, events go to slog.Default.
func NewClient(baseURL string, httpClient *http.Client, observers ...EventObserver) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	client := &Client{baseURL: baseURL, httpClient: httpClient}
	if len(observers) > 0 {
		client.observer = observers[0]
	}
	return client
}

// NewClientWithContextObserver creates a client that forwards each call event
// together with the call's context.
func NewClientWithContextObserver(baseURL string, httpClient *http.Client, observer ContextEventObserver) *Client {
	client := NewClient(baseURL, httpClient)
	client.contextObserver = observer
	return client
}

// SetClock overrides the clock used when a call is signed. Nil restores time.Now.
func (c *Client) SetClock(now func() time.Time) {
	if c != nil {
		c.now = now
	}
}

// SetNonceGenerator overrides nonce generation for signed calls. Nil restores
// a cryptographic nonce.
func (c *Client) SetNonceGenerator(fn func() (string, error)) {
	if c != nil {
		c.newNonce = fn
	}
}

// Call invokes a Flickr REST method using form encoded parameters. Flickr's
// JSON response is returned intact on success. Calls that carry an OAuth
// token are signed with HMAC-SHA1.
//
//nolint:cyclop // single Flickr call pipeline (sign, send, decode, classify); tracked for a refactor
func (c *Client) Call(ctx context.Context, creds Credentials, method string, params map[string]string) (payload json.RawMessage, meta CallMeta, callErr error) {
	started := time.Now()
	defer func() {
		meta.Latency = time.Since(started)
		c.emitCallEvent(ctx, method, meta)
	}()

	method = strings.TrimSpace(method)
	if method == "" {
		return nil, meta, &CallError{Message: "method is required"}
	}
	if strings.TrimSpace(creds.APIKey) == "" {
		return nil, meta, &CallError{Kind: ErrInvalidKey, Code: 100}
	}
	endpoint, err := parseEndpoint(c.baseURL)
	if err != nil {
		return nil, meta, &CallError{Cause: err}
	}

	form := make(url.Values, len(params)+4)
	for key, value := range params {
		form.Set(key, value)
	}
	form.Set("method", method)
	form.Set("api_key", creds.APIKey)
	form.Set("format", "json")
	form.Set("nojsoncallback", "1")
	form, err = c.signForm(endpoint.String(), creds, form)
	if err != nil {
		return nil, meta, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, meta, &CallError{Cause: err}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := c.httpClient.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			return nil, meta, &CallError{Cause: err}
		}
		if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
			return nil, meta, &CallError{Kind: ErrTransient, Cause: err}
		}
		return nil, meta, &CallError{Kind: ErrTransient, Cause: err}
	}
	defer response.Body.Close()
	meta.HTTPStatus = response.StatusCode

	body, readErr := io.ReadAll(response.Body)
	meta.Bytes = int64(len(body))
	if readErr != nil {
		kind := error(nil)
		if errors.Is(readErr, context.DeadlineExceeded) || isTimeout(readErr) {
			kind = ErrTransient
		}
		return nil, meta, &CallError{Kind: kind, Status: response.StatusCode, Cause: readErr}
	}

	envelope, decodeErr := decodeResponse(body)
	if decodeErr == nil {
		meta.ErrorCode = envelope.errorCode()
	}

	if response.StatusCode == http.StatusTooManyRequests {
		return nil, meta, &CallError{Kind: ErrRateLimited, Code: meta.ErrorCode, Status: response.StatusCode, Message: responseMessage(envelope, "too many requests")}
	}
	if response.StatusCode >= 500 {
		return nil, meta, &CallError{Kind: ErrTransient, Code: meta.ErrorCode, Status: response.StatusCode, Message: responseMessage(envelope, http.StatusText(response.StatusCode))}
	}
	if response.StatusCode == http.StatusForbidden {
		if decodeErr == nil && strings.EqualFold(envelope.Stat, "fail") && envelope.errorCode() != 0 {
			return nil, meta, flickrError(envelope, response.StatusCode)
		}
		return nil, meta, &CallError{Kind: ErrIPBlocked, Status: response.StatusCode, Message: responseMessage(envelope, "request blocked by Flickr edge")}
	}
	if response.StatusCode == http.StatusUnauthorized {
		if decodeErr == nil && strings.EqualFold(envelope.Stat, "fail") && envelope.errorCode() != 0 {
			return nil, meta, flickrError(envelope, response.StatusCode)
		}
		return nil, meta, &CallError{Kind: ErrPermission, Code: meta.ErrorCode, Status: response.StatusCode, Message: responseMessage(envelope, http.StatusText(response.StatusCode))}
	}
	if decodeErr != nil {
		return nil, meta, &CallError{Kind: ErrMalformedResponse, Status: response.StatusCode, Cause: decodeErr}
	}
	if strings.EqualFold(envelope.Stat, "fail") {
		return nil, meta, flickrError(envelope, response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, meta, &CallError{Kind: ErrHTTPStatus, Code: meta.ErrorCode, Status: response.StatusCode, Message: responseMessage(envelope, http.StatusText(response.StatusCode))}
	}
	return append(json.RawMessage(nil), body...), meta, nil
}

func (c *Client) signForm(endpoint string, creds Credentials, form url.Values) (url.Values, error) {
	if strings.TrimSpace(creds.Token) == "" {
		return form, nil
	}
	if strings.TrimSpace(creds.APISecret) == "" {
		return nil, &CallError{Message: "API secret is required when an OAuth token is set"}
	}
	signed, err := (oauth.Signer{
		ConsumerKey:    creds.APIKey,
		ConsumerSecret: creds.APISecret,
		Token:          creds.Token,
		TokenSecret:    creds.TokenSecret,
		Now:            c.now,
		NewNonce:       c.newNonce,
	}).Sign(http.MethodPost, endpoint, form)
	if err != nil {
		return nil, &CallError{Cause: err}
	}
	return signed, nil
}

func (c *Client) emitCallEvent(ctx context.Context, method string, meta CallMeta) {
	fields := map[string]any{
		"method":     method,
		"status":     meta.HTTPStatus,
		"latency_ms": meta.Latency.Milliseconds(),
		"bytes":      meta.Bytes,
		"error_code": meta.ErrorCode,
	}
	if c != nil && c.contextObserver != nil {
		c.contextObserver.EventContext(ctx, "flickr.call", fields)
		return
	}
	if c != nil && c.observer != nil {
		c.observer.Event("flickr.call", fields)
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	slog.Default().LogAttrs(ctx, slog.LevelInfo, "flickr.call",
		slog.String("method", method),
		slog.Int("status", meta.HTTPStatus),
		slog.Int64("latency_ms", meta.Latency.Milliseconds()),
		slog.Int64("bytes", meta.Bytes),
		slog.Int("error_code", meta.ErrorCode),
	)
}

func parseEndpoint(raw string) (*url.URL, error) {
	endpoint, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid Flickr base URL: %w", err)
	}
	if endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return nil, fmt.Errorf("invalid Flickr base URL: expected an absolute HTTP or HTTPS URL")
	}
	return endpoint, nil
}

type responseEnvelope struct {
	Stat      string          `json:"stat"`
	Code      json.RawMessage `json:"code"`
	Message   string          `json:"message"`
	codeValue int
}

func decodeResponse(body []byte) (responseEnvelope, error) {
	trimmed := strings.TrimSpace(string(body))
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return responseEnvelope{}, fmt.Errorf("expected a JSON object")
	}
	var envelope responseEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return responseEnvelope{}, err
	}
	if len(envelope.Code) > 0 {
		code, err := parseErrorCode(envelope.Code)
		if err != nil {
			return responseEnvelope{}, fmt.Errorf("invalid Flickr error code: %w", err)
		}
		envelope.codeValue = code
	}
	return envelope, nil
}

func (e responseEnvelope) errorCode() int { return e.codeValue }

func parseErrorCode(raw json.RawMessage) (int, error) {
	var code int
	if err := json.Unmarshal(raw, &code); err == nil {
		return code, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, err
	}
	return strconv.Atoi(text)
}

func responseMessage(envelope responseEnvelope, fallback string) string {
	if envelope.Message != "" {
		return envelope.Message
	}
	return fallback
}

func flickrError(envelope responseEnvelope, status int) error {
	code := envelope.errorCode()
	message := responseMessage(envelope, "Flickr API request failed")
	var kind error
	switch {
	case code == 100:
		kind = ErrInvalidKey
	case code == 105:
		kind = ErrServiceUnavailable
	case code == 429:
		kind = ErrRateLimited
	case code == 1 || code == 2:
		kind = ErrNotFound
	case code == 98 && resourceDeniedMessage(message):
		kind = errors.Join(ErrResourceDenied, ErrPermission)
	case code == 98:
		// An ambiguous login failure is treated as a bad credential.
		kind = ErrTokenRevoked
	case code == 99:
		kind = ErrTokenRevoked
		if strings.Contains(strings.ToLower(message), "permission") {
			kind = errors.Join(ErrTokenRevoked, ErrPermission)
		}
	case code == 10:
		kind = ErrPermission
	case isQuotaError(code, message):
		kind = ErrQuotaExceeded
	}
	return &CallError{Kind: kind, Code: code, Status: status, Message: message}
}

// resourceDeniedMessage reports a code-98 message that names a denied
// resource rather than a failed login.
func resourceDeniedMessage(message string) bool {
	lower := strings.ToLower(message)
	if strings.Contains(lower, "token") || strings.Contains(lower, "login") {
		return false
	}
	return strings.Contains(lower, "permission denied") || strings.Contains(lower, "not allowed") || strings.Contains(lower, "private")
}

func isQuotaError(code int, message string) bool {
	if code == 429 {
		return true
	}
	lower := strings.ToLower(message)
	return strings.Contains(lower, "quota") || strings.Contains(lower, "rate limit") || strings.Contains(lower, "too many request")
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
