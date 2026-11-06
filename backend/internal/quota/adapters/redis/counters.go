package redis

import (
	"context"
	"errors"
	"time"

	redisv9 "github.com/redis/go-redis/v9"

	"github.com/jooservices/xflickr/internal/quota/app"
)

// Counters reads and restores rolling usage for the flusher and rebuilder.
type Counters struct {
	Client redisv9.UniversalClient
	Prefix string
}

var (
	_          app.CounterStore = Counters{}
	_          app.LiveReader   = Counters{}
	readScript                  = redisv9.NewScript(mustScript("scripts/read.lua"))
)

func (c Counters) read(ctx context.Context, keyID int64) ([]int64, error) {
	if ctx == nil || keyID <= 0 || c.Client == nil {
		return nil, errors.New("quota: counters are not configured")
	}
	result, err := readScript.Run(ctx, c.Client, Governor{Prefix: c.Prefix}.keys(keyID)).Slice()
	if err != nil {
		return nil, err
	}
	if len(result) != 7 {
		return nil, errors.New("quota: invalid snapshot")
	}
	values := make([]int64, len(result))
	for i, v := range result {
		n, ok := asInt64(v)
		if !ok {
			return nil, errors.New("quota: invalid counter")
		}
		values[i] = n
	}
	return values, nil
}

// Snapshot persists the start of the rolling interval, rather than a fixed hour.
func (c Counters) Snapshot(ctx context.Context, keyID int64) (app.Usage, bool, error) {
	v, err := c.read(ctx, keyID)
	if err != nil {
		return app.Usage{}, false, err
	}
	if v[6] == 0 {
		return app.Usage{}, false, nil
	}
	classes := map[string]int64{}
	for i, class := range []string{"interactive", "person", "photo"} {
		if v[i+2] > 0 {
			classes[class] = v[i+2]
		}
	}
	return app.Usage{KeyID: keyID, WindowStart: time.UnixMilli(v[0]).Add(-time.Hour), Used: v[2] + v[3] + v[4], ByClass: classes}, true, nil
}

// Restore retains existing counts and replays exact grant timestamps.
func (c Counters) Restore(ctx context.Context, usage app.Usage) (bool, error) {
	if ctx == nil || usage.KeyID <= 0 || c.Client == nil {
		return false, errors.New("quota: counters are not configured")
	}
	args := make([]any, 0, len(usage.Grants)*3)
	for _, grant := range usage.Grants {
		args = append(args, grant.Class, grant.At.UnixMilli(), grant.Member)
	}
	n, err := restoreScript.Run(ctx, c.Client, Governor{Prefix: c.Prefix}.keys(usage.KeyID), args...).Int64()
	return n == 1, err
}

// Live reports the earliest expiry separately from the rolling interval start.
func (c Counters) Live(ctx context.Context, keyID int64) (app.Live, error) {
	v, err := c.read(ctx, keyID)
	if err != nil {
		return app.Live{}, err
	}
	out := app.Live{Used: v[2] + v[3] + v[4], ByClass: map[string]int64{}}
	if out.Used > 0 {
		out.WindowStart = time.UnixMilli(v[0]).Add(-time.Hour)
		out.ResetAt = time.UnixMilli(v[1]).Add(time.Hour)
	}
	for i, class := range []string{"interactive", "person", "photo"} {
		if v[i+2] > 0 {
			out.ByClass[class] = v[i+2]
		}
	}
	if v[5] > 0 {
		out.CooldownUntil = time.UnixMilli(v[5])
	}
	return out, nil
}
