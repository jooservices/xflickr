// Package app coordinates crawl interest rules with settings and persistence.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	crawldomain "github.com/jooservices/xflickr/internal/crawl/domain"
	settingsdomain "github.com/jooservices/xflickr/internal/settings/domain"
)

var (
	ErrRepositoryRequired = errors.New("crawl: interest repository is required")
	ErrSettingsRequired   = errors.New("crawl: effective settings reader is required")
	ErrContextRequired    = errors.New("crawl: context is required")
)

// InterestRepository stores per-user person interests.
type InterestRepository interface {
	Add(context.Context, crawldomain.Interest) error
	ForPerson(context.Context, string) ([]crawldomain.InterestDepth, error)
}

// EffectiveSettings supplies the user's merged settings.
type EffectiveSettings interface {
	Effective(context.Context, int64) (settingsdomain.Effective, error)
}

// Interests applies per-user depth limits before persisting interests.
type Interests struct {
	Repository InterestRepository
	Settings   EffectiveSettings
}

// Add records an interest unless its depth exceeds this user's effective max.
// The repository keeps the lowest depth for each user-person pair.
func (service Interests) Add(ctx context.Context, userID int64, person string, depth int, via string) error {
	if err := service.ready(ctx); err != nil {
		return err
	}
	interest := crawldomain.Interest{UserID: userID, PersonNSID: person, Depth: depth, Via: via}
	if err := interest.Validate(); err != nil {
		return err
	}
	effective, err := service.Settings.Effective(ctx, userID)
	if err != nil {
		return fmt.Errorf("crawl: load max depth for user %d: %w", userID, err)
	}
	if depth > effective.MaxDepth {
		return nil
	}
	return service.Repository.Add(ctx, interest)
}

// Expandable reports whether any interested user can continue discovery.
func (service Interests) Expandable(ctx context.Context, person string) (bool, error) {
	if err := service.ready(ctx); err != nil {
		return false, err
	}
	interests, err := service.Repository.ForPerson(ctx, person)
	if err != nil {
		return false, err
	}
	maxDepthByUser := make(map[int64]int, len(interests))
	for _, interest := range interests {
		if _, loaded := maxDepthByUser[interest.UserID]; loaded {
			continue
		}
		effective, err := service.Settings.Effective(ctx, interest.UserID)
		if err != nil {
			return false, fmt.Errorf("crawl: load max depth for user %d: %w", interest.UserID, err)
		}
		maxDepthByUser[interest.UserID] = effective.MaxDepth
	}
	return crawldomain.IsExpandable(interests, maxDepthByUser), nil
}

// HandleConnected decodes flickr_account.connected and adds its depth zero
// owner interest. Repeated delivery is safe because Add is idempotent.
func (service Interests) HandleConnected(ctx context.Context, payload []byte) error {
	var event struct {
		UserID int64  `json:"user_id"`
		NSID   string `json:"nsid"`
		Depth  int    `json:"depth"`
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("crawl: decode connected account event: %w", err)
	}
	return service.Add(ctx, event.UserID, event.NSID, event.Depth, "connected_account")
}

func (service Interests) ready(ctx context.Context) error {
	if service.Repository == nil {
		return ErrRepositoryRequired
	}
	if service.Settings == nil {
		return ErrSettingsRequired
	}
	if ctx == nil {
		return ErrContextRequired
	}
	return ctx.Err()
}
