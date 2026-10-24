// Package settings reads administrator key caps from the settings application service.
package settings

import (
	"context"
	"errors"
	"fmt"

	"github.com/jooservices/xflickr/internal/accounts/domain"
	settingsdomain "github.com/jooservices/xflickr/internal/settings/domain"
)

// EffectiveSettings is the settings application API used to read administrator caps.
type EffectiveSettings interface {
	Effective(context.Context, int64) (settingsdomain.Effective, error)
}

// Reader maps application settings onto API key limits. User overrides are
// not consulted: the caps belong to the administrator.
type Reader struct {
	Settings EffectiveSettings
}

// Policy returns the key limit policy. userID is accepted so callers can pass
// the signed-in user; the caps are the application settings, not that user's overrides.
func (r Reader) Policy(ctx context.Context, _ int64) (domain.Policy, error) {
	if r.Settings == nil {
		return domain.Policy{}, errors.New("accounts: settings service is required")
	}
	if ctx == nil {
		return domain.Policy{}, errors.New("accounts: context is required")
	}
	effective, err := r.Settings.Effective(ctx, 0)
	if err != nil {
		return domain.Policy{}, fmt.Errorf("accounts: load administrator caps: %w", err)
	}
	return domain.PolicyFromSettings(effective.DailyCallLimit, effective.PerSecondRate, effective.Burst)
}
