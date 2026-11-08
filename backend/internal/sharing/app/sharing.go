package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/jooservices/xflickr/internal/sharing/domain"
)

var ErrInvalidUser = errors.New("sharing: user id is required")

type Repository interface {
	Set(context.Context, domain.Setting) (domain.Setting, error)
}

// Reader loads the current caller's persisted sharing choice.
type Reader interface {
	Get(context.Context, int64) (domain.Setting, error)
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

// Read returns the caller's sharing choice, including the off-by-default state.
type Read struct{ Repository Reader }

// Execute loads the caller's persisted sharing choice.
func (r Read) Execute(ctx context.Context, userID int64) (domain.Setting, error) {
	if userID <= 0 {
		return domain.Setting{}, ErrInvalidUser
	}
	if r.Repository == nil {
		return domain.Setting{}, errors.New("sharing: reader is required")
	}
	setting, err := r.Repository.Get(ctx, userID)
	if err != nil {
		return domain.Setting{}, fmt.Errorf("sharing: read setting: %w", err)
	}
	return setting, nil
}
