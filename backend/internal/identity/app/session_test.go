package app

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/identity/domain"
)

type sessionMemory struct {
	user        domain.User
	userErr     error
	insertErr   error
	findErr     error
	touchOK     bool
	touchErr    error
	touchCalls  int
	session     domain.Session
	gotHash     []byte
	gotUser     int64
	gotExpires  time.Time
	gotSeen     time.Time
	deleted     []byte
	deleteErr   error
	othersUser  int64
	othersHash  []byte
	othersErr   error
	listed      []domain.Session
	listErr     error
	revokedID   int64
	revokedFor  int64
	revokeIDOK  bool
	revokeIDErr error
}

func (m *sessionMemory) InsertSession(_ context.Context, userID int64, tokenHash []byte, expiresAt, seenAt time.Time, userAgent, ipAddress string) (domain.Session, error) {
	m.gotUser = userID
	m.gotHash = append([]byte(nil), tokenHash...)
	m.gotExpires = expiresAt
	m.gotSeen = seenAt
	if m.insertErr != nil {
		return domain.Session{}, m.insertErr
	}
	return domain.Session{ID: 11, UserID: userID, ExpiresAt: expiresAt, LastSeenAt: seenAt, CreatedAt: seenAt, UserAgent: userAgent, IPAddress: ipAddress}, nil
}

func (m *sessionMemory) FindSessionByHash(context.Context, []byte, time.Time) (domain.Session, error) {
	if m.findErr != nil {
		return domain.Session{}, m.findErr
	}
	return m.session, nil
}

func (m *sessionMemory) TouchSession(context.Context, []byte, time.Time, time.Time) (bool, error) {
	m.touchCalls++
	if m.touchErr != nil {
		return false, m.touchErr
	}
	return m.touchOK, nil
}

func (m *sessionMemory) DeleteOtherSessions(_ context.Context, userID int64, tokenHash []byte) error {
	m.othersUser = userID
	m.othersHash = append([]byte(nil), tokenHash...)
	return m.othersErr
}

func (m *sessionMemory) DeleteSessionByHash(_ context.Context, tokenHash []byte) error {
	m.deleted = append([]byte(nil), tokenHash...)
	return m.deleteErr
}

func (m *sessionMemory) ListSessions(context.Context, int64) ([]domain.Session, error) {
	return m.listed, m.listErr
}

func (m *sessionMemory) DeleteSessionByID(_ context.Context, userID, sessionID int64) (bool, error) {
	m.revokedFor, m.revokedID = userID, sessionID
	return m.revokeIDOK, m.revokeIDErr
}

func (m *sessionMemory) FindByID(context.Context, int64) (domain.User, error) {
	if m.userErr != nil {
		return domain.User{}, m.userErr
	}
	return m.user, nil
}

func TestIssueStoresOnlyTheSessionHash(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	memory := &sessionMemory{}
	service := Sessions{Repository: memory, Now: func() time.Time { return now }}
	raw, session, csrf, err := service.Issue(context.Background(), 7, "Firefox", "192.0.2.8")
	if err != nil {
		t.Fatal(err)
	}
	if !domain.ValidOpaqueToken(raw) || !domain.ValidOpaqueToken(csrf) || raw == csrf {
		t.Fatalf("tokens raw=%q csrf=%q", raw, csrf)
	}
	if memory.gotUser != 7 || !bytes.Equal(memory.gotHash, domain.HashSessionToken(raw)) {
		t.Fatalf("stored user=%d hash=%x", memory.gotUser, memory.gotHash)
	}
	if bytes.Contains(memory.gotHash, []byte(raw)) {
		t.Fatal("raw session id was stored")
	}
	if !memory.gotExpires.Equal(now.Add(domain.SessionLifetime)) || !memory.gotSeen.Equal(now) {
		t.Fatalf("expiry=%s seen=%s", memory.gotExpires, memory.gotSeen)
	}
	if session.UserID != 7 || !session.ExpiresAt.Equal(memory.gotExpires) || session.UserAgent != "Firefox" || session.IPAddress != "192.0.2.8" {
		t.Fatalf("session = %+v", session)
	}
}

func TestIssueRejectsMissingDependencies(t *testing.T) {
	service := Sessions{}
	if _, _, _, err := service.Issue(context.Background(), 1, "", ""); err == nil {
		t.Fatal("expected missing repository error")
	}
	service.Repository = &sessionMemory{}
	if _, _, _, err := service.Issue(context.Background(), 0, "", ""); err == nil {
		t.Fatal("expected missing user error")
	}
	service.Repository = &sessionMemory{insertErr: errors.New("database unavailable")}
	if _, _, _, err := service.Issue(context.Background(), 1, "", ""); err == nil {
		t.Fatal("expected insert error")
	}
}

func TestListAndRevokeSessions(t *testing.T) {
	items := []domain.Session{{ID: 2}, {ID: 1}}
	memory := &sessionMemory{listed: items, revokeIDOK: true}
	service := Sessions{Repository: memory}
	got, err := service.List(context.Background(), 7)
	if err != nil || len(got) != 2 || got[0].ID != 2 {
		t.Fatalf("List() = %v, %v", got, err)
	}
	deleted, err := service.RevokeByID(context.Background(), 7, 2)
	if err != nil || !deleted || memory.revokedFor != 7 || memory.revokedID != 2 {
		t.Fatalf("RevokeByID() = %v, %v; user=%d id=%d", deleted, err, memory.revokedFor, memory.revokedID)
	}
	if _, err := service.List(context.Background(), 0); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("List() without user error=%v", err)
	}
	if _, err := service.List(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if _, err := (Sessions{}).List(context.Background(), 7); err == nil {
		t.Fatal("List() without repository succeeded")
	}
	if deleted, err := service.RevokeByID(context.Background(), 0, 2); !errors.Is(err, ErrUnauthenticated) || deleted {
		t.Fatalf("RevokeByID() without user = %v, %v", deleted, err)
	}
	if deleted, err := service.RevokeByID(context.Background(), 7, 0); err != nil || deleted {
		t.Fatalf("RevokeByID() with invalid id = %v, %v", deleted, err)
	}
	if _, err := (Sessions{}).RevokeByID(context.Background(), 7, 2); err == nil {
		t.Fatal("RevokeByID() without repository succeeded")
	}
	memory.listErr = errors.New("database unavailable")
	if _, err := service.List(context.Background(), 7); err == nil {
		t.Fatal("List() ignored a repository failure")
	}
	memory.revokeIDErr = errors.New("database unavailable")
	if _, err := service.RevokeByID(context.Background(), 7, 2); err == nil {
		t.Fatal("RevokeByID() ignored a repository failure")
	}
}

func TestAuthenticateMatrix(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	active := domain.Session{ID: 4, UserID: 8, ExpiresAt: now.Add(time.Hour), LastSeenAt: now.Add(-time.Minute), CreatedAt: now.Add(-time.Hour)}
	recent := active
	recent.LastSeenAt = now.Add(-time.Second)
	for _, tc := range []struct {
		name     string
		memory   *sessionMemory
		token    string
		wantErr  error
		wantSlid bool
		wantCall bool
	}{
		{name: "empty", memory: &sessionMemory{}, wantErr: ErrUnauthenticated},
		{name: "missing", memory: &sessionMemory{findErr: ErrUnauthenticated}, token: "raw", wantErr: ErrUnauthenticated},
		{name: "expired", memory: &sessionMemory{session: domain.Session{ExpiresAt: now}}, token: "raw", wantErr: ErrUnauthenticated},
		{name: "lookup", memory: &sessionMemory{findErr: errors.New("database unavailable")}, token: "raw"},
		{name: "user missing", memory: &sessionMemory{session: active, userErr: ErrUserNotFound}, token: "raw", wantErr: ErrUnauthenticated},
		{name: "user lookup", memory: &sessionMemory{session: active, userErr: errors.New("database unavailable")}, token: "raw"},
		{name: "disabled", memory: &sessionMemory{session: active, user: domain.User{ID: 8, Disabled: true}}, token: "raw", wantErr: ErrUnauthenticated},
		{name: "slide", memory: &sessionMemory{session: active, user: domain.User{ID: 8, Role: domain.RoleUser}, touchOK: true}, token: "raw", wantSlid: true, wantCall: true},
		{name: "within minute", memory: &sessionMemory{session: recent, user: domain.User{ID: 8, Role: domain.RoleAdmin}}, token: "raw"},
		{name: "touch error", memory: &sessionMemory{session: active, user: domain.User{ID: 8}, touchErr: errors.New("database unavailable")}, token: "raw", wantCall: true},
		{name: "touch skipped", memory: &sessionMemory{session: active, user: domain.User{ID: 8}, touchOK: false}, token: "raw", wantCall: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := Sessions{Repository: tc.memory, Users: tc.memory, Now: func() time.Time { return now }}
			got, err := service.Authenticate(context.Background(), tc.token)
			if tc.name == "lookup" || tc.name == "user lookup" || tc.name == "touch error" {
				if err == nil || errors.Is(err, ErrUnauthenticated) {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Slid != tc.wantSlid || got.User.ID != 8 || len(got.TokenHash) != 32 {
				t.Fatalf("authenticated = %+v", got)
			}
			if tc.wantCall && tc.memory.touchCalls != 1 {
				t.Fatalf("touch calls = %d", tc.memory.touchCalls)
			}
			if !tc.wantCall && tc.memory.touchCalls != 0 {
				t.Fatalf("touch calls = %d", tc.memory.touchCalls)
			}
			if tc.wantSlid && !got.Session.ExpiresAt.Equal(now.Add(domain.SessionLifetime)) {
				t.Fatalf("slid expiry = %s", got.Session.ExpiresAt)
			}
		})
	}
}

func TestAuthenticateRequiresDependencies(t *testing.T) {
	if _, err := (Sessions{}).Authenticate(context.Background(), "raw"); err == nil {
		t.Fatal("expected missing repository error")
	}
	if _, err := (Sessions{Repository: &sessionMemory{}}).Authenticate(context.Background(), "raw"); err == nil {
		t.Fatal("expected missing user reader error")
	}
}

func TestRevokeSessionAndOthers(t *testing.T) {
	memory := &sessionMemory{}
	service := Sessions{Repository: memory}
	if err := service.Revoke(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if memory.deleted != nil {
		t.Fatal("empty token was revoked")
	}
	if err := service.Revoke(context.Background(), "raw-token"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(memory.deleted, domain.HashSessionToken("raw-token")) {
		t.Fatalf("deleted = %x", memory.deleted)
	}
	hash := domain.HashSessionToken("current")
	if err := service.RevokeOthers(context.Background(), 4, hash); err != nil {
		t.Fatal(err)
	}
	if memory.othersUser != 4 || !bytes.Equal(memory.othersHash, hash) {
		t.Fatalf("others user=%d hash=%x", memory.othersUser, memory.othersHash)
	}
	if err := service.RevokeOthers(context.Background(), 0, hash); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("error = %v", err)
	}
	if err := (Sessions{}).Revoke(context.Background(), "raw"); err == nil {
		t.Fatal("expected missing repository error")
	}
	if err := (Sessions{}).RevokeOthers(context.Background(), 1, hash); err == nil {
		t.Fatal("expected missing repository error")
	}
	memory.deleteErr = errors.New("database unavailable")
	if err := service.Revoke(context.Background(), "raw-token"); err == nil {
		t.Fatal("expected revoke error")
	}
	memory.othersErr = errors.New("database unavailable")
	if err := service.RevokeOthers(context.Background(), 4, hash); err == nil {
		t.Fatal("expected revoke others error")
	}
}

func TestSessionsUseTheCurrentTimeByDefault(t *testing.T) {
	memory := &sessionMemory{}
	before := time.Now().UTC().Add(-time.Second)
	if _, _, _, err := (Sessions{Repository: memory}).Issue(context.Background(), 1, "", ""); err != nil {
		t.Fatal(err)
	}
	after := time.Now().UTC().Add(time.Second)
	if memory.gotSeen.Before(before) || memory.gotSeen.After(after) {
		t.Fatalf("seen = %s", memory.gotSeen)
	}
}
