package flickr

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jooservices/xflickr/internal/accounts/app"
	"github.com/jooservices/xflickr/pkg/flickr/oauth"
)

// OAuthTimeout bounds each Flickr OAuth call so a stalled call cannot hang the request.
const OAuthTimeout = 15 * time.Second

// Permission is the access the member grants. Crawling only reads.
const Permission = "read"

// Endpoints overrides Flickr's OAuth URLs. Empty fields keep Flickr's own.
type Endpoints struct {
	RequestToken string
	Authorize    string
	AccessToken  string
}

// Authorizer runs Flickr OAuth 1.0a for a member's API key.
type Authorizer struct {
	httpClient   *http.Client
	endpoints    Endpoints
	timeout      time.Duration
	callObserver oauth.CallObserver
}

var _ app.Authorizer = Authorizer{}

// NewAuthorizer calls Flickr with httpClient. A nil client uses the default.
func NewAuthorizer(httpClient *http.Client, endpoints Endpoints, observers ...oauth.CallObserver) Authorizer {
	authorizer := Authorizer{httpClient: httpClient, endpoints: endpoints, timeout: OAuthTimeout}
	if len(observers) > 0 {
		authorizer.callObserver = observers[0]
	}
	return authorizer
}

// RequestToken asks Flickr for a request token bound to callbackURL.
func (a Authorizer) RequestToken(ctx context.Context, consumer app.Consumer, callbackURL string) (app.RequestToken, error) {
	ctx, cancel := a.bound(ctx)
	defer cancel()
	flow := a.flow(consumer)
	token, secret, err := flow.RequestToken(ctx, callbackURL)
	if err != nil {
		return app.RequestToken{}, classify(err)
	}
	authorizeURL, err := flow.AuthorizeURL(token, Permission)
	if err != nil {
		return app.RequestToken{}, app.ErrOAuthUnavailable
	}
	return app.RequestToken{Token: token, Secret: secret, AuthorizeURL: authorizeURL}, nil
}

// AccessToken exchanges the verifier for the member's access token.
func (a Authorizer) AccessToken(ctx context.Context, consumer app.Consumer, request app.RequestToken, verifier string) (app.Grant, error) {
	ctx, cancel := a.bound(ctx)
	defer cancel()
	flow := a.flow(consumer)
	flow.ResumeRequest(request.Token, request.Secret)
	token, secret, nsid, username, err := flow.AccessToken(ctx, verifier)
	if err != nil {
		return app.Grant{}, classify(err)
	}
	return app.Grant{Token: token, Secret: secret, NSID: nsid, Username: username}, nil
}

func (a Authorizer) flow(consumer app.Consumer) *oauth.Flow {
	flow := oauth.NewFlow(consumer.Key, consumer.Secret, a.httpClient)
	flow.SetEndpoints(a.endpoints.RequestToken, a.endpoints.Authorize, a.endpoints.AccessToken)
	flow.SetCallObserver(a.callObserver)
	return flow
}

func (a Authorizer) bound(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := a.timeout
	if timeout <= 0 {
		timeout = OAuthTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

// classify drops the underlying error text, which can echo request material.
func classify(err error) error {
	var protocol *oauth.ProtocolError
	if errors.As(err, &protocol) || errors.Is(err, oauth.ErrCallbackUnconfirmed) {
		return app.ErrOAuthRejected
	}
	return app.ErrOAuthUnavailable
}
