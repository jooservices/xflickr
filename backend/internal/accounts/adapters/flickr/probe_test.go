package flickr

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/accounts/app"
)

func TestEchoAcceptsAValidKeyWithoutSendingTheSecret(t *testing.T) {
	const secret = "super-secret-value"
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		payload, _ := io.ReadAll(request.Body)
		body = string(payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"stat":"ok","method":"flickr.test.echo"}`)
	}))
	t.Cleanup(server.Close)

	probe := NewProbe(server.URL, server.Client())
	if err := probe.Echo(t.Context(), "0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, secret) || strings.Contains(body, "api_sig") {
		t.Fatalf("request contained secret material: %s", body)
	}
	if !strings.Contains(body, "method=flickr.test.echo") || !strings.Contains(body, "api_key=0123456789abcdef0123456789abcdef") {
		t.Fatalf("request body = %s", body)
	}
}

func TestEchoRejectsAnInvalidKey(t *testing.T) {
	server := NewFakeFlickr("0123456789abcdef0123456789abcdef")
	t.Cleanup(server.Close)
	probe := NewProbe(server.URL, server.Client())
	if err := probe.Echo(t.Context(), "0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	err := probe.Echo(t.Context(), "ffffffffffffffffffffffffffffffff")
	if !errors.Is(err, app.ErrKeyInvalid) {
		t.Fatalf("invalid key error = %v", err)
	}
	if strings.Contains(err.Error(), "ffffffffffffffffffffffffffffffff") {
		t.Fatalf("error includes the key: %v", err)
	}
}

func TestEchoReportsCheckFailuresWithoutTheKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	probe := NewProbe(server.URL, server.Client())
	err := probe.Echo(t.Context(), "0123456789abcdef0123456789abcdef")
	if err == nil || errors.Is(err, app.ErrKeyInvalid) || strings.Contains(err.Error(), "0123456789abcdef0123456789abcdef") {
		t.Fatalf("error = %v", err)
	}

	codeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"stat":"fail","code":100,"message":"Invalid API Key"}`)
	}))
	t.Cleanup(codeServer.Close)
	if err := (NewProbe(codeServer.URL, codeServer.Client())).Echo(t.Context(), "bad"); !errors.Is(err, app.ErrKeyInvalid) {
		t.Fatalf("code 100 error = %v", err)
	}

	blocking := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	t.Cleanup(blocking.Close)
	slow := NewProbe(blocking.URL, blocking.Client())
	slow.timeout = 20 * time.Millisecond
	if err := slow.Echo(t.Context(), "0123456789abcdef0123456789abcdef"); err == nil || errors.Is(err, app.ErrKeyInvalid) {
		t.Fatalf("timeout error = %v", err)
	}
}

func TestEchoRejectsAnIncompleteProbe(t *testing.T) {
	if err := (Probe{}).Echo(t.Context(), "key"); err == nil || !strings.Contains(err.Error(), "client is required") {
		t.Fatalf("nil client error = %v", err)
	}
	server := NewFakeFlickr("key")
	t.Cleanup(server.Close)
	probe := NewProbe(server.URL, server.Client())
	if err := probe.Echo(nil, "key"); err == nil || !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("nil context error = %v", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := probe.Echo(canceled, "key"); err == nil || errors.Is(err, app.ErrKeyInvalid) {
		t.Fatalf("canceled error = %v", err)
	}
	probe.timeout = 0
	if err := probe.Echo(t.Context(), "key"); err != nil {
		t.Fatal(err)
	}
	if err := probe.Echo(t.Context(), "  "); !errors.Is(err, app.ErrKeyInvalid) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("blank key error = %v", err)
	}
}

func TestFakeFlickrRejectsUnexpectedRequests(t *testing.T) {
	server := NewFakeFlickr("good-key")
	t.Cleanup(server.Close)
	for _, form := range []url.Values{
		{"method": {"flickr.photos.search"}, "api_key": {"good-key"}},
		{"method": {"flickr.test.echo"}, "api_key": {"good-key"}, "secret": {"super-secret-value"}},
		{"method": {"flickr.test.echo"}, "api_key": {"good-key"}, "api_sig": {"sig"}},
	} {
		response, err := server.Client().PostForm(server.URL, form)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusInternalServerError || strings.Contains(string(body), "super-secret-value") {
			t.Fatalf("status = %d body = %s", response.StatusCode, body)
		}
	}

}
