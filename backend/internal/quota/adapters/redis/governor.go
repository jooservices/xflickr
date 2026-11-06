// Package redis implements the quota governor with atomic Redis Lua scripts.
package redis

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync/atomic"
	"time"

	redisv9 "github.com/redis/go-redis/v9"

	"github.com/jooservices/xflickr/internal/quota/app"
	"github.com/jooservices/xflickr/internal/quota/domain"
	flickr "github.com/jooservices/xflickr/pkg/flickr"
)

//go:embed scripts/*.lua
var scripts embed.FS

var (
	acquireScript   = redisv9.NewScript(mustScript("scripts/acquire.lua"))
	calibrateScript = redisv9.NewScript(mustScript("scripts/calibrate.lua"))
	restoreScript   = redisv9.NewScript(mustScript("scripts/restore.lua"))
)

func mustScript(name string) string {
	content, err := scripts.ReadFile(name)
	if err != nil {
		panic(err)
	}
	common, err := scripts.ReadFile("scripts/rolling.lua")
	if err != nil {
		panic(err)
	}
	return string(common) + "\n" + string(content)
}

// Governor coordinates per-key budgets and uses SHA-cached Lua scripts.
type Governor struct {
	Client  redisv9.UniversalClient
	Prefix  string
	Budgets app.BudgetProvider
	// Rebuilder, when set, makes Acquire fail closed for a key whose counters
	// were lost, and rebuilds them before the call is allowed.
	Rebuilder interface {
		Rebuild(context.Context, int64) error
	}
	// Recorder, when set, logs every granted call to api_call_log so a rebuild
	// can recover calls made after the last flush. A failed record fails closed.
	Recorder interface {
		Record(ctx context.Context, keyID int64, class string, at time.Time) error
	}
}

var _ domain.Governor = Governor{}

// Acquire atomically checks the per-second and hourly budgets. A denied
// attempt returns its retry delay and a nil permit.
func (g Governor) Acquire(ctx context.Context, keyID int64, class domain.Class) (domain.Permit, time.Duration, error) {
	if ctx == nil || keyID <= 0 || !class.Valid() || g.Client == nil || g.Budgets == nil {
		return nil, 0, domain.ErrGovernorOffline
	}
	budget, err := g.Budgets.Budget(ctx, keyID)
	if err != nil || budget.Validate() != nil {
		return nil, 0, domain.ErrGovernorOffline
	}
	for attempt := 0; ; attempt++ {
		allowed, wait, reason, at, err := g.tryAcquire(ctx, keyID, class, budget)
		if err != nil {
			return nil, 0, err
		}
		if retry, err := g.rebuildFirst(ctx, keyID, reason, attempt); err != nil {
			return nil, 0, err
		} else if retry {
			continue
		}
		if !allowed {
			return nil, wait, nil
		}
		if err := g.record(ctx, keyID, class, at); err != nil {
			return nil, 0, err
		}
		return &permit{governor: g, keyID: keyID, budget: budget}, 0, nil
	}
}

const reasonRebuild = "rebuild"

// rebuildFirst blocks until the counters are back when the first attempt found
// them lost; the retry then sees the ready marker.
func (g Governor) rebuildFirst(ctx context.Context, keyID int64, reason string, attempt int) (bool, error) {
	if reason != reasonRebuild || g.Rebuilder == nil || attempt != 0 {
		return false, nil
	}
	if err := g.Rebuilder.Rebuild(ctx, keyID); err != nil {
		return false, domain.ErrGovernorOffline
	}
	return true, nil
}

func (g Governor) record(ctx context.Context, keyID int64, class domain.Class, at time.Time) error {
	if g.Recorder == nil {
		return nil
	}
	if err := g.Recorder.Record(ctx, keyID, string(class), at); err != nil {
		return domain.ErrGovernorOffline
	}
	return nil
}

func (g Governor) tryAcquire(ctx context.Context, keyID int64, class domain.Class, budget domain.Budget) (bool, time.Duration, string, time.Time, error) {
	requireReady := 0
	if g.Rebuilder != nil {
		requireReady = 1
	}
	result, err := acquireScript.Run(ctx, g.Client, g.keys(keyID), budget.PerSecondRate, budget.Burst, budget.HourlyLimit, budget.Reserve, budget.PersonLaneShare, string(class), requireReady, budget.IdlePhotoProtection()).Slice()
	if err != nil || len(result) < 3 {
		return false, 0, "", time.Time{}, domain.ErrGovernorOffline
	}
	allowed, okAllowed := asInt64(result[0])
	waitMS, okWait := asInt64(result[1])
	reason, _ := result[2].(string)
	if !okAllowed || !okWait {
		return false, 0, "", time.Time{}, domain.ErrGovernorOffline
	}
	var at time.Time
	if allowed == 1 {
		if len(result) != 4 {
			return false, 0, "", time.Time{}, domain.ErrGovernorOffline
		}
		ms, ok := asInt64(result[3])
		if !ok {
			return false, 0, "", time.Time{}, domain.ErrGovernorOffline
		}
		at = time.UnixMilli(ms)
	}
	return allowed == 1, time.Duration(waitMS) * time.Millisecond, reason, at, nil
}

func (g Governor) keys(keyID int64) []string {
	base := fmt.Sprintf("%squota:key:%d:", g.Prefix, keyID)
	return []string{base + "bucket", base + "rolling:interactive", base + "rolling:person", base + "cooldown", base + "rolling:photo", base + "rolling:ready", base + "photo_demand", base + "rolling:sequence"}
}

// Capacity reports conservative dispatch credits from the same hourly usage
// counter enforced by Acquire. A Redis or budget failure is treated as offline
// so the scheduler cannot dispatch without quota visibility.
func (g Governor) Capacity(ctx context.Context, keyID int64, window time.Duration) (int, error) {
	if ctx == nil || keyID <= 0 || window <= 0 || g.Client == nil || g.Budgets == nil {
		return 0, domain.ErrGovernorOffline
	}
	budget, err := g.Budgets.Budget(ctx, keyID)
	if errors.Is(err, app.ErrKeyUnavailable) {
		return 0, app.ErrKeyUnavailable
	}
	if err != nil || budget.Validate() != nil {
		return 0, domain.ErrGovernorOffline
	}
	live, err := (Counters{Client: g.Client, Prefix: g.Prefix}).Live(ctx, keyID)
	if err != nil {
		return 0, domain.ErrGovernorOffline
	}
	used := live.Used
	credits := remainingCapacity(budget, used, window)
	if credits > int64(math.MaxInt) {
		credits = int64(math.MaxInt)
	}
	return int(credits), nil
}

func remainingCapacity(budget domain.Budget, used int64, window time.Duration) int64 {
	remainingHourly := max(int64(0), budget.HourlyLimit-used-budget.Reserve)
	perSecond := max(int64(0), budget.PerSecondRate*int64(window/time.Second))
	return min(perSecond, remainingHourly)
}

type permit struct {
	governor Governor
	keyID    int64
	budget   domain.Budget
	done     atomic.Bool
}

func (p *permit) Done(meta domain.Meta) error {
	if p == nil || !p.done.CompareAndSwap(false, true) {
		return nil
	}
	if !domain.QuotaError(meta, flickr.ErrQuotaExceeded) {
		return nil
	}
	// Completion must still be recorded if the request context was canceled.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	keys := p.governor.keys(p.keyID)
	_, err := calibrateScript.Run(ctx, p.governor.Client, keys, p.budget.HourlyLimit).Result()
	if err != nil {
		return fmt.Errorf("%w: %w", domain.ErrGovernorOffline, err)
	}
	return nil
}

func asInt64(value any) (int64, bool) {
	switch number := value.(type) {
	case int64:
		return number, true
	case int:
		return int64(number), true
	case uint64:
		if number <= math.MaxInt64 {
			return int64(number), true
		}
	case string:
		parsed, err := strconv.ParseInt(number, 10, 64)
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}

// StaticBudgets is a small provider useful for a single shared policy and tests.
type StaticBudgets map[int64]domain.Budget

// Budget returns the configured budget for keyID.
func (b StaticBudgets) Budget(_ context.Context, keyID int64) (domain.Budget, error) {
	budget, ok := b[keyID]
	if !ok {
		return domain.Budget{}, errors.New("quota: key budget not found")
	}
	return budget, nil
}
