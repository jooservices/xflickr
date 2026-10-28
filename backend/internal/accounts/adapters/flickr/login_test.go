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

	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

func TestLoginProbeUsesTestLoginAndClassifiesAuthFailures(t *testing.T) {
	var requestForm url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		requestForm = request.Form
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"stat":"ok","user":{"id":"user-1","username":{"_content":"jamal"}}}`)
	}))
	t.Cleanup(server.Close)
	probe := NewLoginProbe(server.URL, server.Client())
	user, err := probe.TestLogin(t.Context(), flickrclient.Credentials{APIKey: "public", APISecret: "secret", Token: "token", TokenSecret: "token-secret"})
	if err != nil || user.ID != "user-1" || string(user.Username) != "jamal" {
		t.Fatalf("TestLogin() = %+v, %v", user, err)
	}
	if requestForm.Get("method") != "flickr.test.login" || requestForm.Get("oauth_token") != "token" || requestForm.Get("api_key") != "public" || requestForm.Get("oauth_signature") == "" {
		t.Fatalf("request parameters = %v", requestForm)
	}
	if strings.Contains(requestForm.Encode(), "secret") || strings.Contains(requestForm.Encode(), "token-secret") {
		t.Fatalf("request form exposed a secret: %v", requestForm)
	}

	revoked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"stat":"fail","code":98,"message":"Invalid auth token"}`)
	}))
	t.Cleanup(revoked.Close)
	_, err = NewLoginProbe(revoked.URL, revoked.Client()).TestLogin(t.Context(), flickrclient.Credentials{APIKey: "public", APISecret: "secret", Token: "token", TokenSecret: "token-secret"})
	if !errors.Is(err, flickrclient.ErrTokenRevoked) {
		t.Fatalf("revoked token error = %v", err)
	}
}

func TestLoginProbeBoundsRequestsAndRejectsMissingInputs(t *testing.T) {
	if _, err := (LoginProbe{}).TestLogin(t.Context(), flickrclient.Credentials{}); err == nil || !strings.Contains(err.Error(), "client is required") {
		t.Fatalf("nil client error = %v", err)
	}
	probe := NewLoginProbe("http://127.0.0.1:1", nil)
	if _, err := probe.TestLogin(nil, flickrclient.Credentials{}); err == nil || !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("nil context error = %v", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := probe.TestLogin(canceled, flickrclient.Credentials{APIKey: "public"}); err == nil {
		t.Fatal("canceled request unexpectedly succeeded")
	}

	blocking := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { time.Sleep(200 * time.Millisecond) }))
	t.Cleanup(blocking.Close)
	slow := NewLoginProbe(blocking.URL, blocking.Client())
	slow.timeout = 20 * time.Millisecond
	_, err := slow.TestLogin(t.Context(), flickrclient.Credentials{APIKey: "public", APISecret: "secret", Token: "token", TokenSecret: "token-secret"})
	if err == nil || !errors.Is(err, flickrclient.ErrTransient) {
		t.Fatalf("timeout error = %v", err)
	}
}
