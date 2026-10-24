package redis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	firstAllowed, err := limiter.Allow(context.Background(), "ttl@example.com", "127.0.0.1")
	if err != nil || !firstAllowed {
		t.Fatalf("initial attempt allowed=%v err=%v", firstAllowed, err)
	}
	keyHash := sha256.Sum256([]byte("ttl@example.com\x00127.0.0.1"))
	if ttl := server.TTL("unit:auth:login:" + hex.EncodeToString(keyHash[:])); ttl != 15*time.Minute {
		t.Fatalf("initial TTL = %s, want 15m", ttl)
	}
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

func TestLimiterRemovesCounterWhenAtomicExpiryFails(t *testing.T) {
	server := miniredis.RunT(t)
	client := redisv9.NewClient(&redisv9.Options{Addr: server.Addr()})
	defer client.Close()
	limiter := Limiter{Client: client, Prefix: "unit:", windowSeconds: "invalid"}
	if _, err := limiter.Allow(context.Background(), "user@example.com", "127.0.0.1"); err == nil {
		t.Fatal("Allow() succeeded when EXPIRE failed")
	}
	keyHash := sha256.Sum256([]byte("user@example.com\x00127.0.0.1"))
	key := "unit:auth:login:" + hex.EncodeToString(keyHash[:])
	if server.Exists(key) {
		t.Fatalf("counter %q remained after script expiry failure", key)
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
