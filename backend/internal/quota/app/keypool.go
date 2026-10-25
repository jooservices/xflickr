package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jooservices/xflickr/internal/quota/domain"
)

// ErrNoKey means no eligible key can serve a person right now.
var ErrNoKey = errors.New("quota: no eligible key")

// KeyID identifies an API key in the pool.
type KeyID int64

// PoolKey is a key as the pool sees it: its state and when it last paid for a call.
type PoolKey struct {
	ID            KeyID
	Valid         bool
	CooldownUntil time.Time
	LastUsedAt    time.Time
}

// InterestReader tells which users have a crawl interest in a person. The
// crawl module provides the adapter, so the pool never reads its storage.
type InterestReader interface {
	InterestedUsers(ctx context.Context, personNSID string) ([]int64, error)
}

// KeySource lists the keys owned by a user.
type KeySource interface {
	KeysOf(ctx context.Context, userID int64) ([]PoolKey, error)
}

// QuotaReader reports the calls a key may still spend within the window. The
// governor implements it; ErrKeyUnavailable marks a key to skip.
type QuotaReader interface {
	Capacity(ctx context.Context, keyID int64, window time.Duration) (int, error)
}

// KeyPool picks the key that pays for a call about a person.
type KeyPool struct {
	Interests InterestReader
	Keys      KeySource
	Quota     QuotaReader
	Now       func() time.Time
}

// Choose returns the eligible key with the most remaining quota. A key is
// eligible when it is valid, not cooling down, has quota left, and belongs to
// a user with a crawl interest in the person. Ties go to the least recently
// used key, then the lowest ID so the result is deterministic.
func (p KeyPool) Choose(ctx context.Context, personNSID string, class domain.Class) (KeyID, error) {
	if ctx == nil || p.Interests == nil || p.Keys == nil || p.Quota == nil {
		return 0, errors.New("quota: key pool dependencies are required")
	}
	if !class.Valid() {
		return 0, domain.ErrInvalidClass
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	users, err := p.Interests.InterestedUsers(ctx, personNSID)
	if err != nil {
		return 0, fmt.Errorf("quota: load interests for %q: %w", personNSID, err)
	}
	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	var (
		best      PoolKey
		bestQuota int
		found     bool
		seenUsers = make(map[int64]bool, len(users))
		seenKeys  = map[KeyID]bool{}
	)
	for _, userID := range users {
		if seenUsers[userID] {
			continue
		}
		seenUsers[userID] = true
		keys, err := p.Keys.KeysOf(ctx, userID)
		if err != nil {
			return 0, fmt.Errorf("quota: load keys of user %d: %w", userID, err)
		}
		for _, key := range keys {
			if seenKeys[key.ID] || !key.Valid || key.CooldownUntil.After(now) {
				continue
			}
			seenKeys[key.ID] = true
			remaining, err := p.Quota.Capacity(ctx, int64(key.ID), Window)
			if errors.Is(err, ErrKeyUnavailable) {
				continue
			}
			if err != nil {
				return 0, fmt.Errorf("quota: read quota of key %d: %w", key.ID, err)
			}
			if remaining <= 0 {
				continue
			}
			if !found || better(key, remaining, best, bestQuota) {
				best, bestQuota, found = key, remaining, true
			}
		}
	}
	if !found {
		return 0, ErrNoKey
	}
	return best.ID, nil
}

func better(key PoolKey, quota int, best PoolKey, bestQuota int) bool {
	if quota != bestQuota {
		return quota > bestQuota
	}
	if !key.LastUsedAt.Equal(best.LastUsedAt) {
		return key.LastUsedAt.Before(best.LastUsedAt)
	}
	return key.ID < best.ID
}
