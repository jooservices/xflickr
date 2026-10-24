package app

import (
	"context"
	"errors"
	"testing"

	"github.com/jooservices/xflickr/internal/identity/domain"
)

type registerRepo struct {
	got    domain.User
	result domain.User
	err    error
}

func (r *registerRepo) Register(_ context.Context, u domain.User) (domain.User, error) {
	r.got = u
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

func TestRegisterFirstUser(t *testing.T) {
	repo := &registerRepo{}
	uc := Register{Repository: repo, Params: domain.Argon2Params{Memory: 32 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}}
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
	_, err := (Register{Repository: repo}).Execute(context.Background(), "a@example.com", "password", "")
	if !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("error = %v", err)
	}
}

func TestRegisterRejectsMissingFields(t *testing.T) {
	for _, values := range [][2]string{{"", "password"}, {"a@example.com", ""}} {
		if _, err := (Register{Repository: &registerRepo{}}).Execute(context.Background(), values[0], values[1], ""); err == nil {
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
}

func (l *limiter) Allow(context.Context, string, string) (bool, error) {
	l.calls++
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
	got, err := (Login{Repository: loginRepo{user: user}, Limiter: limit}).Execute(context.Background(), user.Email, "password", "ip")
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
	for _, repo := range []loginRepo{{err: errors.New("not found")}, {user: domain.User{PasswordHash: hash, Disabled: true}}, {user: domain.User{PasswordHash: hash}}} {
		lim := &limiter{allowed: true}
		_, err := (Login{Repository: repo, Limiter: lim}).Execute(context.Background(), "a@example.com", "wrong", "ip")
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("error = %v", err)
		}
	}
}
