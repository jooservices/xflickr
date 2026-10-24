package app

import (
	"context"
	"errors"
	"time"

	"github.com/jooservices/xflickr/internal/identity/domain"
)

// ErrUnauthenticated means the request has no usable session.
var ErrUnauthenticated = errors.New("identity: authentication required")

// Authenticated is the principal resolved from a session cookie.
type Authenticated struct {
	User      domain.User
	Session   domain.Session
	TokenHash []byte
	Slid      bool
}

// SessionRepository stores hashed session ids.
type SessionRepository interface {
	InsertSession(context.Context, int64, []byte, time.Time, time.Time) (domain.Session, error)
	FindSessionByHash(context.Context, []byte, time.Time) (domain.Session, error)
	TouchSession(context.Context, []byte, time.Time, time.Time) (bool, error)
	DeleteOtherSessions(context.Context, int64, []byte) error
	DeleteSessionByHash(context.Context, []byte) error
}

// SessionUserReader loads the account that owns a session.
type SessionUserReader interface {
	FindByID(context.Context, int64) (domain.User, error)
}

// Sessions issues, resolves, and revokes server-side sessions.
type Sessions struct {
	Repository SessionRepository
	Users      SessionUserReader
	Now        func() time.Time
}

// Issue creates a session for userID and returns the opaque id plus a CSRF token.
// The repository receives only the SHA-256 hash of the opaque id.
func (s Sessions) Issue(ctx context.Context, userID int64) (string, domain.Session, string, error) {
	if s.Repository == nil {
		return "", domain.Session{}, "", errors.New("identity: session repository is required")
	}
	if userID <= 0 {
		return "", domain.Session{}, "", errors.New("identity: session user is required")
	}
	raw, err := domain.NewOpaqueToken()
	if err != nil {
		return "", domain.Session{}, "", err
	}
	csrf, err := domain.NewOpaqueToken()
	if err != nil {
		return "", domain.Session{}, "", err
	}
	now := s.now()
	session, err := s.Repository.InsertSession(ctx, userID, domain.HashSessionToken(raw), now.Add(domain.SessionLifetime), now)
	if err != nil {
		return "", domain.Session{}, "", err
	}
	return raw, session, csrf, nil
}

// Authenticate resolves rawToken to a user. A valid session slides its expiry
// forward by SessionLifetime at most once per SessionSlideInterval.
func (s Sessions) Authenticate(ctx context.Context, rawToken string) (Authenticated, error) {
	if s.Repository == nil || s.Users == nil {
		return Authenticated{}, errors.New("identity: session repository is required")
	}
	if rawToken == "" {
		return Authenticated{}, ErrUnauthenticated
	}
	now := s.now()
	hash := domain.HashSessionToken(rawToken)
	session, err := s.Repository.FindSessionByHash(ctx, hash, now)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			return Authenticated{}, ErrUnauthenticated
		}
		return Authenticated{}, err
	}
	if !session.ExpiresAt.After(now) {
		return Authenticated{}, ErrUnauthenticated
	}
	user, err := s.Users.FindByID(ctx, session.UserID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return Authenticated{}, ErrUnauthenticated
		}
		return Authenticated{}, err
	}
	if user.Disabled {
		return Authenticated{}, ErrUnauthenticated
	}
	slid := false
	if session.LastSeenAt.IsZero() || now.Sub(session.LastSeenAt) >= domain.SessionSlideInterval {
		expiresAt := now.Add(domain.SessionLifetime)
		updated, err := s.Repository.TouchSession(ctx, hash, expiresAt, now)
		if err != nil {
			return Authenticated{}, err
		}
		if updated {
			session.ExpiresAt = expiresAt
			session.LastSeenAt = now
			slid = true
		}
	}
	return Authenticated{User: user, Session: session, TokenHash: hash, Slid: slid}, nil
}

// Revoke deletes the session identified by the opaque cookie value.
func (s Sessions) Revoke(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return nil
	}
	if s.Repository == nil {
		return errors.New("identity: session repository is required")
	}
	return s.Repository.DeleteSessionByHash(ctx, domain.HashSessionToken(rawToken))
}

// RevokeOthers deletes every session for userID except the current token hash.
func (s Sessions) RevokeOthers(ctx context.Context, userID int64, tokenHash []byte) error {
	if userID <= 0 || len(tokenHash) == 0 {
		return ErrUnauthenticated
	}
	if s.Repository == nil {
		return errors.New("identity: session repository is required")
	}
	return s.Repository.DeleteOtherSessions(ctx, userID, tokenHash)
}

func (s Sessions) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
