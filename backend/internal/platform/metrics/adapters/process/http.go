package process

import (
	"net/http"
)

// Middleware counts request and response body bytes on the server side.
func Middleware(counter *ByteCounter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if counter == nil || next == nil {
				if next != nil {
					next.ServeHTTP(w, r)
				}
				return
			}
			if r.ContentLength > 0 {
				counter.AddIn(uint64(r.ContentLength))
			}
			recorder := &countingResponseWriter{ResponseWriter: w, counter: counter}
			next.ServeHTTP(recorder, r)
		})
	}
}

type countingResponseWriter struct {
	http.ResponseWriter
	counter *ByteCounter
}

func (w *countingResponseWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if n > 0 {
		w.counter.AddOut(uint64(n))
	}
	return n, err
}

// RoundTripper counts outbound HTTP request and response bytes.
type RoundTripper struct {
	Base    http.RoundTripper
	Counter *ByteCounter
}

// RoundTrip implements http.RoundTripper.
func (t RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	if t.Counter != nil && req != nil && req.ContentLength > 0 {
		t.Counter.AddOut(uint64(req.ContentLength))
	}
	resp, err := base.RoundTrip(req)
	if err != nil || t.Counter == nil || resp == nil {
		return resp, err
	}
	if resp.ContentLength > 0 {
		t.Counter.AddIn(uint64(resp.ContentLength))
	}
	return resp, nil
}
