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
}

func (limiter Limiter) Allow(ctx context.Context, email, ip string) (bool, error) {
	keyHash := sha256.Sum256([]byte(email + "\x00" + ip))
	key := limiter.Prefix + "auth:login:" + hex.EncodeToString(keyHash[:])
	count, err := limiter.Client.Incr(ctx, key).Result()
	if err != nil {
		return false, err
	}
	if count == 1 {
		if err := limiter.Client.Expire(ctx, key, 15*time.Minute).Err(); err != nil {
			return false, err
		}
	}
	return count <= 5, nil
}
