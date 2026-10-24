package flickr

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/accounts/app"
)

const oauthKey = "0123456789abcdef0123456789abcdef"

func TestAuthorizerRunsTheFlowAgainstFakeOAuth(t *testing.T) {
	fake := NewFakeOAuth(oauthKey, "21207597@N07", "jamal")
	defer fake.Close()
	authorizer := NewAuthorizer(fake.Client(), fake.Endpoints())
	consumer := app.Consumer{Key: oauthKey, Secret: "consumer-secret"}

	request, err := authorizer.RequestToken(t.Context(), consumer, "http://127.0.0.1:8080/flickr-accounts/callback?state=abc")
	if err != nil {
		t.Fatal(err)
	}
	authorize, err := url.Parse(request.AuthorizeURL)
	if err != nil || authorize.Path != FakeAuthorizePath || authorize.Query().Get("oauth_token") != request.Token || authorize.Query().Get("perms") != Permission {
		t.Fatalf("authorize url = %q %v", request.AuthorizeURL, err)
	}
	if got := fake.Callback(request.Token); got != "http://127.0.0.1:8080/flickr-accounts/callback?state=abc" {
		t.Fatalf("callback = %q", got)
	}
	if resp, err := fake.Client().Get(request.AuthorizeURL); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("authorize page = %v %v", resp, err)
	}

	if _, err := authorizer.AccessToken(t.Context(), consumer, request, "wrong"); !errors.Is(err, app.ErrOAuthRejected) {
		t.Fatalf("unapproved exchange = %v", err)
	}
	grant, err := authorizer.AccessToken(t.Context(), consumer, request, fake.Approve(request.Token))
	if err != nil {
		t.Fatal(err)
	}
	if grant.NSID != "21207597@N07" || grant.Username != "jamal" || grant.Token == "" || grant.Secret == "" {
		t.Fatalf("grant = %+v", grant)
	}
}

func TestAuthorizerClassifiesFailures(t *testing.T) {
	fake := NewFakeOAuth(oauthKey, "n", "u")
	defer fake.Close()
	authorizer := NewAuthorizer(fake.Client(), fake.Endpoints())
	if _, err := authorizer.RequestToken(t.Context(), app.Consumer{Key: "other", Secret: "s"}, "http://127.0.0.1/cb"); !errors.Is(err, app.ErrOAuthRejected) {
		t.Fatalf("unknown consumer = %v", err)
	}
	if _, err := authorizer.RequestToken(t.Context(), app.Consumer{Key: oauthKey, Secret: "s"}, " "); !errors.Is(err, app.ErrOAuthUnavailable) {
		t.Fatalf("empty callback = %v", err)
	}
	unconfirmed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("oauth_token=t&oauth_token_secret=s"))
	}))
	defer unconfirmed.Close()
	rejected := NewAuthorizer(unconfirmed.Client(), Endpoints{RequestToken: unconfirmed.URL})
	if _, err := rejected.RequestToken(t.Context(), app.Consumer{Key: oauthKey, Secret: "s"}, "http://127.0.0.1/cb"); !errors.Is(err, app.ErrOAuthRejected) {
		t.Fatalf("unconfirmed callback = %v", err)
	}

	fake.Close()
	if _, err := authorizer.RequestToken(t.Context(), app.Consumer{Key: oauthKey, Secret: "s"}, "http://127.0.0.1/cb"); !errors.Is(err, app.ErrOAuthUnavailable) {
		t.Fatalf("closed server = %v", err)
	}
	if _, err := authorizer.AccessToken(t.Context(), app.Consumer{Key: oauthKey, Secret: "s"}, app.RequestToken{Token: "t", Secret: "s"}, "v"); !errors.Is(err, app.ErrOAuthUnavailable) {
		t.Fatalf("closed server exchange = %v", err)
	}
}

func TestAuthorizerBadAuthorizeURLIsUnavailable(t *testing.T) {
	fake := NewFakeOAuth(oauthKey, "n", "u")
	defer fake.Close()
	endpoints := fake.Endpoints()
	endpoints.Authorize = "http://[::1"
	authorizer := Authorizer{httpClient: fake.Client(), endpoints: endpoints, timeout: -time.Second}
	if _, err := authorizer.RequestToken(t.Context(), app.Consumer{Key: oauthKey, Secret: "s"}, "http://127.0.0.1/cb"); !errors.Is(err, app.ErrOAuthUnavailable) {
		t.Fatalf("bad authorize url = %v", err)
	}
}

func TestFakeOAuthRejectsUnsignedAndCallbackLessRequests(t *testing.T) {
	fake := NewFakeOAuth(oauthKey, "n", "u")
	defer fake.Close()
	for name, query := range map[string]string{
		"unsigned":        "oauth_consumer_key=" + oauthKey + "&oauth_callback=x",
		"missing token":   "oauth_consumer_key=" + oauthKey + "&oauth_signature=s",
		"no callback":     "oauth_consumer_key=" + oauthKey + "&oauth_signature=s",
		"unknown token":   "oauth_consumer_key=" + oauthKey + "&oauth_signature=s&oauth_token=x&oauth_verifier=y",
		"unknown consume": "oauth_consumer_key=nope&oauth_signature=s&oauth_callback=x",
	} {
		path := FakeRequestTokenPath
		if strings.Contains(name, "token") {
			path = FakeAccessTokenPath
		}
		resp, err := fake.Client().Get(fake.URL + path + "?" + query)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Errorf("%s status = %d", name, resp.StatusCode)
		}
	}
	resp, err := fake.Client().Get(fake.URL + FakeAccessTokenPath + "?oauth_consumer_key=nope&oauth_signature=s")
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("access with unknown consumer = %v %v", resp, err)
	}
	_ = resp.Body.Close()
}
