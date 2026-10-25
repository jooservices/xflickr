package app

import (
	"context"
	"errors"

	"github.com/jooservices/xflickr/internal/sharing/domain"
)

var ErrInvalidUser = errors.New("sharing: user id is required")

type Repository interface {
	Set(context.Context, domain.Setting) (domain.Setting, error)
}

type Change struct{ Repository Repository }

func (c Change) Execute(ctx context.Context, userID int64, enabled bool) (domain.Setting, error) {
	setting := domain.Setting{UserID: userID, Enabled: enabled}
	if setting.Validate() != nil {
		return domain.Setting{}, ErrInvalidUser
	}
	if c.Repository == nil {
		return domain.Setting{}, errors.New("sharing: repository is required")
	}
	return c.Repository.Set(ctx, setting)
}
