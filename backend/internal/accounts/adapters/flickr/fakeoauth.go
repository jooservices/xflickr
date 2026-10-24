package flickr

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
)

// Paths served by FakeOAuth.
const (
	FakeRequestTokenPath = "/services/oauth/request_token"
	FakeAuthorizePath    = "/services/oauth/authorize"
	FakeAccessTokenPath  = "/services/oauth/access_token"
)

// FakeOAuth is a fake Flickr OAuth server for tests. It issues request tokens
// for known consumer keys, accepts a verifier granted with Approve, and
// returns a fixed member.
type FakeOAuth struct {
	*httptest.Server

	consumerKey string
	nsid        string
	username    string

	mu        sync.Mutex
	issued    int
	callbacks map[string]string
	verifiers map[string]string
}

// NewFakeOAuth serves the three OAuth endpoints for one consumer key. Every
// successful exchange returns nsid and username.
func NewFakeOAuth(consumerKey, nsid, username string) *FakeOAuth {
	fake := &FakeOAuth{
		consumerKey: consumerKey,
		nsid:        nsid,
		username:    username,
		callbacks:   map[string]string{},
		verifiers:   map[string]string{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc(FakeRequestTokenPath, fake.requestToken)
	mux.HandleFunc(FakeAccessTokenPath, fake.accessToken)
	mux.HandleFunc(FakeAuthorizePath, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	fake.Server = httptest.NewServer(mux)
	return fake
}

// Endpoints returns the fake URLs for NewAuthorizer.
func (f *FakeOAuth) Endpoints() Endpoints {
	return Endpoints{
		RequestToken: f.URL + FakeRequestTokenPath,
		Authorize:    f.URL + FakeAuthorizePath,
		AccessToken:  f.URL + FakeAccessTokenPath,
	}
}

// Callback returns the oauth_callback the member's request token was bound to.
func (f *FakeOAuth) Callback(requestToken string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callbacks[requestToken]
}

// Approve grants the request token and returns the verifier Flickr would send
// to the callback.
func (f *FakeOAuth) Approve(requestToken string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	verifier := "verifier-for-" + requestToken
	f.verifiers[requestToken] = verifier
	return verifier
}

func (f *FakeOAuth) requestToken(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if problem := f.checkSigned(query); problem != "" {
		reply(w, http.StatusUnauthorized, "oauth_problem="+problem)
		return
	}
	callback := query.Get("oauth_callback")
	if callback == "" {
		reply(w, http.StatusBadRequest, "oauth_problem=parameter_absent")
		return
	}
	f.mu.Lock()
	f.issued++
	token := fmt.Sprintf("request-token-%d", f.issued)
	f.callbacks[token] = callback
	f.mu.Unlock()
	values := url.Values{"oauth_callback_confirmed": {"true"}, "oauth_token": {token}, "oauth_token_secret": {"secret-of-" + token}}
	reply(w, http.StatusOK, values.Encode())
}

func (f *FakeOAuth) accessToken(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if problem := f.checkSigned(query); problem != "" {
		reply(w, http.StatusUnauthorized, "oauth_problem="+problem)
		return
	}
	token := query.Get("oauth_token")
	f.mu.Lock()
	want, known := f.verifiers[token]
	f.mu.Unlock()
	if !known || want != query.Get("oauth_verifier") {
		reply(w, http.StatusUnauthorized, "oauth_problem=permission_denied")
		return
	}
	values := url.Values{
		"oauth_token":        {"access-" + token},
		"oauth_token_secret": {"access-secret-" + token},
		"user_nsid":          {f.nsid},
		"username":           {f.username},
	}
	reply(w, http.StatusOK, values.Encode())
}

func (f *FakeOAuth) checkSigned(query url.Values) string {
	if query.Get("oauth_consumer_key") != f.consumerKey {
		return "consumer_key_unknown"
	}
	if strings.TrimSpace(query.Get("oauth_signature")) == "" {
		return "signature_invalid"
	}
	return ""
}

func reply(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/x-www-form-urlencoded")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}
