package redis

import (
	"context"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	redisv9 "github.com/redis/go-redis/v9"
)

func TestLimiterAllowsFiveAttemptsAndExpires(t *testing.T) {
	server := miniredis.RunT(t)
	client := redisv9.NewClient(&redisv9.Options{Addr: server.Addr()})
	defer client.Close()
	limiter := Limiter{Client: client, Prefix: "unit:"}
	for i := 0; i < 5; i++ {
		allowed, err := limiter.Allow(context.Background(), "user@example.com", "127.0.0.1")
		if err != nil || !allowed {
			t.Fatalf("attempt %d allowed=%v err=%v", i+1, allowed, err)
		}
	}
	allowed, err := limiter.Allow(context.Background(), "user@example.com", "127.0.0.1")
	if err != nil || allowed {
		t.Fatalf("sixth attempt allowed=%v err=%v", allowed, err)
	}
	allowed, err = limiter.Allow(context.Background(), "user@example.com", "127.0.0.2")
	if err != nil || !allowed {
		t.Fatalf("different IP allowed=%v err=%v", allowed, err)
	}
	server.FastForward(15 * time.Minute)
	allowed, err = limiter.Allow(context.Background(), "user@example.com", "127.0.0.1")
	if err != nil || !allowed {
		t.Fatalf("after expiry allowed=%v err=%v", allowed, err)
	}
}

func TestLimiterReturnsRedisErrors(t *testing.T) {
	server := miniredis.RunT(t)
	client := redisv9.NewClient(&redisv9.Options{Addr: server.Addr()})
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Limiter{Client: client}).Allow(ctx, "user@example.com", "127.0.0.1"); err == nil {
		t.Fatal("Allow() succeeded with canceled context")
	}
}
