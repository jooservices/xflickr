package oauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultRequestTokenURL is Flickr's temporary-credential endpoint.
	DefaultRequestTokenURL = "https://www.flickr.com/services/oauth/request_token" //nolint:gosec // public endpoint URL, not a credential
	// DefaultAuthorizeURL is the browser page where the member grants access.
	DefaultAuthorizeURL = "https://www.flickr.com/services/oauth/authorize" //nolint:gosec // public endpoint URL, not a credential
	// DefaultAccessTokenURL exchanges a verified request token for an access token.
	DefaultAccessTokenURL = "https://www.flickr.com/services/oauth/access_token" //nolint:gosec // public endpoint URL, not a credential
)

const defaultPermission = "read"

// ErrCallbackUnconfirmed means Flickr did not confirm oauth_callback.
var ErrCallbackUnconfirmed = errors.New("oauth: oauth_callback_confirmed was not true")

// ErrNoRequestToken means AccessToken was called before a successful RequestToken.
var ErrNoRequestToken = errors.New("oauth: request token is missing")

// ProtocolError is an OAuth failure reported by Flickr.
type ProtocolError struct {
	Problem  string
	Status   int
	DebugSBS string
}

func (e *ProtocolError) Error() string {
	if e == nil {
		return "oauth: protocol error"
	}
	problem := e.Problem
	if problem == "" {
		problem = "request failed"
	}
	if e.Status != 0 {
		return fmt.Sprintf("oauth: %s (HTTP %d)", problem, e.Status)
	}
	return "oauth: " + problem
}

// Flow is one Flickr OAuth 1.0a authorization. It keeps the request token
// secret until AccessToken exchanges it. A Flow is not safe for concurrent use.
type Flow struct {
	consumerKey     string
	consumerSecret  string
	httpClient      *http.Client
	now             func() time.Time
	newNonce        func() (string, error)
	requestToken    string
	requestSecret   string
	requestTokenURL string
	authorizeURL    string
	accessTokenURL  string
	callObserver    CallObserver
}

// NewFlow starts an authorization with the member's Flickr API key and secret.
// A nil HTTP client uses http.DefaultClient.
func NewFlow(consumerKey, consumerSecret string, httpClient *http.Client) *Flow {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Flow{
		consumerKey:     consumerKey,
		consumerSecret:  consumerSecret,
		httpClient:      httpClient,
		requestTokenURL: DefaultRequestTokenURL,
		authorizeURL:    DefaultAuthorizeURL,
		accessTokenURL:  DefaultAccessTokenURL,
	}
}

// SetClock overrides the clock used for oauth_timestamp. Nil restores time.Now.
func (f *Flow) SetClock(now func() time.Time) {
	if f != nil {
		f.now = now
	}
}

// SetNonceGenerator overrides nonce generation. Nil restores a cryptographic nonce.
func (f *Flow) SetNonceGenerator(fn func() (string, error)) {
	if f != nil {
		f.newNonce = fn
	}
}

// SetCallObserver records each HTTP request made by the authorization flow.
func (f *Flow) SetCallObserver(observer CallObserver) {
	if f != nil {
		f.callObserver = observer
	}
}

// SetEndpoints overrides the Flickr OAuth endpoints. Empty values keep the current URL.
func (f *Flow) SetEndpoints(requestTokenURL, authorizeURL, accessTokenURL string) {
	if f == nil {
		return
	}
	if strings.TrimSpace(requestTokenURL) != "" {
		f.requestTokenURL = strings.TrimSpace(requestTokenURL)
	}
	if strings.TrimSpace(authorizeURL) != "" {
		f.authorizeURL = strings.TrimSpace(authorizeURL)
	}
	if strings.TrimSpace(accessTokenURL) != "" {
		f.accessTokenURL = strings.TrimSpace(accessTokenURL)
	}
}

// RequestToken asks Flickr for a temporary token bound to callbackURL.
// callbackURL may be "oob" when the member will read the verifier from the page.
func (f *Flow) RequestToken(ctx context.Context, callbackURL string) (token, secret string, err error) {
	if err := f.validate(); err != nil {
		return "", "", err
	}
	callbackURL = strings.TrimSpace(callbackURL)
	if callbackURL == "" {
		return "", "", errors.New("oauth: callback URL is required")
	}
	params := url.Values{"oauth_callback": {callbackURL}}
	signed, err := f.signer().Sign(http.MethodGet, f.requestTokenURL, params)
	if err != nil {
		return "", "", err
	}
	values, err := f.get(ctx, f.requestTokenURL, signed)
	if err != nil {
		return "", "", err
	}
	if values.Get("oauth_callback_confirmed") != "true" {
		return "", "", ErrCallbackUnconfirmed
	}
	token = values.Get("oauth_token")
	secret = values.Get("oauth_token_secret")
	if token == "" || secret == "" {
		return "", "", errors.New("oauth: token response missing oauth_token or oauth_token_secret")
	}
	f.requestToken = token
	f.requestSecret = secret
	return token, secret, nil
}

// ResumeRequest restores a request token issued earlier, possibly by another
// process, so AccessToken can complete the authorization.
func (f *Flow) ResumeRequest(token, secret string) {
	if f != nil {
		f.requestToken = token
		f.requestSecret = secret
	}
}

// AuthorizeURL is the Flickr page the member opens to grant perms.
// An empty perms value means read. write and delete are the other Flickr values.
func (f *Flow) AuthorizeURL(token, perms string) (string, error) {
	if f == nil {
		return "", errors.New("oauth: flow is nil")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", errors.New("oauth: request token is required")
	}
	switch strings.TrimSpace(perms) {
	case "", defaultPermission:
		perms = defaultPermission
	case "write", "delete":
	default:
		return "", fmt.Errorf("oauth: unsupported permission %q", perms)
	}
	parsed, err := url.Parse(f.authorizeURL)
	if err != nil {
		return "", fmt.Errorf("oauth: invalid authorize URL: %w", err)
	}
	query := url.Values{}
	query.Set("oauth_token", token)
	query.Set("perms", perms)
	parsed.RawQuery = EncodeQuery(query)
	return parsed.String(), nil
}

// AccessToken exchanges the verifier for an access token, its secret, and the
// Flickr user nsid and username. It uses the request token from RequestToken.
func (f *Flow) AccessToken(ctx context.Context, verifier string) (token, secret, userNSID, username string, err error) {
	if err := f.validate(); err != nil {
		return "", "", "", "", err
	}
	if f.requestToken == "" || f.requestSecret == "" {
		return "", "", "", "", ErrNoRequestToken
	}
	verifier = strings.TrimSpace(verifier)
	if verifier == "" {
		return "", "", "", "", errors.New("oauth: verifier is required")
	}
	signer := f.signer()
	signer.Token = f.requestToken
	signer.TokenSecret = f.requestSecret
	params := url.Values{"oauth_verifier": {verifier}}
	signed, err := signer.Sign(http.MethodGet, f.accessTokenURL, params)
	if err != nil {
		return "", "", "", "", err
	}
	values, err := f.get(ctx, f.accessTokenURL, signed)
	if err != nil {
		return "", "", "", "", err
	}
	token = values.Get("oauth_token")
	secret = values.Get("oauth_token_secret")
	userNSID = values.Get("user_nsid")
	username = values.Get("username")
	if token == "" || secret == "" || userNSID == "" || username == "" {
		return "", "", "", "", errors.New("oauth: access token response is incomplete")
	}
	f.requestToken = ""
	f.requestSecret = ""
	return token, secret, userNSID, username, nil
}

func (f *Flow) signer() Signer {
	return Signer{
		ConsumerKey:    f.consumerKey,
		ConsumerSecret: f.consumerSecret,
		Now:            f.now,
		NewNonce:       f.newNonce,
	}
}

func (f *Flow) validate() error {
	if f == nil {
		return errors.New("oauth: flow is nil")
	}
	if strings.TrimSpace(f.consumerKey) == "" || strings.TrimSpace(f.consumerSecret) == "" {
		return errors.New("oauth: consumer key and secret are required")
	}
	return nil
}

func (f *Flow) get(ctx context.Context, endpoint string, params url.Values) (values url.Values, callErr error) {
	if ctx == nil {
		return nil, errors.New("oauth: context is required")
	}
	startedAt := time.Now().UTC()
	metadata, _ := CallContextFrom(ctx)
	if metadata.RequestID == "" {
		metadata.RequestID = NewRequestID()
	}
	if metadata.Attempt <= 0 {
		metadata.Attempt = 1
	}
	if metadata.Lane == "" {
		metadata.Lane = "interactive"
	}
	ctx = WithCallContext(ctx, metadata)
	httpStatus, responseBytes := 0, int64(0)
	defer func() {
		outcome := "success"
		message := ""
		if callErr != nil {
			outcome = "failure"
			message = "OAuth request failed"
			if httpStatus == http.StatusTooManyRequests {
				outcome = "throttled"
			}
		}
		if f != nil && f.callObserver != nil {
			f.callObserver.RecordCall(ctx, CallRecord{
				CallContext: metadata, Method: oauthMethod(endpoint), StartedAt: startedAt,
				Latency: time.Since(startedAt), HTTPStatus: httpStatus, ErrorMessage: message,
				ResponseBytes: responseBytes, Outcome: outcome,
			})
		}
	}()
	values, httpStatus, responseBytes, callErr = f.readTokenResponse(ctx, endpoint, params)
	return values, callErr
}

func (f *Flow) readTokenResponse(ctx context.Context, endpoint string, params url.Values) (url.Values, int, int64, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, 0, 0, fmt.Errorf("oauth: invalid endpoint %q", endpoint)
	}
	if parsed.RawQuery != "" {
		return nil, 0, 0, errors.New("oauth: endpoint must not include a query")
	}
	parsed.RawQuery = EncodeQuery(params)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("oauth: build request: %w", err)
	}
	request.Header.Set("Accept", "application/x-www-form-urlencoded")
	response, err := f.httpClient.Do(request)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("oauth: send request: %w", err)
	}
	defer response.Body.Close()
	httpStatus := response.StatusCode
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, httpStatus, 0, fmt.Errorf("oauth: read response: %w", err)
	}
	responseBytes := int64(len(body))
	values, err := url.ParseQuery(strings.TrimSpace(string(body)))
	if err != nil {
		return nil, httpStatus, responseBytes, fmt.Errorf("oauth: malformed token response: %w", err)
	}
	if problem := values.Get("oauth_problem"); problem != "" {
		return nil, httpStatus, responseBytes, &ProtocolError{Problem: problem, Status: httpStatus, DebugSBS: values.Get("debug_sbs")}
	}
	if httpStatus < 200 || httpStatus >= 300 {
		return nil, httpStatus, responseBytes, &ProtocolError{Problem: "unexpected status", Status: httpStatus}
	}
	return values, httpStatus, responseBytes, nil
}

func oauthMethod(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "oauth.call"
	}
	switch {
	case strings.HasSuffix(parsed.Path, "/request_token"):
		return "oauth.request_token"
	case strings.HasSuffix(parsed.Path, "/access_token"):
		return "oauth.access_token"
	default:
		return "oauth.call"
	}
}
