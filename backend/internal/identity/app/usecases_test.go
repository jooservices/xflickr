package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/identity/domain"
)

type registerRepo struct {
	got    domain.User
	result domain.User
	err    error
}

func (r *registerRepo) Register(_ context.Context, u domain.User, open bool) (domain.User, error) {
	r.got = u
	if !open && r.result.Role != domain.RoleAdmin {
		return domain.User{}, ErrRegistrationClosed
	}
	if r.err != nil {
		return domain.User{}, r.err
	}
	if r.result.Role != "" {
		return r.result, nil
	}
	u.ID = 1
	u.Role = domain.RoleAdmin
	return u, nil
}

type registrationPolicy bool

func (p registrationPolicy) RegistrationOpen(context.Context) (bool, error) { return bool(p), nil }

func TestRegisterFirstUser(t *testing.T) {
	repo := &registerRepo{}
	uc := Register{Repository: repo, Policy: registrationPolicy(true), Params: domain.Argon2Params{Memory: 32 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}}
	u, err := uc.Execute(context.Background(), " First@Example.com ", "password", "")
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != domain.RoleAdmin || repo.got.Email != "first@example.com" || repo.got.Locale != "en" || !domain.VerifyPassword(repo.got.PasswordHash, "password") {
		t.Fatalf("unexpected registration user %#v stored %#v", u, repo.got)
	}
}

func TestRegisterPropagatesClosedRegistration(t *testing.T) {
	repo := &registerRepo{err: ErrRegistrationClosed}
	_, err := (Register{Repository: repo, Policy: registrationPolicy(false)}).Execute(context.Background(), "a@example.com", "password", "")
	if !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("error = %v", err)
	}
}

func TestRegisterRejectsMissingFields(t *testing.T) {
	for _, values := range [][2]string{{"", "password12"}, {"a@example.com", ""}, {"a@example.com", "short"}, {"a@example.com", string(make([]byte, MaxPasswordBytes+1))}, {string(make([]byte, MaxEmailBytes+1)), "password12"}} {
		if _, err := (Register{Repository: &registerRepo{}, Policy: registrationPolicy(true)}).Execute(context.Background(), values[0], values[1], ""); err == nil {
			t.Fatalf("Execute(%q, %q) succeeded", values[0], values[1])
		}
	}
}

type loginRepo struct {
	user domain.User
	err  error
}

func (r loginRepo) FindByEmail(context.Context, string) (domain.User, error) { return r.user, r.err }

type limiter struct {
	allowed bool
	calls   int
	email   string
}

func (l *limiter) Allow(_ context.Context, email, _ string) (bool, error) {
	l.calls++
	l.email = email
	return l.allowed, nil
}

func TestLoginRateLimit(t *testing.T) {
	limit := &limiter{}
	_, err := (Login{Repository: loginRepo{}, Limiter: limit}).Execute(context.Background(), "a@example.com", "bad", "127.0.0.1")
	if !errors.Is(err, ErrRateLimited) || limit.calls != 1 {
		t.Fatalf("error/calls = %v/%d", err, limit.calls)
	}
}

func TestLoginSuccess(t *testing.T) {
	params := domain.Argon2Params{Memory: 32 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	hash, _ := domain.HashPassword("password", params)
	user := domain.User{ID: 1, Email: "a@example.com", PasswordHash: hash}
	limit := &limiter{allowed: true}
	dummy, _ := domain.HashPassword("dummy", params)
	got, err := (Login{Repository: loginRepo{user: user}, Limiter: limit, DummyPasswordHash: dummy}).Execute(context.Background(), user.Email, "password", "ip")
	if err != nil || got.ID != user.ID || limit.calls != 1 {
		t.Fatalf("Execute() = %#v, %v; calls=%d", got, err, limit.calls)
	}
}

type failingLimiter struct{}

func (failingLimiter) Allow(context.Context, string, string) (bool, error) {
	return false, errors.New("redis unavailable")
}

func TestLoginReturnsLimiterError(t *testing.T) {
	_, err := (Login{Repository: loginRepo{}, Limiter: failingLimiter{}}).Execute(context.Background(), "a@example.com", "password", "ip")
	if err == nil || err.Error() != "redis unavailable" {
		t.Fatalf("error = %v", err)
	}
}

func TestLoginUsesSameCredentialFailure(t *testing.T) {
	params := domain.Argon2Params{Memory: 32 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	hash, _ := domain.HashPassword("password", params)
	dummy, _ := domain.HashPassword("dummy", params)
	for _, repo := range []loginRepo{{err: ErrUserNotFound}, {user: domain.User{PasswordHash: hash, Disabled: true}}, {user: domain.User{PasswordHash: hash}}} {
		lim := &limiter{allowed: true}
		_, err := (Login{Repository: repo, Limiter: lim, DummyPasswordHash: dummy}).Execute(context.Background(), "a@example.com", "wrong", "ip")
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("error = %v", err)
		}
	}
}

func TestLoginPerformsArgonVerificationForMissingEmail(t *testing.T) {
	params := domain.Argon2Params{Memory: 32 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	dummy, err := domain.HashPassword("dummy", params)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = (Login{Repository: loginRepo{err: ErrUserNotFound}, Limiter: &limiter{allowed: true}, DummyPasswordHash: dummy}).Execute(context.Background(), "missing@example.com", "wrongpass", "ip")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("error = %v", err)
	}
	if elapsed := time.Since(started); elapsed < time.Millisecond {
		t.Fatalf("missing-user verification took %s; expected Argon2 work", elapsed)
	}
}

func TestLoginRejectsOutOfRangeLengths(t *testing.T) {
	params := domain.Argon2Params{Memory: 32 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	dummy, _ := domain.HashPassword("dummy", params)
	for _, password := range []string{"short", string(make([]byte, MaxPasswordBytes+1))} {
		lim := &limiter{allowed: true}
		_, err := (Login{Repository: loginRepo{}, Limiter: lim, DummyPasswordHash: dummy}).Execute(context.Background(), "a@example.com", password, "ip")
		if !errors.Is(err, ErrInvalidCredentials) || lim.calls != 1 {
			t.Fatalf("password length %d: error=%v limiter calls=%d", len(password), err, lim.calls)
		}
	}
}

func TestLoginNormalizesEmailBeforeLimitAndLookup(t *testing.T) {
	params := domain.Argon2Params{Memory: 32 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	hash, _ := domain.HashPassword("password", params)
	dummy, _ := domain.HashPassword("dummy", params)
	lim := &limiter{allowed: true}
	repo := &recordingLoginRepo{user: domain.User{Email: "a@example.com", PasswordHash: hash}}
	_, err := (Login{Repository: repo, Limiter: lim, DummyPasswordHash: dummy}).Execute(context.Background(), " A@Example.com ", "password", "ip")
	if err != nil || lim.email != "a@example.com" || repo.email != "a@example.com" {
		t.Fatalf("email normalization: err=%v limiter=%q repository=%q", err, lim.email, repo.email)
	}
}

type recordingLoginRepo struct {
	user  domain.User
	email string
}

func (r *recordingLoginRepo) FindByEmail(_ context.Context, email string) (domain.User, error) {
	r.email = email
	return r.user, nil
}

func TestLoginPropagatesRepositoryInfrastructureErrors(t *testing.T) {
	dbErr := errors.New("database unavailable")
	_, err := (Login{Repository: loginRepo{err: dbErr}, Limiter: &limiter{allowed: true}}).Execute(context.Background(), "a@example.com", "password", "ip")
	if !errors.Is(err, dbErr) || errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("error = %v", err)
	}
}
