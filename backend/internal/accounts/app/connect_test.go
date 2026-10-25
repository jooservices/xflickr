package app

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/accounts/domain"
	"github.com/jooservices/xflickr/internal/platform/crypto"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

const (
	connectCallback = "http://127.0.0.1:8080/flickr-accounts/callback"
	connectState    = "state-one"
)

func TestStartStoresSealedRequestTokenAndReturnsAuthorizeURL(t *testing.T) {
	fixture := newConnectFixture(t)
	authorizeURL, err := fixture.connect.Start(t.Context(), 7, 4)
	if err != nil {
		t.Fatal(err)
	}
	if authorizeURL != "https://flickr.test/authorize?oauth_token=request-token" {
		t.Fatalf("authorize url = %q", authorizeURL)
	}
	if fixture.authorizer.consumer != (Consumer{Key: testPublic, Secret: testSecret}) {
		t.Fatalf("consumer = %+v", fixture.authorizer.consumer)
	}
	callback, err := url.Parse(fixture.authorizer.callback)
	if err != nil || callback.Query().Get("state") != connectState || callback.Path != "/flickr-accounts/callback" {
		t.Fatalf("callback = %q %v", fixture.authorizer.callback, err)
	}
	pending, ok := fixture.states.saved[connectState]
	if !ok || fixture.states.ttl != 10*time.Minute {
		t.Fatalf("saved = %+v ttl = %s", fixture.states.saved, fixture.states.ttl)
	}
	if pending.UserID != 7 || pending.KeyID != 4 || pending.RequestToken != "request-token" {
		t.Fatalf("pending = %+v", pending)
	}
	if bytes.Contains(pending.RequestSecret.Ciphertext, []byte("request-secret")) {
		t.Fatal("request secret was stored in plaintext")
	}
	opened, err := fixture.cipher.Open(pending.RequestSecret, domain.RequestSecretAAD(connectState))
	if err != nil || string(opened) != "request-secret" {
		t.Fatalf("opened = %q %v", opened, err)
	}
}

func TestListReturnsAccountsOwnedByUser(t *testing.T) {
	fixture := newConnectFixture(t)
	want := []domain.Account{{ID: 11, UserID: 7, KeyID: 4, NSID: "21207597@N07", Username: "jamal", Status: domain.AccountOK}}
	fixture.repo.accounts = want
	got, err := fixture.connect.List(t.Context(), 7)
	if err != nil || len(got) != 1 || got[0] != want[0] {
		t.Fatalf("List() = %#v, %v", got, err)
	}
	if _, err = fixture.connect.List(t.Context(), 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid user list error = %v", err)
	}
}

func TestStartRejectsBadInputAndUnusableKeys(t *testing.T) {
	fixture := newConnectFixture(t)
	if _, err := fixture.connect.Start(t.Context(), 0, 4); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("zero user = %v", err)
	}
	if _, err := fixture.connect.Start(t.Context(), 7, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("zero key = %v", err)
	}
	fixture.repo.key.Status = domain.StatusInvalid
	if _, err := fixture.connect.Start(t.Context(), 7, 4); !errors.Is(err, ErrKeyNotUsable) {
		t.Fatalf("invalid key = %v", err)
	}
	fixture.repo.key.Status = domain.StatusValid
	fixture.repo.keyErr = ErrNotFound
	if _, err := fixture.connect.Start(t.Context(), 7, 4); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing key = %v", err)
	}
	if fixture.authorizer.requests != 0 || len(fixture.states.saved) != 0 {
		t.Fatalf("flickr calls = %d saved = %d", fixture.authorizer.requests, len(fixture.states.saved))
	}
}

func TestStartReportsFailures(t *testing.T) {
	fixture := newConnectFixture(t)
	fixture.authorizer.requestErr = ErrOAuthRejected
	if _, err := fixture.connect.Start(t.Context(), 7, 4); !errors.Is(err, ErrOAuthRejected) {
		t.Fatalf("request token error = %v", err)
	}
	fixture.authorizer.requestErr = nil
	fixture.states.saveErr = errors.New("redis down")
	if _, err := fixture.connect.Start(t.Context(), 7, 4); err == nil {
		t.Fatal("save failure was ignored")
	}
	fixture.states.saveErr = nil
	fixture.connect.NewState = func() (string, error) { return "", errors.New("no entropy") }
	if _, err := fixture.connect.Start(t.Context(), 7, 4); err == nil {
		t.Fatal("state failure was ignored")
	}
	fixture.connect.NewState = nil
	fixture.connect.CallbackURL = "not a url"
	if _, err := fixture.connect.Start(t.Context(), 7, 4); err == nil {
		t.Fatal("bad callback was accepted")
	}
	fixture.connect.CallbackURL = connectCallback
	fixture.cipher.sealErr = errors.New("seal failed")
	if _, err := fixture.connect.Start(t.Context(), 7, 4); err == nil {
		t.Fatal("seal failure was ignored")
	}
}

func TestStartGeneratesUnguessableStates(t *testing.T) {
	fixture := newConnectFixture(t)
	fixture.connect.NewState = nil
	if _, err := fixture.connect.Start(t.Context(), 7, 4); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.connect.Start(t.Context(), 7, 4); err != nil {
		t.Fatal(err)
	}
	if len(fixture.states.saved) != 2 {
		t.Fatalf("states = %d", len(fixture.states.saved))
	}
	for state := range fixture.states.saved {
		if len(state) < 43 {
			t.Fatalf("state %q is shorter than 256 bits", state)
		}
	}
}

func TestCompleteStoresSealedTokensAndConsumesState(t *testing.T) {
	fixture := newConnectFixture(t)
	if _, err := fixture.connect.Start(t.Context(), 7, 4); err != nil {
		t.Fatal(err)
	}
	account, err := fixture.connect.Complete(t.Context(), 7, connectState, "request-token", "verifier-1")
	if err != nil {
		t.Fatal(err)
	}
	if account.ID != 11 || account.UserID != 7 || account.KeyID != 4 || account.NSID != "21207597@N07" || account.Status != domain.AccountOK {
		t.Fatalf("account = %+v", account)
	}
	if fixture.authorizer.verifier != "verifier-1" || fixture.authorizer.request.Secret != "request-secret" {
		t.Fatalf("access call = %+v", fixture.authorizer)
	}
	token, err := fixture.cipher.Open(fixture.repo.sealedToken, domain.TokenAAD(11))
	if err != nil || string(token) != "access-token" {
		t.Fatalf("token = %q %v", token, err)
	}
	secret, err := fixture.cipher.Open(fixture.repo.sealedSecret, domain.TokenSecretAAD(11))
	if err != nil || string(secret) != "access-secret" {
		t.Fatalf("secret = %q %v", secret, err)
	}
	if _, err := fixture.connect.Complete(t.Context(), 7, connectState, "request-token", "verifier-1"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("state reuse = %v", err)
	}
}

func TestCompleteRejectsForgedOrMismatchedState(t *testing.T) {
	cases := []struct {
		name    string
		user    int64
		state   string
		token   string
		wantErr error
	}{
		{"empty state", 7, " ", "request-token", ErrInvalidState},
		{"unknown state", 7, "other", "request-token", ErrInvalidState},
		{"other user", 8, connectState, "request-token", ErrInvalidState},
		{"other request token", 7, connectState, "request-token-2", ErrInvalidState},
		{"no user", 0, connectState, "request-token", ErrInvalidState},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newConnectFixture(t)
			if _, err := fixture.connect.Start(t.Context(), 7, 4); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.connect.Complete(t.Context(), tc.user, tc.state, tc.token, "verifier-1"); !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v", err)
			}
			if fixture.authorizer.accessCalls != 0 || fixture.repo.connects != 0 {
				t.Fatalf("flickr calls = %d stored = %d", fixture.authorizer.accessCalls, fixture.repo.connects)
			}
		})
	}
}

func TestCompleteHandlesDeniedAndFailedExchanges(t *testing.T) {
	fixture := newConnectFixture(t)
	if _, err := fixture.connect.Start(t.Context(), 7, 4); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.connect.Complete(t.Context(), 7, connectState, "request-token", "  "); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("denied = %v", err)
	}
	if _, ok := fixture.states.saved[connectState]; ok {
		t.Fatal("denied authorization left the state usable")
	}

	for name, mutate := range map[string]func(*connectFixture){
		"access rejected":  func(f *connectFixture) { f.authorizer.accessErr = ErrOAuthRejected },
		"key gone":         func(f *connectFixture) { f.repo.keyErr = ErrNotFound },
		"key invalid":      func(f *connectFixture) { f.repo.key.Status = domain.StatusInvalid },
		"empty nsid":       func(f *connectFixture) { f.authorizer.grant.NSID = " " },
		"store failed":     func(f *connectFixture) { f.repo.connectErr = errors.New("db down") },
		"tampered request": func(f *connectFixture) { f.states.tamper = true },
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newConnectFixture(t)
			if _, err := fixture.connect.Start(t.Context(), 7, 4); err != nil {
				t.Fatal(err)
			}
			mutate(fixture)
			if _, err := fixture.connect.Complete(t.Context(), 7, connectState, "request-token", "verifier-1"); err == nil {
				t.Fatal("failure was ignored")
			}
		})
	}
}

func TestReportCallErrorRevokesOnlyOnTokenErrors(t *testing.T) {
	fixture := newConnectFixture(t)
	tokenErr := &flickrclient.CallError{Kind: flickrclient.ErrTokenRevoked, Code: 98}
	revoked, err := fixture.connect.ReportCallError(t.Context(), 11, tokenErr)
	if err != nil || !revoked || fixture.repo.revoked[11] != 1 {
		t.Fatalf("revoked = %v %v calls = %v", revoked, err, fixture.repo.revoked)
	}
	for _, other := range []error{nil, errors.New("timeout"), flickrclient.ErrQuotaExceeded, &flickrclient.CallError{Kind: flickrclient.ErrInvalidKey, Code: 100}} {
		revoked, err := fixture.connect.ReportCallError(t.Context(), 11, other)
		if err != nil || revoked {
			t.Fatalf("%v revoked = %v %v", other, revoked, err)
		}
	}
	if fixture.repo.revoked[11] != 1 {
		t.Fatalf("revoke calls = %d", fixture.repo.revoked[11])
	}
	fixture.repo.revokeChanged = false
	if revoked, err := fixture.connect.ReportCallError(t.Context(), 11, tokenErr); err != nil || revoked {
		t.Fatalf("already revoked = %v %v", revoked, err)
	}
	fixture.repo.revokeErr = errors.New("db down")
	if _, err := fixture.connect.ReportCallError(t.Context(), 11, tokenErr); err == nil {
		t.Fatal("repository failure was ignored")
	}
	if _, err := (Connect{}).ReportCallError(t.Context(), 11, tokenErr); err == nil {
		t.Fatal("incomplete service was accepted")
	}
}

func TestConnectRequiresACompleteService(t *testing.T) {
	fixture := newConnectFixture(t)
	incomplete := []Connect{{}, fixture.connect, fixture.connect, fixture.connect, fixture.connect, fixture.connect}
	incomplete[1].Repository = nil
	incomplete[2].Authorizer = nil
	incomplete[3].States = nil
	incomplete[4].Cipher = nil
	incomplete[5].CallbackURL = " "
	for index, connect := range incomplete {
		if _, err := connect.Start(t.Context(), 7, 4); err == nil {
			t.Errorf("service %d: start accepted an incomplete service", index)
		}
		if _, err := connect.Complete(t.Context(), 7, connectState, "t", "v"); err == nil {
			t.Errorf("service %d: complete accepted an incomplete service", index)
		}
	}
	if _, err := fixture.connect.Start(nil, 7, 4); err == nil { //nolint:staticcheck // a nil context is the case under test
		t.Fatal("nil context was accepted")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := fixture.connect.Start(canceled, 7, 4); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled = %v", err)
	}
}

type connectFixture struct {
	connect    Connect
	repo       *fakeAccountRepo
	authorizer *fakeAuthorizer
	states     *fakeStates
	cipher     *testCipher
}

func newConnectFixture(t *testing.T) *connectFixture {
	t.Helper()
	ring, err := crypto.NewKeyring(map[string][]byte{"test": bytes.Repeat([]byte{3}, 32)}, "test")
	if err != nil {
		t.Fatal(err)
	}
	cipher := &testCipher{Keyring: ring}
	sealedSecret, err := ring.Seal([]byte(testSecret), domain.SecretAAD(4))
	if err != nil {
		t.Fatal(err)
	}
	fixture := &connectFixture{
		repo: &fakeAccountRepo{
			key:           domain.APIKey{ID: 4, UserID: 7, PublicKey: testPublic, Status: domain.StatusValid},
			sealedKey:     sealedSecret,
			revoked:       map[int64]int{},
			revokeChanged: true,
		},
		authorizer: &fakeAuthorizer{
			token: RequestToken{Token: "request-token", Secret: "request-secret", AuthorizeURL: "https://flickr.test/authorize?oauth_token=request-token"},
			grant: Grant{Token: "access-token", Secret: "access-secret", NSID: "21207597@N07", Username: "jamal"},
		},
		states: &fakeStates{saved: map[string]Pending{}},
		cipher: cipher,
	}
	fixture.connect = Connect{
		Repository:  fixture.repo,
		Authorizer:  fixture.authorizer,
		States:      fixture.states,
		Cipher:      cipher,
		CallbackURL: connectCallback,
		NewState:    func() (string, error) { return connectState, nil },
	}
	return fixture
}

type testCipher struct {
	*crypto.Keyring
	sealErr error
}

func (c *testCipher) Seal(plaintext, aad []byte) (crypto.Sealed, error) {
	if c.sealErr != nil {
		return crypto.Sealed{}, c.sealErr
	}
	return c.Keyring.Seal(plaintext, aad)
}

type fakeAccountRepo struct {
	key           domain.APIKey
	accounts      []domain.Account
	sealedKey     crypto.Sealed
	keyErr        error
	connectErr    error
	connects      int
	sealedToken   crypto.Sealed
	sealedSecret  crypto.Sealed
	revoked       map[int64]int
	revokeChanged bool
	revokeErr     error
}

func (r *fakeAccountRepo) ListAccounts(context.Context, int64) ([]domain.Account, error) {
	return r.accounts, nil
}

func (r *fakeAccountRepo) KeyCredentials(context.Context, int64, int64) (domain.APIKey, crypto.Sealed, error) {
	return r.key, r.sealedKey, r.keyErr
}

func (r *fakeAccountRepo) Connect(_ context.Context, account domain.Account, seal func(int64) (crypto.Sealed, crypto.Sealed, error)) (domain.Account, bool, error) {
	if r.connectErr != nil {
		return domain.Account{}, false, r.connectErr
	}
	r.connects++
	account.ID = 11
	var err error
	r.sealedToken, r.sealedSecret, err = seal(account.ID)
	return account, false, err
}

func (r *fakeAccountRepo) MarkRevoked(_ context.Context, id int64) (bool, error) {
	r.revoked[id]++
	return r.revokeChanged, r.revokeErr
}

type fakeAuthorizer struct {
	token       RequestToken
	grant       Grant
	requestErr  error
	accessErr   error
	requests    int
	accessCalls int
	consumer    Consumer
	callback    string
	request     RequestToken
	verifier    string
}

func (a *fakeAuthorizer) RequestToken(_ context.Context, consumer Consumer, callbackURL string) (RequestToken, error) {
	a.requests++
	a.consumer, a.callback = consumer, callbackURL
	return a.token, a.requestErr
}

func (a *fakeAuthorizer) AccessToken(_ context.Context, consumer Consumer, request RequestToken, verifier string) (Grant, error) {
	a.accessCalls++
	a.consumer, a.request, a.verifier = consumer, request, verifier
	return a.grant, a.accessErr
}

type fakeStates struct {
	saved   map[string]Pending
	ttl     time.Duration
	saveErr error
	tamper  bool
}

func (s *fakeStates) Save(_ context.Context, state string, pending Pending, ttl time.Duration) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saved[state], s.ttl = pending, ttl
	return nil
}

func (s *fakeStates) Take(_ context.Context, state string) (Pending, error) {
	pending, ok := s.saved[state]
	if !ok {
		return Pending{}, ErrInvalidState
	}
	delete(s.saved, state)
	if s.tamper {
		pending.RequestSecret.Ciphertext = append([]byte(strings.Repeat("x", 3)), pending.RequestSecret.Ciphertext...)
	}
	return pending, nil
}
