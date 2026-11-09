package roles

import (
	"net"
	"strconv"

	redisv9 "github.com/redis/go-redis/v9"

	"github.com/jooservices/xflickr/internal/platform/config"
)

// redisOptions maps the Redis settings to client options. REDIS_USERNAME selects
// a Redis ACL user (for example the key-prefix restricted production user); an
// empty username keeps the legacy password-only AUTH of the default user.
func redisOptions(cfg config.Config) *redisv9.Options {
	return &redisv9.Options{
		Addr:     net.JoinHostPort(cfg.Redis.Host, strconv.Itoa(cfg.Redis.Port)),
		Username: cfg.Redis.Username,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	}
}

// newRedisClient is the only place that builds a Redis client, so every role
// authenticates the same way.
func newRedisClient(cfg config.Config) *redisv9.Client {
	return redisv9.NewClient(redisOptions(cfg))
}
