// Package domain defines database-backed feature flags.
package domain

import (
	"errors"
	"fmt"
	"regexp"
)

type FlagAudience string

const (
	AudienceAll          FlagAudience = "all"
	AudienceAdmins       FlagAudience = "admins"
	AudienceUserIDs      FlagAudience = "user_ids"
	MaxFlagNameLength                 = 128
	MaxFlagAudienceUsers              = 10000
)

var flagNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,127}$`)

var ErrInvalidFeatureFlag = errors.New("settings: invalid feature flag")

// FeatureFlag is one server-side rollout switch. A disabled or unknown flag is
// always off; the audience is evaluated only when Enabled is true.
type FeatureFlag struct {
	Name     string       `json:"name"`
	Enabled  bool         `json:"enabled"`
	Audience FlagAudience `json:"audience"`
	UserIDs  []int64      `json:"user_ids"`
}

// ValidateFeatureFlag checks the persisted and API-level flag invariants.
func ValidateFeatureFlag(flag FeatureFlag) error {
	if len(flag.Name) == 0 || len(flag.Name) > MaxFlagNameLength || !flagNamePattern.MatchString(flag.Name) {
		return fmt.Errorf("%w: name must be 1-128 lowercase letters, digits, dots, dashes or underscores and start with a letter", ErrInvalidFeatureFlag)
	}
	switch flag.Audience {
	case AudienceAll, AudienceAdmins:
		if len(flag.UserIDs) != 0 {
			return fmt.Errorf("%w: audience %q cannot include user_ids", ErrInvalidFeatureFlag, flag.Audience)
		}
	case AudienceUserIDs:
		if len(flag.UserIDs) == 0 || len(flag.UserIDs) > MaxFlagAudienceUsers {
			return fmt.Errorf("%w: user_ids audience must include 1-%d users", ErrInvalidFeatureFlag, MaxFlagAudienceUsers)
		}
		seen := make(map[int64]struct{}, len(flag.UserIDs))
		for _, userID := range flag.UserIDs {
			if userID <= 0 {
				return fmt.Errorf("%w: user_ids must be positive", ErrInvalidFeatureFlag)
			}
			if _, duplicate := seen[userID]; duplicate {
				return fmt.Errorf("%w: user_ids must be unique", ErrInvalidFeatureFlag)
			}
			seen[userID] = struct{}{}
		}
	default:
		return fmt.Errorf("%w: audience must be %q, %q or %q", ErrInvalidFeatureFlag, AudienceAll, AudienceAdmins, AudienceUserIDs)
	}
	return nil
}

// EnabledFor applies a flag's audience after the enabled bit is checked.
func (flag FeatureFlag) EnabledFor(userID int64, isAdmin bool) bool {
	if !flag.Enabled {
		return false
	}
	switch flag.Audience {
	case AudienceAll:
		return true
	case AudienceAdmins:
		return userID > 0 && isAdmin
	case AudienceUserIDs:
		if userID <= 0 {
			return false
		}
		for _, candidate := range flag.UserIDs {
			if candidate == userID {
				return true
			}
		}
	}
	return false
}
