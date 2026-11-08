package growth

import (
	"context"
	"fmt"

	redisv9 "github.com/redis/go-redis/v9"
)

// RedisReader samples Redis memory and key counts when a client is configured.
type RedisReader struct {
	Client redisv9.UniversalClient
}

var _ RedisStats = RedisReader{}

// UsedMemory returns the used_memory INFO field.
func (r RedisReader) UsedMemory(ctx context.Context) (uint64, error) {
	if r.Client == nil {
		return 0, fmt.Errorf("metrics: redis client is required")
	}
	info, err := r.Client.Info(ctx, "memory").Result()
	if err != nil {
		return 0, fmt.Errorf("metrics: redis INFO memory: %w", err)
	}
	for _, line := range splitLines(info) {
		key, value, ok := splitKV(line)
		if !ok || key != "used_memory" {
			continue
		}
		var used uint64
		if _, err := fmt.Sscan(value, &used); err != nil {
			return 0, fmt.Errorf("metrics: parse used_memory: %w", err)
		}
		return used, nil
	}
	return 0, fmt.Errorf("metrics: used_memory missing from INFO")
}

// KeyCount returns DBSIZE for the selected Redis database.
func (r RedisReader) KeyCount(ctx context.Context) (int64, error) {
	if r.Client == nil {
		return 0, fmt.Errorf("metrics: redis client is required")
	}
	n, err := r.Client.DBSize(ctx).Result()
	if err != nil {
		return 0, fmt.Errorf("metrics: redis DBSIZE: %w", err)
	}
	return n, nil
}
