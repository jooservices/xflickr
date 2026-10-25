package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/accounts/app"
	"github.com/jooservices/xflickr/internal/accounts/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

type scriptedConnect struct {
	url      string
	account  domain.Account
	accounts []domain.Account
	err      error

	userID   int64
	keyID    int64
	state    string
	token    string
	verifier string
}

func (s *scriptedConnect) List(context.Context, int64) ([]domain.Account, error) {
	return s.accounts, s.err
}

func (s *scriptedConnect) Start(_ context.Context, userID, keyID int64) (string, error) {
	s.userID, s.keyID = userID, keyID
	return s.url, s.err
}

func (s *scriptedConnect) Complete(_ context.Context, userID int64, state, oauthToken, verifier string) (domain.Account, error) {
	s.userID, s.state, s.token, s.verifier = userID, state, oauthToken, verifier
	return s.account, s.err
}

func connectedAs(service ConnectService) ConnectHandler {
	return ConnectHandler{Accounts: service, Principal: func(context.Context) (int64, bool) { return 9, true }}
}

func TestConnectEndpointsRequireAUser(t *testing.T) {
	handler := ConnectHandler{Accounts: &scriptedConnect{}, Principal: func(context.Context) (int64, bool) { return 0, false }}
	if response, err := handler.ConnectFlickrAccount(t.Context(), gen.ConnectFlickrAccountRequestObject{Body: &gen.ConnectFlickrAccountRequest{KeyID: 4}}); err != nil || !codeIs(t, response, "unauthenticated") {
		t.Fatalf("connect = %T %v", response, err)
	}
	if response, err := handler.FlickrAccountCallback(t.Context(), gen.FlickrAccountCallbackRequestObject{}); err != nil || !codeIs(t, response, "unauthenticated") {
		t.Fatalf("callback = %T %v", response, err)
	}
	if response, err := handler.ListFlickrAccounts(t.Context(), gen.ListFlickrAccountsRequestObject{}); err != nil || !codeIs(t, response, "unauthenticated") {
		t.Fatalf("list accounts = %T %v", response, err)
	}
	if response, err := (ConnectHandler{}).ConnectFlickrAccount(nil, gen.ConnectFlickrAccountRequestObject{}); err != nil || !codeIs(t, response, "unauthenticated") {
		t.Fatalf("nil principal = %T %v", response, err)
	}
}

func TestConnectEndpointsRejectAMissingService(t *testing.T) {
	handler := connectedAs(nil)
	if _, err := handler.ConnectFlickrAccount(t.Context(), gen.ConnectFlickrAccountRequestObject{Body: &gen.ConnectFlickrAccountRequest{KeyID: 4}}); err == nil {
		t.Fatal("connect accepted a missing service")
	}
	if _, err := handler.FlickrAccountCallback(t.Context(), gen.FlickrAccountCallbackRequestObject{}); err == nil {
		t.Fatal("callback accepted a missing service")
	}
	if _, err := handler.ListFlickrAccounts(t.Context(), gen.ListFlickrAccountsRequestObject{}); err == nil {
		t.Fatal("list accounts accepted a missing service")
	}
}

func TestListFlickrAccountsReturnsOnlyOwnedMetadata(t *testing.T) {
	created := time.Unix(1_700_000_000, 0).UTC()
	service := &scriptedConnect{accounts: []domain.Account{{ID: 11, UserID: 9, KeyID: 4, NSID: "21207597@N07", Username: "jamal", Status: domain.AccountOK, CreatedAt: created}}}
	response, err := connectedAs(service).ListFlickrAccounts(t.Context(), gen.ListFlickrAccountsRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	body, ok := response.(gen.ListFlickrAccounts200JSONResponse)
	if !ok || len(body) != 1 || body[0].ID != 11 || body[0].Nsid != "21207597@N07" || body[0].KeyID != 4 || !body[0].CreatedAt.Equal(created) {
		t.Fatalf("response = %#v", response)
	}
	service.err = errors.New("database down")
	if _, err := connectedAs(service).ListFlickrAccounts(t.Context(), gen.ListFlickrAccountsRequestObject{}); !errors.Is(err, service.err) {
		t.Fatalf("list error = %v", err)
	}
}

func TestConnectReturnsTheAuthorizeURL(t *testing.T) {
	service := &scriptedConnect{url: "https://www.flickr.com/services/oauth/authorize?oauth_token=t&perms=read"}
	response, err := connectedAs(service).ConnectFlickrAccount(t.Context(), gen.ConnectFlickrAccountRequestObject{Body: &gen.ConnectFlickrAccountRequest{KeyID: 4}})
	if err != nil {
		t.Fatal(err)
	}
	body, ok := response.(gen.ConnectFlickrAccount200JSONResponse)
	if !ok || body.AuthorizeURL != service.url || service.userID != 9 || service.keyID != 4 {
		t.Fatalf("response = %#v service = %+v", response, service)
	}
	if response, err := connectedAs(service).ConnectFlickrAccount(t.Context(), gen.ConnectFlickrAccountRequestObject{}); err != nil || !codeIs(t, response, "invalid_input") {
		t.Fatalf("empty body = %T %v", response, err)
	}
}

func TestConnectMapsFailures(t *testing.T) {
	cases := map[error]string{
		app.ErrInvalidInput:     "invalid_input",
		app.ErrNotFound:         "not_found",
		app.ErrKeyNotUsable:     "key_not_usable",
		app.ErrOAuthRejected:    "oauth_rejected",
		app.ErrOAuthUnavailable: "flickr_unavailable",
	}
	for failure, code := range cases {
		response, err := connectedAs(&scriptedConnect{err: failure}).ConnectFlickrAccount(t.Context(), gen.ConnectFlickrAccountRequestObject{Body: &gen.ConnectFlickrAccountRequest{KeyID: 4}})
		if err != nil || !codeIs(t, response, code) {
			t.Errorf("%v = %T %v", failure, response, err)
		}
	}
	unknown := errors.New("database down")
	if _, err := connectedAs(&scriptedConnect{err: unknown}).ConnectFlickrAccount(t.Context(), gen.ConnectFlickrAccountRequestObject{Body: &gen.ConnectFlickrAccountRequest{KeyID: 4}}); !errors.Is(err, unknown) {
		t.Fatalf("unknown error = %v", err)
	}
}

func TestCallbackReturnsTheAccountWithoutTokens(t *testing.T) {
	created := time.Unix(1_700_000_000, 0).UTC()
	service := &scriptedConnect{account: domain.Account{ID: 11, UserID: 9, KeyID: 4, NSID: "21207597@N07", Username: "jamal", Status: domain.AccountOK, CreatedAt: created}}
	verifier := "verifier-1"
	response, err := connectedAs(service).FlickrAccountCallback(t.Context(), gen.FlickrAccountCallbackRequestObject{Params: gen.FlickrAccountCallbackParams{State: "s", OauthToken: "t", OauthVerifier: &verifier}})
	if err != nil {
		t.Fatal(err)
	}
	body, ok := response.(gen.FlickrAccountCallback200JSONResponse)
	if !ok || body.ID != 11 || body.KeyID != 4 || body.Nsid != "21207597@N07" || body.Username != "jamal" || body.Status != gen.FlickrAccountStatusOk || !body.CreatedAt.Equal(created) {
		t.Fatalf("response = %#v", response)
	}
	if service.userID != 9 || service.state != "s" || service.token != "t" || service.verifier != verifier {
		t.Fatalf("service = %+v", service)
	}
	if text := encoded(t, response); strings.Contains(text, "token") || strings.Contains(text, "secret") {
		t.Fatalf("response leaks token material: %s", text)
	}

	// The member declined: Flickr sends no verifier.
	service.err = app.ErrAuthorizationDenied
	response, err = connectedAs(service).FlickrAccountCallback(t.Context(), gen.FlickrAccountCallbackRequestObject{Params: gen.FlickrAccountCallbackParams{State: "s", OauthToken: "t"}})
	if err != nil || !codeIs(t, response, "authorization_denied") || service.verifier != "" {
		t.Fatalf("denied = %T %v verifier=%q", response, err, service.verifier)
	}
}

func TestCallbackMapsFailures(t *testing.T) {
	cases := map[error]string{
		app.ErrInvalidState:     "invalid_state",
		app.ErrKeyNotUsable:     "key_not_usable",
		app.ErrOAuthRejected:    "oauth_rejected",
		app.ErrOAuthUnavailable: "flickr_unavailable",
	}
	for failure, code := range cases {
		response, err := connectedAs(&scriptedConnect{err: failure}).FlickrAccountCallback(t.Context(), gen.FlickrAccountCallbackRequestObject{})
		if err != nil || !codeIs(t, response, code) {
			t.Errorf("%v = %T %v", failure, response, err)
		}
	}
	for _, failure := range []error{errors.New("database down"), app.ErrNotFound} {
		if _, err := connectedAs(&scriptedConnect{err: failure}).FlickrAccountCallback(t.Context(), gen.FlickrAccountCallbackRequestObject{}); !errors.Is(err, failure) {
			t.Errorf("%v error = %v", failure, err)
		}
	}
}

func TestBrowserCallbackRedirectsAndJSONCallbackKeepsContract(t *testing.T) {
	service := &scriptedConnect{account: domain.Account{ID: 11, UserID: 9, Username: "jamal"}}
	ctx := WithRequestAccept(t.Context(), "text/html,application/xhtml+xml")
	response, err := connectedAs(service).FlickrAccountCallback(ctx, gen.FlickrAccountCallbackRequestObject{Params: gen.FlickrAccountCallbackParams{State: "s", OauthToken: "t"}})
	if err != nil {
		t.Fatal(err)
	}
	checkRedirect := func(got gen.FlickrAccountCallbackResponseObject, want string) {
		t.Helper()
		writer := httptest.NewRecorder()
		if err := got.VisitFlickrAccountCallbackResponse(writer); err != nil {
			t.Fatal(err)
		}
		if writer.Code != http.StatusFound || writer.Header().Get("Location") != want {
			t.Fatalf("redirect status=%d location=%q, want 302 %q", writer.Code, writer.Header().Get("Location"), want)
		}
	}
	checkRedirect(response, "/keys?connected=1")

	service.err = app.ErrInvalidState
	response, err = connectedAs(service).FlickrAccountCallback(ctx, gen.FlickrAccountCallbackRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	checkRedirect(response, "/keys?connect_error=invalid_state")

	response, err = connectedAs(service).FlickrAccountCallback(t.Context(), gen.FlickrAccountCallbackRequestObject{})
	if err != nil || !codeIs(t, response, "invalid_state") {
		t.Fatalf("JSON callback = %T %v", response, err)
	}
}
