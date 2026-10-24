package app

import (
	"context"
	"errors"

	"github.com/jooservices/xflickr/internal/identity/domain"
)

var ErrInvalidCredentials = errors.New("invalid email or password")
var ErrRateLimited = errors.New("identity: too many login attempts")

type LoginRepository interface {
	FindByEmail(context.Context, string) (domain.User, error)
}
type LoginLimiter interface {
	Allow(context.Context, string, string) (bool, error)
}
type Login struct {
	Repository LoginRepository
	Limiter    LoginLimiter
}

func (usecase Login) Execute(ctx context.Context, email, password, ip string) (domain.User, error) {
	allowed, err := usecase.Limiter.Allow(ctx, email, ip)
	if err != nil {
		return domain.User{}, err
	}
	if !allowed {
		return domain.User{}, ErrRateLimited
	}
	user, err := usecase.Repository.FindByEmail(ctx, email)
	if err != nil || user.Disabled || !domain.VerifyPassword(user.PasswordHash, password) {
		return domain.User{}, ErrInvalidCredentials
	}
	return user, nil
}
