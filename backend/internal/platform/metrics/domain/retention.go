package domain

import "fmt"

const (
	// DefaultRetentionDays is the seeded admin default.
	DefaultRetentionDays = 30
	// MinRetentionDays is the lowest allowed retention window.
	MinRetentionDays = 1
	// MaxRetentionDays is the highest allowed retention window.
	MaxRetentionDays = 365
)

// ValidateRetentionDays checks the admin retention setting bounds.
func ValidateRetentionDays(days int) error {
	if days < MinRetentionDays || days > MaxRetentionDays {
		return fmt.Errorf("metrics: retention_days %d outside [%d, %d]", days, MinRetentionDays, MaxRetentionDays)
	}
	return nil
}

// ClampRetentionDays returns days when valid, otherwise the default.
func ClampRetentionDays(days int) int {
	if err := ValidateRetentionDays(days); err != nil {
		return DefaultRetentionDays
	}
	return days
}
