package domain

import (
	"context"
	"errors"
	"time"
)

// Budget contains one key's configured hourly and per-second limits, plus the
// shared policy that protects interactive calls and the person lane.
type Budget struct {
	HourlyLimit     int64
	PerSecondRate   int64
	Burst           int64
	Reserve         int64
	PersonLaneShare float64
}

// Validate rejects values that cannot form a meaningful Redis budget.
func (b Budget) Validate() error {
	if b.HourlyLimit <= 0 || b.PerSecondRate <= 0 || b.Burst <= 0 || b.Reserve < 0 || b.PersonLaneShare < 0 || b.PersonLaneShare > 1 {
		return ErrInvalidBudget
	}
	return nil
}

// Meta is the outcome reported when a caller finishes a permit. Err is the
// classified Flickr call error, when the call failed.
type Meta struct {
	Err error
}

// Permit records a completed call. It must be safe to call once; repeated
// calls are ignored by implementations.
type Permit interface {
	Done(Meta) error
}

// Governor atomically acquires a permit for the key and class. Wait is the
// duration until the next possible attempt when no permit is available.
type Governor interface {
	Acquire(context.Context, int64, Class) (Permit, time.Duration, error)
}

// QuotaError reports whether a result is Flickr's quota exhaustion error.
func QuotaError(meta Meta, quotaExceeded error) bool {
	return meta.Err != nil && errors.Is(meta.Err, quotaExceeded)
}
