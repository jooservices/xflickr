package growth

import (
	"context"
	"fmt"

	redisv9 "github.com/redis/go-redis/v9"
)

// MemoryClient reads Redis INFO memory and DBSIZE.
type MemoryClient interface {
	MemoryInfo(ctx context.Context) (string, error)
	DBSize(ctx context.Context) (int64, error)
}

// RedisReader samples Redis memory and key counts when a client is configured.
type RedisReader struct {
	Client MemoryClient
}

var _ RedisStats = RedisReader{}

// GoRedisMemory adapts a go-redis client to MemoryClient.
type GoRedisMemory struct {
	Client redisv9.UniversalClient
}

// MemoryInfo returns the INFO memory payload.
func (g GoRedisMemory) MemoryInfo(ctx context.Context) (string, error) {
	if g.Client == nil {
		return "", fmt.Errorf("metrics: redis client is required")
	}
	info, err := g.Client.Info(ctx).Result()
	if err != nil {
		return "", fmt.Errorf("metrics: redis INFO: %w", err)
	}
	return info, nil
}

// DBSize returns the selected database key count.
func (g GoRedisMemory) DBSize(ctx context.Context) (int64, error) {
	if g.Client == nil {
		return 0, fmt.Errorf("metrics: redis client is required")
	}
	n, err := g.Client.DBSize(ctx).Result()
	if err != nil {
		return 0, fmt.Errorf("metrics: redis DBSIZE: %w", err)
	}
	return n, nil
}

// UsedMemory returns the used_memory INFO field.
func (r RedisReader) UsedMemory(ctx context.Context) (uint64, error) {
	if r.Client == nil {
		return 0, fmt.Errorf("metrics: redis client is required")
	}
	info, err := r.Client.MemoryInfo(ctx)
	if err != nil {
		return 0, err
	}
	return ParseUsedMemory(info)
}

// KeyCount returns DBSIZE for the selected Redis database.
func (r RedisReader) KeyCount(ctx context.Context) (int64, error) {
	if r.Client == nil {
		return 0, fmt.Errorf("metrics: redis client is required")
	}
	n, err := r.Client.DBSize(ctx)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// ParseUsedMemory extracts used_memory from a Redis INFO memory payload.
func ParseUsedMemory(info string) (uint64, error) {
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
