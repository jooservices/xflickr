package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jooservices/xflickr/internal/quota/domain"
)

// ErrKeyNotFound means the key does not exist, is not usable, or belongs to
// another user.
var ErrKeyNotFound = errors.New("quota: key not found")

// KeyInfo identifies a key whose state is reported.
type KeyInfo struct {
	ID     int64
	UserID int64
	Label  string
}

// KeyDirectory lists the keys that can report state.
type KeyDirectory interface {
	// Owned returns the key when it is usable and belongs to userID.
	Owned(ctx context.Context, userID, keyID int64) (KeyInfo, bool, error)
	// Active lists every usable key.
	Active(context.Context) ([]KeyInfo, error)
}

// Live is a key's current window as held in Redis. WindowStart is zero when no
// window is open.
type Live struct {
	WindowStart   time.Time
	Used          int64
	ByClass       map[string]int64
	CooldownUntil time.Time
}

// LiveReader reads the Redis counters of a key.
type LiveReader interface {
	Live(ctx context.Context, keyID int64) (Live, error)
}

// UsageHistory finds the usage of the window before the current one.
type UsageHistory interface {
	// Previous returns the newest window that started in [notBefore, before).
	Previous(ctx context.Context, keyID int64, notBefore, before time.Time) (Usage, bool, error)
}

// LaneUsage counts granted calls per lane in the current window.
type LaneUsage struct {
	Interactive int64
	Person      int64
	Photo       int64
}

// KeyState is the quota state of one key.
type KeyState struct {
	Key     KeyInfo
	Used    int64
	Limit   int64
	Reserve int64
	// ResetAt is the end of the open window; zero when no window is open.
	ResetAt time.Time
	State   domain.State
	Trend   domain.Trend
	Lanes   LaneUsage
}

// States builds KeyState values from live counters, budgets and history.
type States struct {
	Keys    KeyDirectory
	Budgets BudgetProvider
	Live    LiveReader
	History UsageHistory
	Now     func() time.Time
}

// ForOwner returns the state of one key owned by userID.
func (s States) ForOwner(ctx context.Context, userID, keyID int64) (KeyState, error) {
	if err := s.ready(ctx); err != nil {
		return KeyState{}, err
	}
	key, ok, err := s.Keys.Owned(ctx, userID, keyID)
	if err != nil {
		return KeyState{}, err
	}
	if !ok {
		return KeyState{}, ErrKeyNotFound
	}
	state, err := s.state(ctx, key)
	if errors.Is(err, ErrKeyUnavailable) {
		return KeyState{}, ErrKeyNotFound
	}
	return state, err
}

// All returns the state of every usable key. A key that became unusable while
// reading is skipped.
func (s States) All(ctx context.Context) ([]KeyState, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	keys, err := s.Keys.Active(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]KeyState, 0, len(keys))
	for _, key := range keys {
		state, err := s.state(ctx, key)
		if errors.Is(err, ErrKeyUnavailable) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, state)
	}
	return out, nil
}

func (s States) ready(ctx context.Context) error {
	if ctx == nil || s.Keys == nil || s.Budgets == nil || s.Live == nil || s.History == nil {
		return errors.New("quota: state service is not configured")
	}
	return nil
}

func (s States) state(ctx context.Context, key KeyInfo) (KeyState, error) {
	budget, err := s.Budgets.Budget(ctx, key.ID)
	if err != nil {
		return KeyState{}, err
	}
	live, err := s.Live.Live(ctx, key.ID)
	if err != nil {
		return KeyState{}, fmt.Errorf("quota: read live counters for key %d: %w", key.ID, err)
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	open := !live.WindowStart.IsZero() && now.Before(live.WindowStart.Add(Window))
	out := KeyState{Key: key, Limit: budget.HourlyLimit, Reserve: budget.Reserve}
	reference, elapsed := now, Window
	if open {
		out.Used = live.Used
		out.ResetAt = live.WindowStart.Add(Window)
		out.Lanes = LaneUsage{
			Interactive: live.ByClass[string(domain.Interactive)],
			Person:      live.ByClass[string(domain.Person)],
			Photo:       live.ByClass[string(domain.Photo)],
		}
		reference, elapsed = live.WindowStart, now.Sub(live.WindowStart)
	}
	out.State = domain.Classify(out.Used, out.Limit, open && live.CooldownUntil.After(now))
	previous, hasPrevious, err := s.History.Previous(ctx, key.ID, reference.Add(-2*Window), reference)
	if err != nil {
		return KeyState{}, fmt.Errorf("quota: read usage history for key %d: %w", key.ID, err)
	}
	out.Trend = domain.TrendOf(out.Used, previous.Used, hasPrevious, elapsed, Window)
	return out, nil
}
