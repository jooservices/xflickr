package redis

import (
	"errors"
	"strings"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/jooservices/xflickr/internal/accounts/app"
	"github.com/jooservices/xflickr/internal/platform/crypto"
	redisv9 "github.com/redis/go-redis/v9"
)

func newStates(t *testing.T) (States, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redisv9.NewClient(&redisv9.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return States{Client: client, Prefix: "xflickr_wt98:"}, server
}

func samplePending() app.Pending {
	return app.Pending{UserID: 7, KeyID: 4, RequestToken: "request-token", RequestSecret: crypto.Sealed{KeyID: "k", Nonce: []byte{1}, Ciphertext: []byte{2}, WrappedDEK: []byte{3}}}
}

func TestStatesSaveThenTakeWorksOnce(t *testing.T) {
	states, server := newStates(t)
	if err := states.Save(t.Context(), "state-one", samplePending(), 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	keys := server.Keys()
	if len(keys) != 1 || !strings.HasPrefix(keys[0], "xflickr_wt98:accounts:oauth:") || strings.Contains(keys[0], "state-one") {
		t.Fatalf("keys = %v", keys)
	}
	if ttl := server.TTL(keys[0]); ttl != 10*time.Minute {
		t.Fatalf("ttl = %s", ttl)
	}
	got, err := states.Take(t.Context(), "state-one")
	if err != nil || got.UserID != 7 || got.KeyID != 4 || got.RequestToken != "request-token" || got.RequestSecret.KeyID != "k" {
		t.Fatalf("take = %+v %v", got, err)
	}
	if _, err := states.Take(t.Context(), "state-one"); !errors.Is(err, app.ErrInvalidState) {
		t.Fatalf("second take = %v", err)
	}
}

func TestStatesExpireAfterTheTTL(t *testing.T) {
	states, server := newStates(t)
	if err := states.Save(t.Context(), "state-one", samplePending(), app.StateTTL); err != nil {
		t.Fatal(err)
	}
	server.FastForward(app.StateTTL + time.Second)
	if _, err := states.Take(t.Context(), "state-one"); !errors.Is(err, app.ErrInvalidState) {
		t.Fatalf("expired take = %v", err)
	}
}

func TestStatesRejectBadInputAndFailures(t *testing.T) {
	states, server := newStates(t)
	if err := states.Save(t.Context(), "s", samplePending(), 0); err == nil {
		t.Fatal("zero ttl was accepted")
	}
	if err := (States{}).Save(t.Context(), "s", samplePending(), time.Minute); err == nil {
		t.Fatal("missing client accepted by save")
	}
	if _, err := (States{}).Take(t.Context(), "s"); err == nil {
		t.Fatal("missing client accepted by take")
	}
	server.Set(states.key("garbage"), "{not json")
	if _, err := states.Take(t.Context(), "garbage"); !errors.Is(err, app.ErrInvalidState) {
		t.Fatalf("garbage take = %v", err)
	}
	server.Close()
	if err := states.Save(t.Context(), "s", samplePending(), time.Minute); err == nil {
		t.Fatal("save ignored a Redis failure")
	}
	if _, err := states.Take(t.Context(), "s"); err == nil || errors.Is(err, app.ErrInvalidState) {
		t.Fatalf("take on a Redis failure = %v", err)
	}
}
