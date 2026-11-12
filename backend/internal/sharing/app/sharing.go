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

// Reader loads the current caller's persisted sharing choice and the number of
// records the caller shares.
type Reader interface {
	Get(context.Context, int64) (domain.Setting, error)
	CountShared(context.Context, int64) (int64, error)
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

// Read returns the caller's sharing choice, including the off-by-default state,
// with the count of records the caller shares.
type Read struct{ Repository Reader }

// Execute loads the caller's persisted sharing choice and shared record count.
func (r Read) Execute(ctx context.Context, userID int64) (domain.Summary, error) {
	if userID <= 0 {
		return domain.Summary{}, ErrInvalidUser
	}
	if r.Repository == nil {
		return domain.Summary{}, errors.New("sharing: reader is required")
	}
	setting, err := r.Repository.Get(ctx, userID)
	if err != nil {
		return domain.Summary{}, fmt.Errorf("sharing: read setting: %w", err)
	}
	shared, err := r.Repository.CountShared(ctx, userID)
	if err != nil {
		return domain.Summary{}, fmt.Errorf("sharing: count shared records: %w", err)
	}
	return domain.Summary{Setting: setting, SharedRecordCount: shared}, nil
}
