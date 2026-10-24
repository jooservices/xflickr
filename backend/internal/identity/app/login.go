package app

import (
	"context"
	"errors"
	"strings"

	"github.com/jooservices/xflickr/internal/identity/domain"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrRateLimited        = errors.New("identity: too many login attempts")
	ErrUserNotFound       = errors.New("identity: user not found")
)

const (
	MinPasswordBytes = 8
	MaxPasswordBytes = 128
	MaxEmailBytes    = 254
)

type LoginRepository interface {
	FindByEmail(context.Context, string) (domain.User, error)
}
type LoginLimiter interface {
	Allow(context.Context, string, string) (bool, error)
}
type Login struct {
	Repository        LoginRepository
	Limiter           LoginLimiter
	DummyPasswordHash string
}

func (usecase Login) Execute(ctx context.Context, email, password, ip string) (domain.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	allowed, err := usecase.Limiter.Allow(ctx, email, ip)
	if err != nil {
		return domain.User{}, err
	}
	if !allowed {
		return domain.User{}, ErrRateLimited
	}
	if len(email) > MaxEmailBytes || len(password) < MinPasswordBytes || len(password) > MaxPasswordBytes {
		domain.VerifyPassword(usecase.DummyPasswordHash, password)
		return domain.User{}, ErrInvalidCredentials
	}
	user, err := usecase.Repository.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			domain.VerifyPassword(usecase.DummyPasswordHash, password)
			return domain.User{}, ErrInvalidCredentials
		}
		return domain.User{}, err
	}
	valid := domain.VerifyPassword(user.PasswordHash, password)
	if user.Disabled || !valid {
		return domain.User{}, ErrInvalidCredentials
	}
	return user, nil
}
