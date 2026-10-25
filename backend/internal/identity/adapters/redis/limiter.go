package redis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	redis "github.com/redis/go-redis/v9"
)

type Limiter struct {
	Client redis.UniversalClient
	Prefix string
	// windowSeconds is kept injectable for package tests that exercise the
	// script's EXPIRE error cleanup. Production always uses the fixed window.
	windowSeconds any
}

var incrementWithExpiry = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  local ok, err = pcall(redis.call, 'EXPIRE', KEYS[1], ARGV[1])
  if not ok then
    redis.call('DEL', KEYS[1])
    return redis.error_reply(err)
  end
end
return count
`)

func (limiter Limiter) Allow(ctx context.Context, email, ip string) (bool, error) {
	key := limiter.key(email, ip)
	windowSeconds := limiter.windowSeconds
	if windowSeconds == nil {
		windowSeconds = int64((15 * time.Minute) / time.Second)
	}
	count, err := incrementWithExpiry.Run(ctx, limiter.Client, []string{key}, windowSeconds).Int64()
	if err != nil {
		return false, err
	}
	return count <= 5, nil
}

func (limiter Limiter) RetryAfter(ctx context.Context, email, ip string) (time.Duration, error) {
	remaining, err := limiter.Client.PTTL(ctx, limiter.key(email, ip)).Result()
	if err != nil {
		return 0, err
	}
	if remaining < 0 {
		return 0, nil
	}
	return remaining, nil
}

func (limiter Limiter) key(email, ip string) string {
	keyHash := sha256.Sum256([]byte(email + "\x00" + ip))
	return limiter.Prefix + "auth:login:" + hex.EncodeToString(keyHash[:])
}
