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
)

func mustScript(name string) string {
	content, err := scripts.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return string(content)
}

// Governor coordinates per-key budgets and uses SHA-cached Lua scripts.
type Governor struct {
	Client  redisv9.UniversalClient
	Prefix  string
	Budgets app.BudgetProvider
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
	keys := g.keys(keyID)
	result, err := acquireScript.Run(ctx, g.Client, keys, budget.PerSecondRate, budget.Burst, budget.HourlyLimit, budget.Reserve, budget.PersonLaneShare, string(class)).Slice()
	if err != nil || len(result) < 2 {
		return nil, 0, domain.ErrGovernorOffline
	}
	allowed, okAllowed := asInt64(result[0])
	waitMS, okWait := asInt64(result[1])
	if !okAllowed || !okWait {
		return nil, 0, domain.ErrGovernorOffline
	}
	wait := time.Duration(waitMS) * time.Millisecond
	if allowed != 1 {
		return nil, wait, nil
	}
	return &permit{governor: g, keyID: keyID, budget: budget}, 0, nil
}

func (g Governor) keys(keyID int64) []string {
	base := fmt.Sprintf("%squota:key:%d:", g.Prefix, keyID)
	return []string{base + "bucket", base + "hourly", base + "window", base + "cooldown"}
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
	_, err := calibrateScript.Run(ctx, p.governor.Client, keys[1:4:4], p.budget.HourlyLimit).Result()
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
