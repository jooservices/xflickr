package app

import (
	"context"
	"errors"
)

var (
	ErrDepthStoreRequired = errors.New("crawl: depth store is required")
	ErrDepthInvalid       = errors.New("crawl: depth must be at least 1")
	ErrDepthAboveCap      = errors.New("crawl: depth is above the administrator limit")
)

// DepthPolicy is a user's effective crawl depth with the default and the cap.
type DepthPolicy struct {
	Depth   int
	Default int
	Max     int
}

// DepthStore reads and writes the per-user crawl depth setting.
type DepthStore interface {
	Policy(context.Context, int64) (DepthPolicy, error)
	// SetDepth returns ErrDepthAboveCap when the store rejects the value as too deep.
	SetDepth(context.Context, int64, int) error
}

// Depth is the crawl depth use case: a user reads and changes how deep their
// own discovery goes, within the administrator limit.
type Depth struct {
	Store DepthStore
}

func (service Depth) Get(ctx context.Context, userID int64) (DepthPolicy, error) {
	if service.Store == nil {
		return DepthPolicy{}, ErrDepthStoreRequired
	}
	return service.Store.Policy(ctx, userID)
}

// Set stores the depth and returns the policy after the change.
func (service Depth) Set(ctx context.Context, userID int64, depth int) (DepthPolicy, error) {
	policy, err := service.Get(ctx, userID)
	if err != nil {
		return DepthPolicy{}, err
	}
	if depth < 1 {
		return policy, ErrDepthInvalid
	}
	if policy.Max > 0 && depth > policy.Max {
		return policy, ErrDepthAboveCap
	}
	if err := service.Store.SetDepth(ctx, userID, depth); err != nil {
		return policy, err
	}
	return service.Get(ctx, userID)
}
