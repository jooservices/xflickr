// Package app contains the application service for effective XFlickr settings.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jooservices/xflickr/internal/settings/domain"
)

const MaxCacheAge = 30 * time.Second

var (
	ErrRepositoryRequired = errors.New("settings: repository is required")
	ErrContextRequired    = errors.New("settings: context is required")
)

// Repository provides the atomic settings snapshot and persists changes.
// Write methods publish domain.ChangedTopic in the same transaction as the
// changed row. SetUserOverride also validates the override against the
// application definition inside that transaction.
type Repository interface {
	Load(context.Context, int64) (domain.Snapshot, error)
	SetUserOverride(context.Context, int64, domain.Key, json.RawMessage) error
	SetApplicationSetting(context.Context, domain.Definition) error
}

type cacheEntry struct {
	value     domain.Effective
	expiresAt time.Time
	epoch     uint64
	revision  uint64
}

// Service merges user settings, validates writes, and keeps a short-lived
// per-user cache. The maximum cache age is always capped at 30 seconds.
type Service struct {
	repository Repository
	cacheAge   time.Duration
	now        func() time.Time
	mu         sync.Mutex
	cache      map[int64]cacheEntry
	epoch      uint64
	revision   map[int64]uint64
}

// New creates a settings service with the maximum supported cache age.
func New(repository Repository) *Service {
	return NewWithCacheAge(repository, MaxCacheAge)
}

// NewWithCacheAge creates a settings service with a caller-selected cache age.
// Values above 30 seconds are reduced to the required maximum.
func NewWithCacheAge(repository Repository, age time.Duration) *Service {
	if age <= 0 || age > MaxCacheAge {
		age = MaxCacheAge
	}
	return &Service{
		repository: repository,
		cacheAge:   age,
		now:        time.Now,
		cache:      make(map[int64]cacheEntry),
		revision:   make(map[int64]uint64),
	}
}

// Effective returns the typed effective settings for a user. A hit is reused
// for at most 30 seconds. Writes from this process drop the affected entries
// immediately, including a load that is already in flight.
func (service *Service) Effective(ctx context.Context, userID int64) (domain.Effective, error) {
	if service == nil || service.repository == nil {
		return domain.Effective{}, ErrRepositoryRequired
	}
	if ctx == nil {
		return domain.Effective{}, ErrContextRequired
	}
	if err := ctx.Err(); err != nil {
		return domain.Effective{}, err
	}

	now := service.now()
	service.mu.Lock()
	epoch := service.epoch
	revision := service.revision[userID]
	if entry, found := service.cache[userID]; found && !now.After(entry.expiresAt) && entry.epoch == epoch && entry.revision == revision {
		service.mu.Unlock()
		return entry.value, nil
	}
	service.mu.Unlock()

	snapshot, err := service.repository.Load(ctx, userID)
	if err != nil {
		return domain.Effective{}, fmt.Errorf("settings: load settings for user %d: %w", userID, err)
	}
	if err := ctx.Err(); err != nil {
		return domain.Effective{}, err
	}
	effective, dropped, err := domain.Merge(snapshot)
	if err != nil {
		return domain.Effective{}, err
	}
	for _, drop := range dropped {
		slog.WarnContext(ctx, "settings: dropped stale user override",
			slog.Int64("user_id", userID),
			slog.String("key", string(drop.Key)),
			slog.String("reason", drop.Reason),
		)
	}

	service.mu.Lock()
	if service.epoch == epoch && service.revision[userID] == revision {
		service.cache[userID] = cacheEntry{
			value:     effective,
			expiresAt: service.now().Add(service.cacheAge),
			epoch:     epoch,
			revision:  revision,
		}
	}
	service.mu.Unlock()
	return effective, nil
}

// RegistrationOpen reads the application registration policy through the
// settings repository. Identity consumes this narrow application port rather
// than querying the settings tables directly.
func (service *Service) RegistrationOpen(ctx context.Context) (bool, error) {
	if service == nil || service.repository == nil {
		return false, ErrRepositoryRequired
	}
	if ctx == nil {
		return false, ErrContextRequired
	}
	snapshot, err := service.repository.Load(ctx, 0)
	if err != nil {
		return false, fmt.Errorf("settings: load registration policy: %w", err)
	}
	setting, found := snapshot.Settings[domain.RegistrationOpen]
	if !found {
		return false, fmt.Errorf("settings: %s is not configured", domain.RegistrationOpen)
	}
	var open bool
	if err := json.Unmarshal(setting.Value, &open); err != nil {
		return false, fmt.Errorf("settings: decode %s: %w", domain.RegistrationOpen, err)
	}
	return open, nil
}

// SetUser persists one user override. The repository checks the application
// cap and writes the row in one serializable transaction, so a concurrent
// cap change cannot land between the check and the upsert. Successful writes
// evict the local user's cached effective settings immediately.
func (service *Service) SetUser(ctx context.Context, userID int64, key domain.Key, value json.RawMessage) error {
	if service == nil || service.repository == nil {
		return ErrRepositoryRequired
	}
	if ctx == nil {
		return ErrContextRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := service.repository.SetUserOverride(ctx, userID, key, value); err != nil {
		var validation *domain.ValidationError
		if errors.As(err, &validation) {
			return err
		}
		return fmt.Errorf("settings: persist user override %s for user %d: %w", key, userID, err)
	}
	service.evict(userID)
	return nil
}

// SetApplicationSetting validates and persists an application default and its
// caps. This operation is intended for an administrator-facing caller.
// Existing user overrides that fall outside the new policy stay stored and
// are ignored by Effective until they fit again.
func (service *Service) SetApplicationSetting(ctx context.Context, setting domain.Definition) error {
	if service == nil || service.repository == nil {
		return ErrRepositoryRequired
	}
	if ctx == nil {
		return ErrContextRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := domain.ValidateDefinition(setting); err != nil {
		return err
	}
	if err := service.repository.SetApplicationSetting(ctx, setting); err != nil {
		return fmt.Errorf("settings: persist application setting %s: %w", setting.Key, err)
	}
	service.mu.Lock()
	service.epoch++
	clear(service.cache)
	service.mu.Unlock()
	return nil
}

func (service *Service) evict(userID int64) {
	service.mu.Lock()
	service.revision[userID]++
	delete(service.cache, userID)
	service.mu.Unlock()
}
