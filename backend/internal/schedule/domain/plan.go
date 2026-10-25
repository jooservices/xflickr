// Package domain contains schedule entitlements and plan rules.
package domain

import "errors"

var ErrInvalidPlan = errors.New("schedule: invalid plan")

// Plan defines the configurable limits applied to an XFlickr user.
type Plan struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	Weight        int     `json:"weight"`
	MaxDepthCap   int     `json:"max_depth_cap"`
	MaxContacts   int     `json:"max_contacts"`
	AIQuotaPerDay int     `json:"ai_quota_per_day"`
	ZipTTLHours   int     `json:"zip_ttl_hours"`
	ResourceShare float64 `json:"resource_share"`
}

// Validate rejects plans that cannot be applied as user entitlements.
func (p Plan) Validate() error {
	if p.Name == "" || p.Weight <= 0 || p.MaxDepthCap < 0 || p.MaxContacts < 0 || p.AIQuotaPerDay < 0 || p.ZipTTLHours < 0 || p.ResourceShare < 0 || p.ResourceShare > 1 {
		return ErrInvalidPlan
	}
	return nil
}
