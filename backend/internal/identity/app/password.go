package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jooservices/xflickr/internal/identity/domain"
)

var ErrInvalidCurrentPassword = errors.New("identity: current password is incorrect")

type PasswordRepository interface {
	FindByID(context.Context, int64) (domain.User, error)
	UpdatePassword(context.Context, int64, string) error
}

type ChangePassword struct {
	Repository PasswordRepository
	Sessions   SessionRepository
	Limiter    LoginLimiter
	Params     domain.Argon2Params
}

func (u ChangePassword) RetryAfter(ctx context.Context, userID int64) (time.Duration, error) {
	if u.Limiter == nil {
		return 0, nil
	}
	return u.Limiter.RetryAfter(ctx, fmt.Sprintf("password:%d", userID), "")
}

func (u ChangePassword) Execute(ctx context.Context, userID int64, currentPassword, newPassword string, currentTokenHash []byte) error {
	if userID <= 0 {
		return ErrUnauthenticated
	}
	if len(newPassword) < MinPasswordBytes || len(newPassword) > MaxPasswordBytes {
		return ErrInvalidInput
	}
	if u.Repository == nil {
		return errors.New("identity: password repository is required")
	}
	if u.Sessions == nil {
		return errors.New("identity: session repository is required")
	}
	if u.Limiter != nil {
		allowed, err := u.Limiter.Allow(ctx, fmt.Sprintf("password:%d", userID), "")
		if err != nil {
			return err
		}
		if !allowed {
			return ErrRateLimited
		}
	}
	user, err := u.Repository.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if !domain.VerifyPassword(user.PasswordHash, currentPassword) {
		return ErrInvalidCurrentPassword
	}
	hash, err := domain.HashPassword(newPassword, u.Params)
	if err != nil {
		return err
	}
	if err := u.Repository.UpdatePassword(ctx, userID, hash); err != nil {
		return err
	}
	return u.Sessions.DeleteOtherSessions(ctx, userID, currentTokenHash)
}
