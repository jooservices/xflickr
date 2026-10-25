package redis

import (
	"context"
	"errors"
	"strconv"
	"time"

	redisv9 "github.com/redis/go-redis/v9"

	"github.com/jooservices/xflickr/internal/quota/app"
)

// Counters reads and restores a key's usage counters for the flusher and the
// rebuilder.
type Counters struct {
	Client redisv9.UniversalClient
	Prefix string
}

var _ app.CounterStore = Counters{}

// Snapshot reads the hourly counter, window start and per-class counts. ok is
// false when the window counters are missing, as after a Redis loss.
func (c Counters) Snapshot(ctx context.Context, keyID int64) (app.Usage, bool, error) {
	if ctx == nil || keyID <= 0 || c.Client == nil {
		return app.Usage{}, false, errors.New("quota: counters are not configured")
	}
	keys := Governor{Prefix: c.Prefix}.keys(keyID)
	pipe := c.Client.Pipeline()
	used := pipe.Get(ctx, keys[1])
	start := pipe.Get(ctx, keys[2])
	byClass := pipe.HGetAll(ctx, keys[4])
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redisv9.Nil) {
		return app.Usage{}, false, err
	}
	startMS, err := start.Int64()
	if errors.Is(err, redisv9.Nil) || (err == nil && startMS == 0) {
		return app.Usage{}, false, nil
	}
	if err != nil {
		return app.Usage{}, false, err
	}
	usedN, err := used.Int64()
	if err != nil && !errors.Is(err, redisv9.Nil) {
		return app.Usage{}, false, err
	}
	usage := app.Usage{KeyID: keyID, WindowStart: time.UnixMilli(startMS), Used: usedN, ByClass: map[string]int64{}}
	for class, raw := range byClass.Val() {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return app.Usage{}, false, err
		}
		usage.ByClass[class] = n
	}
	return usage, true, nil
}

// Restore writes usage back through restore.lua. It returns false when the
// counters were already ready.
func (c Counters) Restore(ctx context.Context, usage app.Usage) (bool, error) {
	if ctx == nil || usage.KeyID <= 0 || c.Client == nil {
		return false, errors.New("quota: counters are not configured")
	}
	keys := Governor{Prefix: c.Prefix}.keys(usage.KeyID)
	args := []any{0, 0}
	if !usage.WindowStart.IsZero() {
		args[0], args[1] = usage.WindowStart.UnixMilli(), usage.Used
		for class, n := range usage.ByClass {
			args = append(args, class, n)
		}
	}
	wrote, err := restoreScript.Run(ctx, c.Client, []string{keys[1], keys[2], keys[4], keys[5]}, args...).Int64()
	if err != nil {
		return false, err
	}
	return wrote == 1, nil
}

var _ app.LiveReader = Counters{}

// Live reads the open window, per-class counts and cooldown of a key without
// changing them. Missing counters read as an empty window.
func (c Counters) Live(ctx context.Context, keyID int64) (app.Live, error) {
	if ctx == nil || keyID <= 0 || c.Client == nil {
		return app.Live{}, errors.New("quota: counters are not configured")
	}
	keys := Governor{Prefix: c.Prefix}.keys(keyID)
	pipe := c.Client.Pipeline()
	used := pipe.Get(ctx, keys[1])
	start := pipe.Get(ctx, keys[2])
	cooldown := pipe.Get(ctx, keys[3])
	byClass := pipe.HGetAll(ctx, keys[4])
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redisv9.Nil) {
		return app.Live{}, err
	}
	live := app.Live{ByClass: map[string]int64{}}
	var err error
	if live.Used, err = optionalInt(used); err != nil {
		return app.Live{}, err
	}
	startMS, err := optionalInt(start)
	if err != nil {
		return app.Live{}, err
	}
	cooldownMS, err := optionalInt(cooldown)
	if err != nil {
		return app.Live{}, err
	}
	if startMS > 0 {
		live.WindowStart = time.UnixMilli(startMS)
	}
	if cooldownMS > 0 {
		live.CooldownUntil = time.UnixMilli(cooldownMS)
	}
	for class, raw := range byClass.Val() {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return app.Live{}, err
		}
		live.ByClass[class] = n
	}
	return live, nil
}

func optionalInt(command *redisv9.StringCmd) (int64, error) {
	n, err := command.Int64()
	if errors.Is(err, redisv9.Nil) {
		return 0, nil
	}
	return n, err
}
