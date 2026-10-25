package roles

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	accountsflickr "github.com/jooservices/xflickr/internal/accounts/adapters/flickr"
	accountshttp "github.com/jooservices/xflickr/internal/accounts/adapters/http"
	accountsapp "github.com/jooservices/xflickr/internal/accounts/app"
	accountsdomain "github.com/jooservices/xflickr/internal/accounts/domain"
	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	identityapp "github.com/jooservices/xflickr/internal/identity/app"
	identitydomain "github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/crypto"
	schedulehttp "github.com/jooservices/xflickr/internal/schedule/adapters/http"
)

const (
	flowSession  = "signed-in"
	flowKey      = "flow-consumer-key"
	flowNSID     = "21207597@N07"
	flowUserID   = int64(7)
	flowKeyID    = int64(4)
	flowCallback = "http://example.test/flickr-accounts/callback"
)

type flowSessions struct{ stubSessions }

func (flowSessions) Authenticate(_ context.Context, raw string) (identityapp.Authenticated, error) {
	if raw != flowSession {
		return identityapp.Authenticated{}, identityapp.ErrUnauthenticated
	}
	return identityapp.Authenticated{User: identitydomain.User{ID: flowUserID, Role: identitydomain.RoleUser}}, nil
}

type flowRepository struct {
	sealedSecret crypto.Sealed
	accounts     []accountsdomain.Account
}

func (r *flowRepository) KeyCredentials(context.Context, int64, int64) (accountsdomain.APIKey, crypto.Sealed, error) {
	return accountsdomain.APIKey{ID: flowKeyID, UserID: flowUserID, PublicKey: flowKey, Status: accountsdomain.StatusValid}, r.sealedSecret, nil
}

func (r *flowRepository) Connect(_ context.Context, account accountsdomain.Account, seal func(int64) (crypto.Sealed, crypto.Sealed, error)) (accountsdomain.Account, bool, error) {
	account.ID = int64(len(r.accounts) + 1)
	if _, _, err := seal(account.ID); err != nil {
		return accountsdomain.Account{}, false, err
	}
	r.accounts = append(r.accounts, account)
	return account, false, nil
}

func (r *flowRepository) MarkRevoked(context.Context, int64) (bool, error) { return false, nil }

type flowStates struct {
	pending map[string]accountsapp.Pending
}

func (s flowStates) Save(_ context.Context, state string, pending accountsapp.Pending, _ time.Duration) error {
	s.pending[state] = pending
	return nil
}

func (s flowStates) Take(_ context.Context, state string) (accountsapp.Pending, error) {
	pending, ok := s.pending[state]
	if !ok {
		return accountsapp.Pending{}, accountsapp.ErrInvalidState
	}
	delete(s.pending, state)
	return pending, nil
}

// TestOAuthFlowThroughRouterAgainstFakeOAuth drives start, authorize and the
// browser callback through the real router, middleware and SPA fallback.
func TestOAuthFlowThroughRouterAgainstFakeOAuth(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html>app</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XFLICKR_WEB_DIR", root)
	ring, err := crypto.NewKeyring(map[string][]byte{"test": bytes.Repeat([]byte{3}, 32)}, "test")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := ring.Seal([]byte("consumer-secret"), accountsdomain.SecretAAD(flowKeyID))
	if err != nil {
		t.Fatal(err)
	}
	oauth := accountsflickr.NewFakeOAuth(flowKey, flowNSID, "jamal")
	t.Cleanup(oauth.Close)
	repository := &flowRepository{sealedSecret: sealed}
	states := flowStates{pending: map[string]accountsapp.Pending{}}
	csrf, err := identitydomain.NewOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	connect := accountshttp.ConnectHandler{
		Accounts: accountsapp.Connect{
			Repository:  repository,
			Authorizer:  accountsflickr.NewAuthorizer(oauth.Client(), oauth.Endpoints()),
			States:      states,
			Cipher:      ring,
			CallbackURL: flowCallback,
		},
		Principal: userIDFromPrincipal,
	}
	router := newAPIRouter(identityhttp.Handler{Version: "t", Sessions: flowSessions{}}, accountshttp.KeyHandler{}, connect, schedulehttp.Handler{})

	do := func(request *http.Request, signedIn bool) *httptest.ResponseRecorder {
		if signedIn {
			request.AddCookie(&http.Cookie{Name: identityhttp.SessionCookieName, Value: flowSession})
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	navigate := func(target string, signedIn bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.Header.Set("Accept", "text/html,application/xhtml+xml")
		return do(request, signedIn)
	}
	start := func() (requestToken, state string) {
		body := strings.NewReader(`{"key_id":4}`)
		request := httptest.NewRequest(http.MethodPost, "/flickr-accounts/connect", body)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json")
		request.Header.Set(identityhttp.CSRFHeaderName, csrf)
		request.AddCookie(&http.Cookie{Name: identityhttp.CSRFCookieName, Value: csrf})
		response := do(request, true)
		var started struct {
			AuthorizeURL string `json:"authorize_url"`
		}
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &started) != nil {
			t.Fatalf("connect status=%d body=%q", response.Code, response.Body.String())
		}
		authorize, err := url.Parse(started.AuthorizeURL)
		if err != nil || authorize.Path != accountsflickr.FakeAuthorizePath {
			t.Fatalf("authorize url = %q %v", started.AuthorizeURL, err)
		}
		requestToken = authorize.Query().Get("oauth_token")
		callback, err := url.Parse(oauth.Callback(requestToken))
		if err != nil || callback.Query().Get("state") == "" {
			t.Fatalf("callback = %q %v", oauth.Callback(requestToken), err)
		}
		return requestToken, callback.Query().Get("state")
	}
	callbackURL := func(token, state, verifier string) string {
		query := url.Values{"state": {state}, "oauth_token": {token}}
		if verifier != "" {
			query.Set("oauth_verifier", verifier)
		}
		return "/flickr-accounts/callback?" + query.Encode()
	}
	wantRedirect := func(response *httptest.ResponseRecorder, location string) {
		t.Helper()
		if response.Code != http.StatusFound || response.Header().Get("Location") != location {
			t.Fatalf("status=%d location=%q body=%q, want 302 %s", response.Code, response.Header().Get("Location"), response.Body.String(), location)
		}
	}
	wantPage := func(response *httptest.ResponseRecorder) {
		t.Helper()
		if response.Code != http.StatusOK || response.Body.String() != "<html>app</html>" {
			t.Fatalf("status=%d body=%q, want the SPA index", response.Code, response.Body.String())
		}
	}

	// Authorized: the browser callback stores the account and lands on the SPA.
	token, state := start()
	response := navigate(callbackURL(token, state, oauth.Approve(token)), true)
	wantRedirect(response, "/keys?connected=1")
	if len(repository.accounts) != 1 || repository.accounts[0].NSID != flowNSID || repository.accounts[0].UserID != flowUserID {
		t.Fatalf("accounts = %+v", repository.accounts)
	}
	wantPage(navigate("/keys?connected=1", true))
	wantPage(navigate("/admin/nodes", true))
	wantPage(navigate("/keys", false))

	// Reused state, denied authorization and an expired (missing) state each redirect with a code.
	wantRedirect(navigate(callbackURL(token, state, oauth.Approve(token)), true), "/keys?connect_error=invalid_state")
	token, state = start()
	wantRedirect(navigate(callbackURL(token, state, ""), true), "/keys?connect_error=authorization_denied")
	token, state = start()
	clear(states.pending)
	wantRedirect(navigate(callbackURL(token, state, oauth.Approve(token)), true), "/keys?connect_error=invalid_state")
	if len(repository.accounts) != 1 {
		t.Fatalf("a failed callback stored an account: %+v", repository.accounts)
	}

	// An expired browser session is sent to the SPA, not to a JSON 401 page.
	token, state = start()
	response = navigate(callbackURL(token, state, oauth.Approve(token)), false)
	wantRedirect(response, "/keys?connect_error=unauthenticated")
	if strings.Contains(response.Body.String(), "unauthenticated\"") {
		t.Fatalf("redirect carries a JSON body: %q", response.Body.String())
	}

	// The JSON contract is unchanged for API clients.
	token, state = start()
	request := httptest.NewRequest(http.MethodGet, callbackURL(token, state, oauth.Approve(token)), nil)
	request.Header.Set("Accept", "application/json")
	response = do(request, true)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), flowNSID) {
		t.Fatalf("json callback status=%d body=%q", response.Code, response.Body.String())
	}
}
