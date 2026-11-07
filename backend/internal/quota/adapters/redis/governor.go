// Package redis implements the quota governor with atomic Redis Lua scripts.
package redis

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
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
	unlockScript    = redisv9.NewScript(mustFile("scripts/unlock.lua"))
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

func mustFile(name string) string {
	content, err := scripts.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return string(content)
}

// KeyRebuilder restores a key's rolling ledger before Acquire may grant.
type KeyRebuilder interface {
	Rebuild(context.Context, int64) error
}

// CallRecorder persists each granted call for later ledger rebuilds.
type CallRecorder interface {
	Record(ctx context.Context, keyID int64, class string, at time.Time) error
}

// Governor coordinates per-key budgets and uses SHA-cached Lua scripts.
type Governor struct {
	Client    redisv9.UniversalClient
	Prefix    string
	Budgets   app.BudgetProvider
	Rebuilder KeyRebuilder
	// Recorder, when set, logs every granted call to api_call_log so a rebuild
	// can recover calls made after the last flush. A failed record fails closed.
	Recorder CallRecorder
}

var _ domain.Governor = Governor{}

// NewGovernor builds a governor that always fails closed until a Rebuilder has
// restored the rolling ledger. The rebuilder is required for every role.
func NewGovernor(client redisv9.UniversalClient, prefix string, budgets app.BudgetProvider, rebuilder KeyRebuilder, recorder CallRecorder) (Governor, error) {
	if client == nil || budgets == nil || rebuilder == nil {
		return Governor{}, errors.New("quota: governor requires client, budgets and rebuilder")
	}
	return Governor{Client: client, Prefix: prefix, Budgets: budgets, Rebuilder: rebuilder, Recorder: recorder}, nil
}

// Acquire atomically checks the per-second and hourly budgets. A denied
// attempt returns its retry delay and a nil permit.
func (g Governor) Acquire(ctx context.Context, keyID int64, class domain.Class) (domain.Permit, time.Duration, error) {
	if !g.canAcquire(ctx, keyID, class) {
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
			return nil, denyWait(reason, wait), nil
		}
		if err := g.record(ctx, keyID, class, at); err != nil {
			return nil, 0, err
		}
		return &permit{governor: g, keyID: keyID, budget: budget}, 0, nil
	}
}

func (g Governor) canAcquire(ctx context.Context, keyID int64, class domain.Class) bool {
	return ctx != nil && keyID > 0 && class.Valid() && g.Client != nil && g.Budgets != nil && g.Rebuilder != nil
}

func denyWait(reason string, wait time.Duration) time.Duration {
	if reason == reasonRebuild {
		return domain.LedgerNotReadyRetry
	}
	return wait
}

const (
	reasonRebuild    = "rebuild"
	rebuildLockTTL   = 30 * time.Second
	rebuildPollEvery = 50 * time.Millisecond
)

// rebuildFirst runs one Redis-locked rebuild when the ledger is not ready, then
// retries Acquire. Concurrent roles share the lock so only one rebuild runs.
func (g Governor) rebuildFirst(ctx context.Context, keyID int64, reason string, attempt int) (bool, error) {
	if reason != reasonRebuild || attempt != 0 {
		return false, nil
	}
	if err := g.rebuildWithLock(ctx, keyID); err != nil {
		return false, domain.ErrGovernorOffline
	}
	return true, nil
}

func (g Governor) rebuildWithLock(ctx context.Context, keyID int64) error {
	readyKey := g.keys(keyID)[5]
	if ready, err := g.ready(ctx, readyKey); err != nil || ready {
		return err
	}
	lockKey := fmt.Sprintf("%squota:key:%d:rolling:rebuild_lock", g.Prefix, keyID)
	token, err := rebuildLockToken()
	if err != nil {
		return fmt.Errorf("quota: rebuild lock token: %w", err)
	}
	ok, err := g.Client.SetNX(ctx, lockKey, token, rebuildLockTTL).Result()
	if err != nil {
		return fmt.Errorf("quota: rebuild lock: %w", err)
	}
	if ok {
		defer func() {
			_ = unlockScript.Run(context.Background(), g.Client, []string{lockKey}, token).Err()
		}()
		return g.rebuildIfNeeded(ctx, keyID, readyKey)
	}
	if err := g.waitReady(ctx, readyKey, lockKey, rebuildLockTTL); err != nil {
		return err
	}
	return g.rebuildIfNeeded(ctx, keyID, readyKey)
}

func rebuildLockToken() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func (g Governor) rebuildIfNeeded(ctx context.Context, keyID int64, readyKey string) error {
	if ready, err := g.ready(ctx, readyKey); err != nil || ready {
		return err
	}
	if err := g.Rebuilder.Rebuild(ctx, keyID); err != nil {
		return fmt.Errorf("quota: rebuild key %d: %w", keyID, err)
	}
	return nil
}

func (g Governor) waitReady(ctx context.Context, readyKey, lockKey string, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if ready, err := g.ready(ctx, readyKey); err != nil || ready {
			return err
		}
		held, err := g.Client.Exists(ctx, lockKey).Result()
		if err != nil {
			return fmt.Errorf("quota: rebuild lock: %w", err)
		}
		if held == 0 {
			return nil
		}
		timer := time.NewTimer(rebuildPollEvery)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func (g Governor) ready(ctx context.Context, readyKey string) (bool, error) {
	n, err := g.Client.Exists(ctx, readyKey).Result()
	if err != nil {
		return false, fmt.Errorf("quota: ready marker: %w", err)
	}
	return n == 1, nil
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
	result, err := acquireScript.Run(ctx, g.Client, g.keys(keyID), budget.PerSecondRate, budget.Burst, budget.HourlyLimit, budget.Reserve, budget.PersonLaneShare, string(class), budget.IdlePhotoProtection()).Slice()
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
