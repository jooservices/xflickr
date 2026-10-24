package oauth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFlowRequestAuthorizeAndAccess(t *testing.T) {
	t.Parallel()
	var sawAccess bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		if strings.Contains(r.URL.RawQuery, "+") {
			t.Errorf("query uses form encoding: %s", r.URL.RawQuery)
		}
		switch r.URL.Path {
		case "/request_token":
			if !strings.Contains(r.URL.RawQuery, "oauth_callback=http%3A%2F%2Fexample.com%2Fa%20b~%C3%A9") {
				t.Errorf("callback query = %s", r.URL.RawQuery)
			}
			if r.URL.Query().Get("oauth_nonce") != "nonce-1" || r.URL.Query().Get("oauth_timestamp") != "1305586162" {
				t.Errorf("nonce/timestamp = %s %s", r.URL.Query().Get("oauth_nonce"), r.URL.Query().Get("oauth_timestamp"))
			}
			if r.URL.Query().Get("oauth_token") != "" {
				t.Errorf("request token call included oauth_token")
			}
			assertQuerySignature(t, r, "consumer secret", "")
			_, _ = io.WriteString(w, "oauth_callback_confirmed=true&oauth_token=request-token&oauth_token_secret=request-secret")
		case "/access_token":
			sawAccess = true
			if r.URL.Query().Get("oauth_token") != "request-token" || r.URL.Query().Get("oauth_verifier") != "verifier-1" {
				t.Errorf("access query = %s", r.URL.RawQuery)
			}
			assertQuerySignature(t, r, "consumer secret", "request-secret")
			_, _ = io.WriteString(w, "fullname=Jamal%20Fanaian&oauth_token=access-token&oauth_token_secret=access%20secret&user_nsid=21207597%40N07&username=jamal%20fanaian")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	flow := NewFlow("consumer-key", "consumer secret", server.Client())
	flow.SetEndpoints(server.URL+"/request_token", server.URL+"/authorize", server.URL+"/access_token")
	flow.SetClock(func() time.Time { return time.Unix(1305586162, 0) })
	flow.SetNonceGenerator(func() (string, error) { return "nonce-1", nil })

	token, secret, err := flow.RequestToken(context.Background(), " http://example.com/a b~é ")
	if err != nil {
		t.Fatalf("RequestToken() error = %v", err)
	}
	if token != "request-token" || secret != "request-secret" {
		t.Fatalf("RequestToken() = %q %q", token, secret)
	}
	authorize, err := flow.AuthorizeURL(token, "")
	if err != nil {
		t.Fatalf("AuthorizeURL() error = %v", err)
	}
	if authorize != server.URL+"/authorize?oauth_token=request-token&perms=read" {
		t.Fatalf("AuthorizeURL() = %s", authorize)
	}
	for _, perms := range []string{"write", "delete"} {
		got, err := flow.AuthorizeURL("a b~", perms)
		if err != nil {
			t.Fatalf("AuthorizeURL(%s) error = %v", perms, err)
		}
		if !strings.Contains(got, "oauth_token=a%20b~") || !strings.Contains(got, "perms="+perms) {
			t.Fatalf("AuthorizeURL(%s) = %s", perms, got)
		}
	}

	accessToken, accessSecret, nsid, username, err := flow.AccessToken(context.Background(), " verifier-1 ")
	if err != nil {
		t.Fatalf("AccessToken() error = %v", err)
	}
	if accessToken != "access-token" || accessSecret != "access secret" || nsid != "21207597@N07" || username != "jamal fanaian" {
		t.Fatalf("AccessToken() = %q %q %q %q", accessToken, accessSecret, nsid, username)
	}
	if !sawAccess {
		t.Fatal("access token endpoint was not called")
	}
	if _, _, _, _, err := flow.AccessToken(context.Background(), "again"); !errors.Is(err, ErrNoRequestToken) {
		t.Fatalf("second AccessToken() error = %v", err)
	}
}

func TestFlowKeepsRequestTokenWhenExchangeFails(t *testing.T) {
	t.Parallel()
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if strings.HasSuffix(r.URL.Path, "/request_token") {
			_, _ = io.WriteString(w, "oauth_callback_confirmed=true&oauth_token=keep-token&oauth_token_secret=keep-secret")
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "oauth_problem=token_rejected&debug_sbs=GET%26example")
	}))
	defer server.Close()

	flow := NewFlow("key", "secret", server.Client())
	flow.SetEndpoints(server.URL+"/request_token", "", server.URL+"/access_token")
	flow.SetNonceGenerator(func() (string, error) { return "n", nil })
	if _, _, err := flow.RequestToken(context.Background(), "oob"); err != nil {
		t.Fatalf("RequestToken() error = %v", err)
	}
	_, _, _, _, err := flow.AccessToken(context.Background(), "verifier")
	var protocol *ProtocolError
	if !errors.As(err, &protocol) || protocol.Problem != "token_rejected" || protocol.Status != http.StatusUnauthorized || protocol.DebugSBS != "GET&example" {
		t.Fatalf("AccessToken() error = %#v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
	if _, _, _, _, err := flow.AccessToken(context.Background(), "verifier"); !errors.As(err, &protocol) {
		t.Fatalf("request token was dropped, error = %v", err)
	}
}

func TestFlowValidation(t *testing.T) {
	t.Parallel()
	flow := NewFlow("key", "secret", nil)
	if flow.httpClient != http.DefaultClient {
		t.Fatal("nil client was not replaced")
	}
	ctx := context.Background()
	if _, _, err := (*Flow)(nil).RequestToken(ctx, "oob"); err == nil {
		t.Fatal("nil flow RequestToken error = nil")
	}
	if _, err := (*Flow)(nil).AuthorizeURL("token", "read"); err == nil {
		t.Fatal("nil flow AuthorizeURL error = nil")
	}
	if _, _, _, _, err := (*Flow)(nil).AccessToken(ctx, "v"); err == nil {
		t.Fatal("nil flow AccessToken error = nil")
	}
	(*Flow)(nil).SetClock(time.Now)
	(*Flow)(nil).SetNonceGenerator(func() (string, error) { return "n", nil })
	(*Flow)(nil).SetEndpoints("http://example.test", "http://example.test", "http://example.test")

	if _, _, err := NewFlow(" ", "secret", nil).RequestToken(ctx, "oob"); err == nil {
		t.Fatal("blank consumer error = nil")
	}
	if _, _, err := flow.RequestToken(ctx, "  "); err == nil || !strings.Contains(err.Error(), "callback URL") {
		t.Fatalf("blank callback error = %v", err)
	}
	if _, _, err := flow.RequestToken(nil, "oob"); err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("nil context error = %v", err)
	}
	if _, err := flow.AuthorizeURL(" ", "read"); err == nil {
		t.Fatal("blank token error = nil")
	}
	if _, err := flow.AuthorizeURL("token", "admin"); err == nil || !strings.Contains(err.Error(), "unsupported permission") {
		t.Fatalf("bad permission error = %v", err)
	}
	flow.SetEndpoints("", "", "")
	if _, _, _, _, err := flow.AccessToken(ctx, "v"); !errors.Is(err, ErrNoRequestToken) {
		t.Fatalf("missing request token error = %v", err)
	}
	flow.requestToken = "token"
	flow.requestSecret = "secret"
	if _, _, _, _, err := flow.AccessToken(ctx, " "); err == nil || !strings.Contains(err.Error(), "verifier") {
		t.Fatalf("blank verifier error = %v", err)
	}
	flow.SetNonceGenerator(func() (string, error) { return "", errors.New("no entropy") })
	if _, _, err := flow.RequestToken(ctx, "oob"); err == nil || !strings.Contains(err.Error(), "no entropy") {
		t.Fatalf("nonce error = %v", err)
	}
	flow.SetNonceGenerator(func() (string, error) { return "n", nil })
	flow.SetEndpoints("://bad", "://bad", "://also-bad")
	if _, _, err := flow.RequestToken(ctx, "oob"); err == nil {
		t.Fatal("bad request endpoint error = nil")
	}
	if _, err := flow.AuthorizeURL("token", "read"); err == nil {
		t.Fatal("bad authorize endpoint error = nil")
	}
	flow.authorizeURL = DefaultAuthorizeURL
	flow.requestTokenURL = "https://example.test/request?already=1"
	if _, _, err := flow.RequestToken(ctx, "oob"); err == nil || !strings.Contains(err.Error(), "must not include a query") {
		t.Fatalf("query endpoint error = %v", err)
	}
}

func TestFlowResponseFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		status  int
		body    string
		want    string
		problem string
	}{
		{name: "problem on success status", status: http.StatusOK, body: "oauth_problem=signature_invalid", problem: "signature_invalid"},
		{name: "callback not confirmed", status: http.StatusOK, body: "oauth_callback_confirmed=false&oauth_token=t&oauth_token_secret=s", want: ErrCallbackUnconfirmed.Error()},
		{name: "missing secret", status: http.StatusOK, body: "oauth_callback_confirmed=true&oauth_token=t", want: "missing oauth_token"},
		{name: "unexpected status", status: http.StatusBadGateway, body: "upstream down", problem: "unexpected status"},
		{name: "malformed body", status: http.StatusOK, body: "%", want: "malformed token response"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			flow := NewFlow("key", "secret", server.Client())
			flow.SetEndpoints(server.URL, server.URL, server.URL)
			flow.SetNonceGenerator(func() (string, error) { return "n", nil })
			_, _, err := flow.RequestToken(context.Background(), "oob")
			if err == nil {
				t.Fatal("RequestToken() error = nil")
			}
			if test.problem != "" {
				var protocol *ProtocolError
				if !errors.As(err, &protocol) || protocol.Problem != test.problem {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if test.want == ErrCallbackUnconfirmed.Error() && !errors.Is(err, ErrCallbackUnconfirmed) {
				t.Fatalf("error = %v", err)
			}
			if !strings.Contains(err.Error(), test.want) && test.want != ErrCallbackUnconfirmed.Error() {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestFlowAccessResponseIsIncomplete(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "request") {
			_, _ = io.WriteString(w, "oauth_callback_confirmed=true&oauth_token=t&oauth_token_secret=s")
			return
		}
		_, _ = io.WriteString(w, "oauth_token=access&oauth_token_secret=secret&user_nsid=123")
	}))
	defer server.Close()
	flow := NewFlow("key", "secret", server.Client())
	flow.SetEndpoints(server.URL+"/request", server.URL+"/authorize", server.URL+"/access")
	flow.SetNonceGenerator(func() (string, error) { return "n", nil })
	if _, _, err := flow.RequestToken(context.Background(), "oob"); err != nil {
		t.Fatalf("RequestToken() error = %v", err)
	}
	if _, _, _, _, err := flow.AccessToken(context.Background(), "v"); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("AccessToken() error = %v", err)
	}
}

func TestFlowTransportFailures(t *testing.T) {
	t.Parallel()
	flow := NewFlow("key", "secret", &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed")
	})})
	flow.SetNonceGenerator(func() (string, error) { return "n", nil })
	flow.SetEndpoints("https://example.test/request", "", "")
	if _, _, err := flow.RequestToken(context.Background(), "oob"); err == nil || !strings.Contains(err.Error(), "dial failed") {
		t.Fatalf("transport error = %v", err)
	}

	flow.httpClient = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       errorReadCloser{},
		}, nil
	})}
	if _, _, err := flow.RequestToken(context.Background(), "oob"); err == nil || !strings.Contains(err.Error(), "read response") {
		t.Fatalf("read error = %v", err)
	}
}

func TestProtocolErrorNil(t *testing.T) {
	t.Parallel()
	var err *ProtocolError
	if err.Error() != "oauth: protocol error" {
		t.Fatalf("nil error = %q", err.Error())
	}
	if got := (&ProtocolError{}).Error(); got != "oauth: request failed" {
		t.Fatalf("empty error = %q", got)
	}
	if got := (&ProtocolError{Problem: "signature_invalid"}).Error(); got != "oauth: signature_invalid" {
		t.Fatalf("problem error = %q", got)
	}
}

func assertQuerySignature(t *testing.T, r *http.Request, consumerSecret, tokenSecret string) {
	t.Helper()
	values := r.URL.Query()
	got := values.Get("oauth_signature")
	values.Del("oauth_signature")
	endpoint := *r.URL
	if endpoint.Scheme == "" {
		endpoint.Scheme = "http"
	}
	if endpoint.Host == "" {
		endpoint.Host = r.Host
	}
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	want, err := Signature(r.Method, endpoint.String(), values, consumerSecret, tokenSecret)
	if err != nil {
		t.Fatalf("Signature() error = %v", err)
	}
	if got != want {
		t.Fatalf("query signature = %s, want %s", got, want)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type errorReadCloser struct{}

func (errorReadCloser) Read([]byte) (int, error) { return 0, errors.New("read broke") }
func (errorReadCloser) Close() error             { return nil }

func TestFlowResumeRequestAllowsAccessTokenInAnotherProcess(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("oauth_token") != "request-token" {
			t.Errorf("oauth_token = %q", r.URL.Query().Get("oauth_token"))
		}
		assertQuerySignature(t, r, "consumer secret", "request-secret")
		_, _ = io.WriteString(w, "oauth_token=access-token&oauth_token_secret=access-secret&user_nsid=1%40N01&username=jamal")
	}))
	defer server.Close()

	flow := NewFlow("consumer-key", "consumer secret", server.Client())
	flow.SetEndpoints("", "", server.URL+"/access_token")
	flow.SetClock(func() time.Time { return time.Unix(1305586162, 0) })
	flow.SetNonceGenerator(func() (string, error) { return "nonce-1", nil })
	flow.ResumeRequest("request-token", "request-secret")
	token, secret, nsid, username, err := flow.AccessToken(t.Context(), "verifier-1")
	if err != nil || token != "access-token" || secret != "access-secret" || nsid != "1@N01" || username != "jamal" {
		t.Fatalf("access = %q %q %q %q %v", token, secret, nsid, username, err)
	}
	(*Flow)(nil).ResumeRequest("a", "b")
}
