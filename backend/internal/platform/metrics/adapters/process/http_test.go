package process_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jooservices/xflickr/internal/platform/metrics/adapters/process"
)

func TestHTTPMiddlewareAndRoundTripperCountBytes(t *testing.T) {
	counter := &process.ByteCounter{}
	handler := process.Middleware(counter)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("abcd"))
	req.ContentLength = 4
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	in, out := counter.Snapshot()
	if in != 4 || out != 5 {
		t.Fatalf("server in/out=%d/%d", in, out)
	}

	clientCounter := &process.ByteCounter{}
	transport := process.RoundTripper{
		Counter: clientCounter,
		Base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("xyz")), ContentLength: 3, Request: req}, nil
		}),
	}
	req = httptest.NewRequest(http.MethodPost, "http://example.test/", strings.NewReader("ab"))
	req.ContentLength = 2
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	in, out = clientCounter.Snapshot()
	if out != 2 || in != 3 {
		t.Fatalf("client in/out=%d/%d", in, out)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
