package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/identity/domain"
)

type passwordRepo struct {
	user    domain.User
	err     error
	hash    string
	updated bool
}

type passwordAttemptLimiter struct {
	allowed bool
	err     error
}

func (l passwordAttemptLimiter) Allow(context.Context, string, string) (bool, error) {
	return l.allowed, l.err
}

func (l passwordAttemptLimiter) RetryAfter(context.Context, string, string) (time.Duration, error) {
	return time.Minute, nil
}

func (r *passwordRepo) FindByID(context.Context, int64) (domain.User, error) {
	return r.user, r.err
}

func (r *passwordRepo) UpdatePassword(_ context.Context, _ int64, hash string) error {
	r.hash, r.updated = hash, true
	return r.err
}

func TestChangePasswordVerifiesUpdatesAndRevokesOtherSessions(t *testing.T) {
	params := domain.Argon2Params{Memory: 8, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16}
	oldHash, err := domain.HashPassword("current-pass", params)
	if err != nil {
		t.Fatal(err)
	}
	repo := &passwordRepo{user: domain.User{ID: 12, PasswordHash: oldHash}}
	sessions := &sessionMemory{}
	currentHash := []byte{1, 2, 3}
	usecase := ChangePassword{Repository: repo, Sessions: sessions, Params: params}
	if err := usecase.Execute(context.Background(), 12, "current-pass", "replacement-pass", currentHash); err != nil {
		t.Fatal(err)
	}
	if !repo.updated || !domain.VerifyPassword(repo.hash, "replacement-pass") {
		t.Fatal("password was not replaced with a valid hash")
	}
	if sessions.othersUser != 12 || string(sessions.othersHash) != string(currentHash) {
		t.Fatalf("other sessions revoked for user=%d hash=%v", sessions.othersUser, sessions.othersHash)
	}
}

func TestChangePasswordValidationAndDependencyErrors(t *testing.T) {
	ctx := context.Background()
	params := domain.Argon2Params{Memory: 8, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16}
	hash, err := domain.HashPassword("current-pass", params)
	if err != nil {
		t.Fatal(err)
	}
	repo := &passwordRepo{user: domain.User{ID: 3, PasswordHash: hash}}
	usecase := ChangePassword{Repository: repo, Sessions: &sessionMemory{}, Params: params}
	if err := usecase.Execute(ctx, 0, "current-pass", "new-password", []byte{1}); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("invalid user error=%v", err)
	}
	if err := usecase.Execute(ctx, 3, "current-pass", "short", []byte{1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("short password error=%v", err)
	}
	if err := usecase.Execute(ctx, 3, "wrong-password", "new-password", []byte{1}); !errors.Is(err, ErrInvalidCurrentPassword) {
		t.Fatalf("current password error=%v", err)
	}
	if err := (ChangePassword{}).Execute(ctx, 3, "current-pass", "new-password", []byte{1}); err == nil {
		t.Fatal("missing repository was accepted")
	}
	if err := (ChangePassword{Repository: repo}).Execute(ctx, 3, "current-pass", "new-password", []byte{1}); err == nil {
		t.Fatal("missing session repository was accepted")
	}
	repo.err = ErrUserNotFound
	if err := usecase.Execute(ctx, 3, "current-pass", "new-password", []byte{1}); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("lookup error=%v", err)
	}
}

func TestChangePasswordRateLimit(t *testing.T) {
	ctx := context.Background()
	params := domain.Argon2Params{Memory: 8, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16}
	hash, err := domain.HashPassword("current-pass", params)
	if err != nil {
		t.Fatal(err)
	}
	repo := &passwordRepo{user: domain.User{ID: 3, PasswordHash: hash}}
	usecase := ChangePassword{Repository: repo, Sessions: &sessionMemory{}, Limiter: passwordAttemptLimiter{allowed: false}, Params: params}
	if err := usecase.Execute(ctx, 3, "current-pass", "new-password", []byte{1}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("rate limit error=%v", err)
	}
	if repo.updated {
		t.Fatal("password updated despite rate limit")
	}
}
