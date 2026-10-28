package flickr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCallSuccessBuildsRequestAndEmitsEvent(t *testing.T) {
	t.Parallel()

	const responseBody = `{"stat":"ok","photos":{"page":1}}`
	var observedName string
	var observedFields map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		for key, want := range map[string]string{
			"method":         "flickr.photos.search",
			"api_key":        "test-key",
			"format":         "json",
			"nojsoncallback": "1",
			"text":           "cat photos",
			"page":           "3",
		} {
			if got := r.Form.Get(key); got != want {
				t.Errorf("form[%q] = %q, want %q", key, got, want)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, responseBody)
	}))
	defer server.Close()

	observer := EventObserverFunc(func(name string, fields map[string]any) {
		observedName = name
		observedFields = fields
	})
	client := NewClient(server.URL, server.Client(), observer)
	got, meta, err := client.Call(context.Background(), Credentials{APIKey: "test-key"}, " flickr.photos.search ", map[string]string{
		"text":           "cat photos",
		"page":           "3",
		"api_key":        "overridden",
		"method":         "other.method",
		"format":         "xml",
		"nojsoncallback": "0",
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if string(got) != responseBody {
		t.Fatalf("Call() body = %s, want %s", got, responseBody)
	}
	if meta.HTTPStatus != http.StatusOK || meta.Bytes != int64(len(responseBody)) || meta.ErrorCode != 0 || meta.Latency < 0 {
		t.Fatalf("Call() meta = %+v", meta)
	}
	if observedName != "flickr.call" {
		t.Fatalf("event name = %q", observedName)
	}
	if observedFields["method"] != "flickr.photos.search" || observedFields["status"] != http.StatusOK || observedFields["bytes"] != int64(len(responseBody)) || observedFields["error_code"] != 0 {
		t.Fatalf("event fields = %#v", observedFields)
	}
	if _, ok := observedFields["latency_ms"].(int64); !ok {
		t.Fatalf("latency_ms has type %T, want int64", observedFields["latency_ms"])
	}
}

func TestCallPreservesRequestContextForContextAwareObserver(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"stat":"ok"}`)
	}))
	defer server.Close()
	type testContextKey struct{}
	ctx := context.WithValue(context.Background(), testContextKey{}, "request-context")
	var observedContext context.Context
	var observedName string
	client := NewClientWithContextObserver(server.URL, server.Client(), ContextEventObserverFunc(func(got context.Context, name string, _ map[string]any) {
		observedContext = got
		observedName = name
	}))
	if _, _, err := client.Call(ctx, Credentials{APIKey: "test-key"}, "flickr.test.echo", nil); err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if observedName != "flickr.call" || observedContext.Value(testContextKey{}) != "request-context" {
		t.Fatalf("observer got event %q with context value %v", observedName, observedContext.Value(testContextKey{}))
	}
}

func TestCallMapsFlickrErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		code     int
		text     string
		want     error
		alsoWant error
	}{
		{name: "invalid key", code: 100, text: "Invalid API Key", want: ErrInvalidKey},
		{name: "service unavailable", code: 105, text: "Service currently unavailable", want: ErrServiceUnavailable},
		{name: "photo not found", code: 1, text: "Photo not found", want: ErrNotFound},
		{name: "user not found", code: 2, text: "User not found", want: ErrNotFound},
		{name: "invalid token", code: 98, text: "Invalid auth token", want: ErrTokenRevoked},
		{name: "revoked token", code: 99, text: "User not logged in", want: ErrTokenRevoked},
		{name: "permission token error", code: 99, text: "Insufficient permissions", want: ErrTokenRevoked, alsoWant: ErrPermission},
		{name: "permission", code: 10, text: "Insufficient permissions", want: ErrPermission},
		{name: "code 98 permission", code: 98, text: "Permission denied", want: ErrResourceDenied, alsoWant: ErrPermission},
		{name: "code 98 login failed is a bad credential", code: 98, text: "Login failed", want: ErrTokenRevoked},
		{name: "code 98 denied token is a bad credential", code: 98, text: "Permission denied: token expired", want: ErrTokenRevoked},
		{name: "code 98 unknown message is a bad credential", code: 98, text: "Something odd", want: ErrTokenRevoked},
		{name: "service unavailable text", code: 105, text: "Service unavailable", want: ErrServiceUnavailable},
		{name: "quota message", code: 42, text: "Request quota exceeded", want: ErrQuotaExceeded},
		{name: "rate limit code", code: 429, text: "Rate limited", want: ErrRateLimited},
		{name: "unknown Flickr code", code: 777, text: "Unknown failure", want: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			body := fmt.Sprintf(`{"stat":"fail","code":%d,"message":%q}`, test.code, test.text)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()

			var eventFields map[string]any
			client := NewClient(server.URL, server.Client(), EventObserverFunc(func(_ string, fields map[string]any) { eventFields = fields }))
			_, meta, err := client.Call(context.Background(), Credentials{APIKey: "key"}, "flickr.test", nil)
			if err == nil {
				t.Fatal("Call() error = nil, want Flickr error")
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("errors.Is(%v, %v) = false", err, test.want)
			}
			if test.alsoWant != nil && !errors.Is(err, test.alsoWant) {
				t.Fatalf("errors.Is(%v, %v) = false", err, test.alsoWant)
			}
			if test.want == nil && errors.Is(err, ErrInvalidKey) {
				t.Fatalf("unknown error was classified as invalid key: %v", err)
			}
			if meta.ErrorCode != test.code || eventFields["error_code"] != test.code {
				t.Fatalf("meta/event error codes = %d/%v, want %d", meta.ErrorCode, eventFields["error_code"], test.code)
			}
		})
	}
}

func TestCallMapsHTTPFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{name: "rate limited", status: http.StatusTooManyRequests, body: "edge rate limit", want: ErrRateLimited},
		{name: "edge blocked", status: http.StatusForbidden, body: "<html>blocked</html>", want: ErrIPBlocked},
		{name: "server failure", status: http.StatusBadGateway, body: "upstream failure", want: ErrTransient},
		{name: "permission denied", status: http.StatusUnauthorized, body: "unauthorized", want: ErrPermission},
		{name: "Flickr error on unauthorized status", status: http.StatusUnauthorized, body: `{"stat":"fail","code":98,"message":"Invalid auth token"}`, want: ErrTokenRevoked},
		{name: "unclassified client status", status: http.StatusBadRequest, body: `{"stat":"ok"}`, want: ErrHTTPStatus},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()

			client := NewClient(server.URL, server.Client(), EventObserverFunc(func(string, map[string]any) {}))
			_, meta, err := client.Call(context.Background(), Credentials{APIKey: "key"}, "flickr.test", nil)
			if !errors.Is(err, test.want) {
				t.Fatalf("Call() error = %v, want errors.Is(_, %v)", err, test.want)
			}
			if meta.HTTPStatus != test.status || meta.Bytes != int64(len(test.body)) {
				t.Fatalf("Call() meta = %+v", meta)
			}
		})
	}
}

func TestCallRejectsMalformedJSON(t *testing.T) {
	t.Parallel()
	for _, body := range []string{"", "not json", `[]`, `{"stat":"fail","code":"bad","message":"broken"}`} {
		t.Run(fmt.Sprintf("body_%q", body), func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }))
			defer server.Close()
			client := NewClient(server.URL, server.Client(), EventObserverFunc(func(string, map[string]any) {}))
			_, meta, err := client.Call(context.Background(), Credentials{APIKey: "key"}, "flickr.test", nil)
			if !errors.Is(err, ErrMalformedResponse) {
				t.Fatalf("Call() error = %v, want ErrMalformedResponse", err)
			}
			if meta.HTTPStatus != http.StatusOK {
				t.Fatalf("HTTPStatus = %d", meta.HTTPStatus)
			}
		})
	}
}

func TestCallTimeoutIsTransient(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	client := NewClient(server.URL, &http.Client{Timeout: 20 * time.Millisecond}, EventObserverFunc(func(string, map[string]any) {}))
	_, meta, err := client.Call(context.Background(), Credentials{APIKey: "key"}, "flickr.test", nil)
	if !errors.Is(err, ErrTransient) {
		t.Fatalf("Call() error = %v, want ErrTransient", err)
	}
	if meta.HTTPStatus != 0 || meta.Latency <= 0 {
		t.Fatalf("Call() meta = %+v", meta)
	}
}

func TestCallValidationAndCancellation(t *testing.T) {
	t.Parallel()
	client := NewClient("bad://url", nil, EventObserverFunc(func(string, map[string]any) {}))
	_, _, err := client.Call(context.Background(), Credentials{APIKey: "key"}, "flickr.test", nil)
	if err == nil || !strings.Contains(err.Error(), "invalid Flickr base URL") {
		t.Fatalf("invalid endpoint error = %v", err)
	}

	client = NewClient(DefaultBaseURL, nil, EventObserverFunc(func(string, map[string]any) {}))
	_, _, err = client.Call(context.Background(), Credentials{}, "flickr.test", nil)
	if !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("missing key error = %v", err)
	}
	_, _, err = client.Call(context.Background(), Credentials{APIKey: "key"}, "  ", nil)
	if err == nil || !strings.Contains(err.Error(), "method is required") {
		t.Fatalf("missing method error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"stat":"ok"}`) }))
	defer server.Close()
	client = NewClient(server.URL, server.Client(), EventObserverFunc(func(string, map[string]any) {}))
	_, _, err = client.Call(ctx, Credentials{APIKey: "key"}, "flickr.test", nil)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrTransient) {
		t.Fatalf("cancelled request error = %v", err)
	}
}

func TestReadTimeoutIsTransient(t *testing.T) {
	t.Parallel()
	client := NewClient(DefaultBaseURL, &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       failingReadCloser{err: timeoutError{}},
		}, nil
	})}, EventObserverFunc(func(string, map[string]any) {}))
	_, meta, err := client.Call(context.Background(), Credentials{APIKey: "key"}, "flickr.test", nil)
	if !errors.Is(err, ErrTransient) {
		t.Fatalf("Call() error = %v, want ErrTransient", err)
	}
	if meta.HTTPStatus != http.StatusOK {
		t.Fatalf("HTTPStatus = %d", meta.HTTPStatus)
	}
}

func TestDecodeResponseAcceptsNumericAndStringCodes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		body string
		want int
	}{
		{body: `{"stat":"fail","code":100}`, want: 100},
		{body: `{"stat":"fail","code":"105"}`, want: 105},
	} {
		envelope, err := decodeResponse([]byte(test.body))
		if err != nil {
			t.Fatalf("decodeResponse(%s) error = %v", test.body, err)
		}
		if got := envelope.errorCode(); got != test.want {
			t.Errorf("errorCode() = %d, want %d", got, test.want)
		}
	}
}

func TestCallSignsWhenTokenIsPresent(t *testing.T) {
	t.Parallel()
	const (
		apiKey      = "653e7a6ecc1d528c516cc8f92cf98611"
		apiSecret   = "consumer secret"
		token       = "token-1"
		tokenSecret = "token~secret"
		wantSign    = "YLg/VnHsOg+kN8s+hIWaEjCbVOU="
	)
	var saw oauthFields
	client := NewClient("https://www.flickr.com/services/rest", &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		saw = oauthFields{
			signature: r.Form.Get("oauth_signature"),
			token:     r.Form.Get("oauth_token"),
			key:       r.Form.Get("oauth_consumer_key"),
			nonce:     r.Form.Get("oauth_nonce"),
			stamp:     r.Form.Get("oauth_timestamp"),
			version:   r.Form.Get("oauth_version"),
			method:    r.Form.Get("oauth_signature_method"),
			text:      r.Form.Get("text"),
			title:     r.Form.Get("title"),
			apiKey:    r.Form.Get("api_key"),
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"stat":"ok"}`)),
		}, nil
	})}, EventObserverFunc(func(string, map[string]any) {}))
	client.SetClock(func() time.Time { return time.Unix(1305583871, 0) })
	client.SetNonceGenerator(func() (string, error) { return "84354935", nil })

	_, _, err := client.Call(context.Background(), Credentials{
		APIKey:      apiKey,
		APISecret:   apiSecret,
		Token:       token,
		TokenSecret: tokenSecret,
	}, "flickr.test.login", map[string]string{"text": "hello world", "title": "café~x"})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if saw.signature != wantSign || saw.token != token || saw.key != apiKey || saw.nonce != "84354935" || saw.stamp != "1305583871" || saw.version != "1.0" || saw.method != "HMAC-SHA1" {
		t.Fatalf("signed form = %+v", saw)
	}
	if saw.text != "hello world" || saw.title != "café~x" || saw.apiKey != apiKey {
		t.Fatalf("request fields = %+v", saw)
	}
}

func TestCallSignatureRequiresSecret(t *testing.T) {
	t.Parallel()
	called := false
	client := NewClient(DefaultBaseURL, &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("should not send")
	})}, EventObserverFunc(func(string, map[string]any) {}))
	_, _, err := client.Call(context.Background(), Credentials{APIKey: "key", Token: "token"}, "flickr.test.login", nil)
	if err == nil || !strings.Contains(err.Error(), "API secret is required") {
		t.Fatalf("Call() error = %v", err)
	}
	if called {
		t.Fatal("request was sent without a secret")
	}

	client.SetNonceGenerator(func() (string, error) { return "", errors.New("entropy unavailable") })
	_, _, err = client.Call(context.Background(), Credentials{APIKey: "key", APISecret: "secret", Token: "token", TokenSecret: "token-secret"}, "flickr.test.login", nil)
	if err == nil || !strings.Contains(err.Error(), "entropy unavailable") {
		t.Fatalf("nonce error = %v", err)
	}

	client.SetNonceGenerator(nil)
	client.SetClock(nil)
	unsigned := NewClient(DefaultBaseURL, &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		if r.Form.Get("oauth_signature") != "" {
			t.Errorf("unsigned call was signed")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"stat":"ok"}`))}, nil
	})}, EventObserverFunc(func(string, map[string]any) {}))
	if _, _, err := unsigned.Call(context.Background(), Credentials{APIKey: "key", APISecret: "secret"}, "flickr.test.login", nil); err != nil {
		t.Fatalf("unsigned Call() error = %v", err)
	}

	revoked := NewClient(DefaultBaseURL, &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"stat":"fail","code":98,"message":"Invalid auth token"}`)),
		}, nil
	})}, EventObserverFunc(func(string, map[string]any) {}))
	revoked.SetNonceGenerator(func() (string, error) { return "n", nil })
	_, _, err = revoked.Call(context.Background(), Credentials{APIKey: "key", APISecret: "secret", Token: "token", TokenSecret: "token-secret"}, "flickr.test.login", nil)
	if !errors.Is(err, ErrTokenRevoked) {
		t.Fatalf("signed Flickr error = %v", err)
	}
	(*Client)(nil).SetClock(time.Now)
	(*Client)(nil).SetNonceGenerator(func() (string, error) { return "n", nil })
}

type oauthFields struct {
	signature string
	token     string
	key       string
	nonce     string
	stamp     string
	version   string
	method    string
	text      string
	title     string
	apiKey    string
}

func TestNewClientDefaults(t *testing.T) {
	t.Parallel()
	client := NewClient(" ", nil)
	if client.baseURL != DefaultBaseURL || client.httpClient != http.DefaultClient {
		t.Fatalf("default client = %#v", client)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

type failingReadCloser struct{ err error }

func (r failingReadCloser) Read([]byte) (int, error) { return 0, r.err }
func (r failingReadCloser) Close() error             { return nil }

type timeoutError struct{}

func (timeoutError) Error() string   { return "read timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

var _ net.Error = timeoutError{}
