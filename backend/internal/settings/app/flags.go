package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/jooservices/xflickr/internal/settings/domain"
)

const MaxFeatureFlagCacheAge = 30 * time.Second

var (
	ErrFlagRepositoryRequired = errors.New("settings: feature flag repository is required")
	ErrNoFeatureFlags         = errors.New("settings: at least one feature flag is required")
	ErrDuplicateFeatureFlag   = errors.New("settings: feature flag names must be unique")
)

// FeatureFlagRepository persists rollout flags and determines whether a user
// currently has the administrator role.
type FeatureFlagRepository interface {
	ListFlags(context.Context) ([]domain.FeatureFlag, error)
	UserIsAdmin(context.Context, int64) (bool, error)
	UpsertFlags(context.Context, []domain.FeatureFlag) error
}

type featureFlagSnapshot struct {
	flags     []domain.FeatureFlag
	expiresAt time.Time
}

// FeatureFlags provides cached lookups and administrator updates. A database
// update invalidates this process immediately; other API processes refresh
// within the 30-second cache window.
type FeatureFlags struct {
	repository FeatureFlagRepository
	cacheAge   time.Duration
	now        func() time.Time
	mu         sync.Mutex
	cache      *featureFlagSnapshot
	revision   uint64
}

func NewFeatureFlags(repository FeatureFlagRepository) *FeatureFlags {
	return NewFeatureFlagsWithCacheAge(repository, MaxFeatureFlagCacheAge)
}

func NewFeatureFlagsWithCacheAge(repository FeatureFlagRepository, age time.Duration) *FeatureFlags {
	if age <= 0 || age > MaxFeatureFlagCacheAge {
		age = MaxFeatureFlagCacheAge
	}
	return &FeatureFlags{repository: repository, cacheAge: age, now: time.Now}
}

// List returns all configured flags, ordered by name. Results are copied so
// callers cannot mutate the cached audience list.
func (service *FeatureFlags) List(ctx context.Context) ([]domain.FeatureFlag, error) {
	flags, err := service.load(ctx)
	if err != nil {
		return nil, err
	}
	return cloneFeatureFlags(flags), nil
}

// Enabled returns whether name is enabled for userID. Unknown flags are off.
func (service *FeatureFlags) Enabled(ctx context.Context, name string, userID int64) (bool, error) {
	flags, err := service.load(ctx)
	if err != nil {
		return false, err
	}
	var flag domain.FeatureFlag
	found := false
	for _, candidate := range flags {
		if candidate.Name == name {
			flag, found = candidate, true
			break
		}
	}
	if !found || !flag.Enabled {
		return false, nil
	}
	if flag.Audience != domain.AudienceAdmins {
		return flag.EnabledFor(userID, false), nil
	}
	if userID <= 0 {
		return false, nil
	}
	isAdmin, err := service.repository.UserIsAdmin(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("settings: check administrator for feature flag %q: %w", name, err)
	}
	return flag.EnabledFor(userID, isAdmin), nil
}

// Update upserts the supplied flags atomically. Omitted flags remain unchanged.
func (service *FeatureFlags) Update(ctx context.Context, flags []domain.FeatureFlag) error {
	if service == nil || service.repository == nil {
		return ErrFlagRepositoryRequired
	}
	if ctx == nil {
		return ErrContextRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(flags) == 0 {
		return ErrNoFeatureFlags
	}
	normalized := cloneFeatureFlags(flags)
	seen := make(map[string]struct{}, len(normalized))
	for i := range normalized {
		if err := domain.ValidateFeatureFlag(normalized[i]); err != nil {
			return err
		}
		if _, duplicate := seen[normalized[i].Name]; duplicate {
			return fmt.Errorf("%w: %q", ErrDuplicateFeatureFlag, normalized[i].Name)
		}
		seen[normalized[i].Name] = struct{}{}
		sort.Slice(normalized[i].UserIDs, func(a, b int) bool { return normalized[i].UserIDs[a] < normalized[i].UserIDs[b] })
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].Name < normalized[j].Name })
	if err := service.repository.UpsertFlags(ctx, normalized); err != nil {
		return fmt.Errorf("settings: save feature flags: %w", err)
	}
	service.mu.Lock()
	service.revision++
	service.cache = nil
	service.mu.Unlock()
	return nil
}

func (service *FeatureFlags) load(ctx context.Context) ([]domain.FeatureFlag, error) {
	if service == nil || service.repository == nil {
		return nil, ErrFlagRepositoryRequired
	}
	if ctx == nil {
		return nil, ErrContextRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := service.now()
	service.mu.Lock()
	if service.cache != nil && now.Before(service.cache.expiresAt) {
		flags := cloneFeatureFlags(service.cache.flags)
		service.mu.Unlock()
		return flags, nil
	}
	revision := service.revision
	service.mu.Unlock()

	flags, err := service.repository.ListFlags(ctx)
	if err != nil {
		return nil, fmt.Errorf("settings: load feature flags: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if flags == nil {
		flags = []domain.FeatureFlag{}
	}
	sort.Slice(flags, func(i, j int) bool { return flags[i].Name < flags[j].Name })
	service.mu.Lock()
	if service.revision == revision {
		service.cache = &featureFlagSnapshot{flags: cloneFeatureFlags(flags), expiresAt: service.now().Add(service.cacheAge)}
	}
	service.mu.Unlock()
	return cloneFeatureFlags(flags), nil
}

func cloneFeatureFlags(flags []domain.FeatureFlag) []domain.FeatureFlag {
	clone := make([]domain.FeatureFlag, len(flags))
	for i, flag := range flags {
		clone[i] = flag
		clone[i].UserIDs = append([]int64(nil), flag.UserIDs...)
	}
	return clone
}
