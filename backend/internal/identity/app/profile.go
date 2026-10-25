package app

import (
	"context"
	"errors"

	"github.com/jooservices/xflickr/internal/identity/domain"
)

var ErrInvalidLocale = errors.New("identity: locale must be en or vi")

type ProfileRepository interface {
	UpdateLocale(context.Context, int64, string) (domain.User, error)
}

type UpdateProfile struct{ Repository ProfileRepository }

func (u UpdateProfile) UpdateLocale(ctx context.Context, userID int64, locale string) (domain.User, error) {
	if userID < 1 || (locale != "en" && locale != "vi") {
		return domain.User{}, ErrInvalidLocale
	}
	if u.Repository == nil {
		return domain.User{}, errors.New("identity: profile repository is required")
	}
	return u.Repository.UpdateLocale(ctx, userID, locale)
}
