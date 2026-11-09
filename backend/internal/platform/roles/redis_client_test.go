package roles

import (
	"testing"

	"github.com/jooservices/xflickr/internal/platform/config"
)

func TestRedisOptionsCarryTheACLUser(t *testing.T) {
	cfg := config.Config{Redis: config.RedisConfig{Host: "redis.internal", Port: 6379, DB: 2, Username: "xflickr", Password: "secret"}}
	got := redisOptions(cfg)
	if got.Addr != "redis.internal:6379" || got.Username != "xflickr" || got.Password != "secret" || got.DB != 2 {
		t.Fatalf("options = addr %q user %q db %d (password set: %t)", got.Addr, got.Username, got.DB, got.Password == "secret")
	}
}

func TestRedisOptionsWithoutUsernameKeepLegacyPasswordAuth(t *testing.T) {
	cfg := config.Config{Redis: config.RedisConfig{Host: "127.0.0.1", Port: 16382, DB: 7, Password: "secret"}}
	got := redisOptions(cfg)
	if got.Username != "" || got.Password != "secret" || got.Addr != "127.0.0.1:16382" {
		t.Fatalf("legacy options changed: user %q addr %q", got.Username, got.Addr)
	}
}

func TestNewRedisClientUsesTheSameOptions(t *testing.T) {
	cfg := config.Config{Redis: config.RedisConfig{Host: "127.0.0.1", Port: 1, Username: "xflickr", Password: "secret"}}
	client := newRedisClient(cfg)
	t.Cleanup(func() { _ = client.Close() })
	if got := client.Options(); got.Username != "xflickr" || got.Addr != "127.0.0.1:1" {
		t.Fatalf("client options = user %q addr %q", got.Username, got.Addr)
	}
}
