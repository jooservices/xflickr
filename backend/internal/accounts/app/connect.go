package app

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/jooservices/xflickr/internal/accounts/domain"
	"github.com/jooservices/xflickr/internal/platform/crypto"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

// StateTTL is how long a started authorization stays valid.
const StateTTL = 10 * time.Minute

var (
	// ErrInvalidState means the callback state is unknown, expired, already
	// used, belongs to another user, or does not match the request token.
	ErrInvalidState = errors.New("accounts: oauth state is invalid")
	// ErrAuthorizationDenied means the member did not grant access on Flickr.
	ErrAuthorizationDenied = errors.New("accounts: flickr authorization was denied")
	// ErrKeyNotUsable means the key is not valid, so it cannot authorize accounts.
	ErrKeyNotUsable = errors.New("accounts: api key cannot authorize accounts")
	// ErrOAuthRejected means Flickr refused the authorization request.
	ErrOAuthRejected = errors.New("accounts: flickr rejected the authorization")
	// ErrOAuthUnavailable means the Flickr call did not complete.
	ErrOAuthUnavailable = errors.New("accounts: flickr authorization is unavailable")
)

// Consumer is the API key and secret that sign OAuth requests for one key.
type Consumer struct {
	Key    string
	Secret string
}

// RequestToken is the temporary credential returned by Flickr, with the page
// where the member grants access.
type RequestToken struct {
	Token        string
	Secret       string
	AuthorizeURL string
}

// Grant is the access token Flickr issues after the member approves.
type Grant struct {
	Token    string
	Secret   string
	NSID     string
	Username string
}

// Authorizer runs the Flickr OAuth 1.0a exchange. It returns ErrOAuthRejected
// when Flickr refuses and ErrOAuthUnavailable when the call did not complete.
type Authorizer interface {
	RequestToken(ctx context.Context, consumer Consumer, callbackURL string) (RequestToken, error)
	AccessToken(ctx context.Context, consumer Consumer, request RequestToken, verifier string) (Grant, error)
}

// Pending is one started authorization. The request secret is sealed with
// domain.RequestSecretAAD(state) so Redis never holds it in plaintext.
type Pending struct {
	UserID        int64         `json:"user_id"`
	KeyID         int64         `json:"key_id"`
	RequestToken  string        `json:"request_token"`
	RequestSecret crypto.Sealed `json:"request_secret"`
}

// StateStore keeps started authorizations until the callback or expiry.
type StateStore interface {
	Save(ctx context.Context, state string, pending Pending, ttl time.Duration) error
	// Take returns and deletes the entry, so a state works once. It returns
	// ErrInvalidState when the state is unknown or expired.
	Take(ctx context.Context, state string) (Pending, error)
}

// Opener opens a sealed value with its associated data.
type Opener interface {
	Open(sealed crypto.Sealed, aad []byte) ([]byte, error)
}

// Cipher seals and opens secrets. The platform keyring implements it.
type Cipher interface {
	Sealer
	Opener
}

// AccountRepository stores connected accounts and publishes their events in
// the same transaction.
type AccountRepository interface {
	// List returns connected accounts owned by the user without token material.
	ListAccounts(context.Context, int64) ([]domain.Account, error)
	// KeyCredentials returns the user's key with its sealed secret. It
	// returns ErrNotFound when the key is not the user's.
	KeyCredentials(ctx context.Context, userID, keyID int64) (domain.APIKey, crypto.Sealed, error)
	// Connect inserts the account, or refreshes the tokens and sets it ok when
	// the user already connected that nsid. It publishes
	// domain.ConnectedTopic. reconnected reports a refreshed account.
	Connect(ctx context.Context, account domain.Account, seal func(id int64) (token, secret crypto.Sealed, err error)) (stored domain.Account, reconnected bool, err error)
	// MarkRevoked sets the account revoked and publishes domain.RevokedTopic.
	// It reports false and publishes nothing when the account is missing or
	// already revoked.
	MarkRevoked(ctx context.Context, accountID int64) (bool, error)
}

// Connect is the Flickr account connection use case set.
type Connect struct {
	Repository  AccountRepository
	Authorizer  Authorizer
	States      StateStore
	Cipher      Cipher
	CallbackURL string
	// NewState returns a fresh unguessable state. Nil uses 256 random bits.
	NewState func() (string, error)
}

// List returns the signed-in user's connected Flickr accounts.
func (c Connect) List(ctx context.Context, userID int64) ([]domain.Account, error) {
	if c.Repository == nil {
		return nil, errors.New("accounts: connect repository is required")
	}
	if ctx == nil {
		return nil, errors.New("accounts: context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if userID <= 0 {
		return nil, ErrInvalidInput
	}
	return c.Repository.ListAccounts(ctx, userID)
}

// Start asks Flickr for a request token for the user's key, keeps it for
// StateTTL, and returns the Flickr authorize URL. The callback URL carries the
// state, which Complete checks.
func (c Connect) Start(ctx context.Context, userID, keyID int64) (string, error) {
	if err := c.ready(ctx); err != nil {
		return "", err
	}
	if userID <= 0 || keyID <= 0 {
		return "", ErrInvalidInput
	}
	consumer, err := c.consumer(ctx, userID, keyID)
	if err != nil {
		return "", err
	}
	state, err := c.newState()
	if err != nil {
		return "", err
	}
	callback, err := callbackWithState(c.CallbackURL, state)
	if err != nil {
		return "", err
	}
	ownerID := userID
	ctx = flickrclient.WithCallContext(ctx, flickrclient.CallContext{KeyID: keyID, UserID: &ownerID, Lane: "interactive", Facet: "oauth", Attempt: 1})
	request, err := c.Authorizer.RequestToken(ctx, consumer, callback)
	if err != nil {
		return "", err
	}
	secret := []byte(request.Secret)
	sealed, err := c.Cipher.Seal(secret, domain.RequestSecretAAD(state))
	wipeBytes(secret)
	if err != nil {
		return "", err
	}
	if err := c.States.Save(ctx, state, Pending{
		UserID:        userID,
		KeyID:         keyID,
		RequestToken:  request.Token,
		RequestSecret: sealed,
	}, StateTTL); err != nil {
		return "", err
	}
	return request.AuthorizeURL, nil
}

// Complete finishes the flow. The state is consumed first, so it works once
// even when a later step fails. It must belong to the same user and the
// returned oauth_token must be the request token that was issued.
func (c Connect) Complete(ctx context.Context, userID int64, state, oauthToken, verifier string) (domain.Account, error) {
	if err := c.ready(ctx); err != nil {
		return domain.Account{}, err
	}
	state = strings.TrimSpace(state)
	if userID <= 0 || state == "" {
		return domain.Account{}, ErrInvalidState
	}
	pending, err := c.States.Take(ctx, state)
	if err != nil {
		return domain.Account{}, err
	}
	if pending.UserID != userID || subtle.ConstantTimeCompare([]byte(pending.RequestToken), []byte(oauthToken)) != 1 {
		return domain.Account{}, ErrInvalidState
	}
	verifier = strings.TrimSpace(verifier)
	if verifier == "" {
		return domain.Account{}, ErrAuthorizationDenied
	}
	requestSecret, err := c.Cipher.Open(pending.RequestSecret, domain.RequestSecretAAD(state))
	if err != nil {
		return domain.Account{}, ErrInvalidState
	}
	defer wipeBytes(requestSecret)
	consumer, err := c.consumer(ctx, userID, pending.KeyID)
	if err != nil {
		return domain.Account{}, err
	}
	ownerID := userID
	ctx = flickrclient.WithCallContext(ctx, flickrclient.CallContext{KeyID: pending.KeyID, UserID: &ownerID, Lane: "interactive", Facet: "oauth", Attempt: 1})
	grant, err := c.Authorizer.AccessToken(ctx, consumer, RequestToken{Token: pending.RequestToken, Secret: string(requestSecret)}, verifier)
	if err != nil {
		return domain.Account{}, err
	}
	nsid, err := domain.NormalizeNSID(grant.NSID)
	if err != nil {
		return domain.Account{}, ErrOAuthRejected
	}
	account, _, err := c.Repository.Connect(ctx, domain.Account{
		UserID:   userID,
		KeyID:    pending.KeyID,
		NSID:     nsid,
		Username: grant.Username,
		Status:   domain.AccountOK,
	}, func(id int64) (crypto.Sealed, crypto.Sealed, error) {
		return c.sealGrant(grant, id)
	})
	return account, err
}

// ReportCallError records the outcome of a Flickr call made with an account's
// token. A token error from Flickr flips the account to revoked and publishes
// account.revoked once. It reports whether the account was revoked by this
// call. Other errors are ignored.
func (c Connect) ReportCallError(ctx context.Context, accountID int64, callErr error) (bool, error) {
	if c.Repository == nil {
		return false, errors.New("accounts: connect service is incomplete")
	}
	if !errors.Is(callErr, flickrclient.ErrTokenRevoked) {
		return false, nil
	}
	return c.Repository.MarkRevoked(ctx, accountID)
}

func (c Connect) consumer(ctx context.Context, userID, keyID int64) (Consumer, error) {
	key, sealed, err := c.Repository.KeyCredentials(ctx, userID, keyID)
	if err != nil {
		return Consumer{}, err
	}
	if key.Status != domain.StatusValid {
		return Consumer{}, ErrKeyNotUsable
	}
	secret, err := c.Cipher.Open(sealed, domain.SecretAAD(key.ID))
	if err != nil {
		return Consumer{}, err
	}
	defer wipeBytes(secret)
	return Consumer{Key: key.PublicKey, Secret: string(secret)}, nil
}

func (c Connect) sealGrant(grant Grant, id int64) (crypto.Sealed, crypto.Sealed, error) {
	token := []byte(grant.Token)
	secret := []byte(grant.Secret)
	defer wipeBytes(token)
	defer wipeBytes(secret)
	sealedToken, err := c.Cipher.Seal(token, domain.TokenAAD(id))
	if err != nil {
		return crypto.Sealed{}, crypto.Sealed{}, err
	}
	sealedSecret, err := c.Cipher.Seal(secret, domain.TokenSecretAAD(id))
	if err != nil {
		return crypto.Sealed{}, crypto.Sealed{}, err
	}
	return sealedToken, sealedSecret, nil
}

func (c Connect) newState() (string, error) {
	if c.NewState != nil {
		return c.NewState()
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (c Connect) ready(ctx context.Context) error {
	if c.Repository == nil || c.Authorizer == nil || c.States == nil || c.Cipher == nil || strings.TrimSpace(c.CallbackURL) == "" {
		return errors.New("accounts: connect service is incomplete")
	}
	if ctx == nil {
		return errors.New("accounts: context is required")
	}
	return ctx.Err()
}

func callbackWithState(callback, state string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(callback))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("accounts: oauth callback URL is invalid")
	}
	query := parsed.Query()
	query.Set("state", state)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}
