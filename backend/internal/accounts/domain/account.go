package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// AccountOK is a connected Flickr account whose token can be used.
	AccountOK AccountStatus = "ok"
	// AccountReconnectRequired is set when the key secret changes. The member
	// must authorize again before the stored token can be used.
	AccountReconnectRequired AccountStatus = "reconnect_required"
	// AccountRevoked is set when Flickr reports that the token is no longer valid.
	// Authorizing the account again makes it ok.
	AccountRevoked AccountStatus = "revoked"

	// ConnectedTopic is the outbox topic published when an account is connected
	// or reconnected. Crawl planning consumes it.
	ConnectedTopic = "flickr_account.connected"
	// RevokedTopic is the outbox topic published when Flickr rejects a token.
	RevokedTopic = "account.revoked"

	// InterestPerson is the interest kind registered for a connected account.
	InterestPerson = "person"
	// ConnectedDepth is the crawl depth of a connected account's own interest.
	ConnectedDepth = 0
)

// ErrInvalidAccountTransition means an account status change is not allowed.
var ErrInvalidAccountTransition = errors.New("accounts: invalid account status transition")

// AccountStatus is the lifecycle of one connected Flickr account.
type AccountStatus string

// Valid reports whether the status is one of the stored values.
func (s AccountStatus) Valid() bool {
	switch s {
	case AccountOK, AccountReconnectRequired, AccountRevoked:
		return true
	default:
		return false
	}
}

// Transition reports whether an account may move from the current status to
// next. Repeating the current status is allowed. A revoked or reconnect_required
// account becomes ok only by authorizing again.
func (s AccountStatus) Transition(next AccountStatus) error {
	if !s.Valid() || !next.Valid() {
		return ErrInvalidAccountTransition
	}
	if s == next {
		return nil
	}
	if s == AccountOK && next == AccountReconnectRequired {
		return nil
	}
	if next == AccountRevoked || next == AccountOK {
		return nil
	}
	return ErrInvalidAccountTransition
}

// Account is one Flickr account connected through a user's API key. The
// tokens are absent: they are sealed before storage and never returned.
type Account struct {
	ID        int64
	UserID    int64
	KeyID     int64
	NSID      string
	Username  string
	Status    AccountStatus
	CreatedAt time.Time
}

// WithStatus returns a copy of the account after a legal status transition.
func (a Account) WithStatus(next AccountStatus) (Account, error) {
	if err := a.Status.Transition(next); err != nil {
		return Account{}, err
	}
	a.Status = next
	return a, nil
}

// TokenAAD is the associated data for sealing the access token of an account.
func TokenAAD(id int64) []byte {
	return []byte(fmt.Sprintf("flickr_accounts:%d:token", id))
}

// TokenSecretAAD is the associated data for sealing the access token secret.
func TokenSecretAAD(id int64) []byte {
	return []byte(fmt.Sprintf("flickr_accounts:%d:secret", id))
}

// RequestSecretAAD is the associated data for the request token secret kept in
// Redis. It is bound to the state so the value cannot be moved to another state.
func RequestSecretAAD(state string) []byte {
	return []byte("flickr_oauth_state:" + state)
}

// NormalizeNSID trims a Flickr user id and rejects empty or oversized values.
func NormalizeNSID(nsid string) (string, error) {
	trimmed := strings.TrimSpace(nsid)
	if trimmed == "" || len(trimmed) > CredentialMaxLength {
		return "", ErrInvalidCredential
	}
	return trimmed, nil
}

// Connected is the JSON body of ConnectedTopic. It identifies the account and
// the depth 0 person interest to register, and carries no token material.
// Reconnected is true when the member authorized an account that already existed.
type Connected struct {
	AccountID   int64  `json:"account_id"`
	UserID      int64  `json:"user_id"`
	KeyID       int64  `json:"key_id"`
	NSID        string `json:"nsid"`
	Username    string `json:"username"`
	Interest    string `json:"interest"`
	Depth       int    `json:"depth"`
	Reconnected bool   `json:"reconnected"`
}

// Revoked is the JSON body of RevokedTopic. It carries no token material.
type Revoked struct {
	AccountID int64  `json:"account_id"`
	UserID    int64  `json:"user_id"`
	KeyID     int64  `json:"key_id"`
	NSID      string `json:"nsid"`
}
