// Package redis keeps started Flickr authorizations until the callback.
package redis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	redisv9 "github.com/redis/go-redis/v9"

	"github.com/jooservices/xflickr/internal/accounts/app"
)

// States stores pending authorizations under the configured Redis prefix. The
// state itself is never a key: the key holds its SHA-256 hash.
type States struct {
	Client redisv9.UniversalClient
	Prefix string
}

var _ app.StateStore = States{}

// Save stores the pending authorization for ttl.
func (s States) Save(ctx context.Context, state string, pending app.Pending, ttl time.Duration) error {
	if s.Client == nil {
		return errors.New("accounts: redis client is required")
	}
	if ttl <= 0 {
		return errors.New("accounts: oauth state ttl must be positive")
	}
	encoded, err := json.Marshal(pending)
	if err != nil {
		return fmt.Errorf("accounts: encode oauth state: %w", err)
	}
	if err := s.Client.Set(ctx, s.key(state), encoded, ttl).Err(); err != nil {
		return fmt.Errorf("accounts: save oauth state: %w", err)
	}
	return nil
}

// Take reads and deletes the entry in one step, so a state works once.
func (s States) Take(ctx context.Context, state string) (app.Pending, error) {
	if s.Client == nil {
		return app.Pending{}, errors.New("accounts: redis client is required")
	}
	value, err := s.Client.GetDel(ctx, s.key(state)).Bytes()
	if errors.Is(err, redisv9.Nil) {
		return app.Pending{}, app.ErrInvalidState
	}
	if err != nil {
		return app.Pending{}, fmt.Errorf("accounts: take oauth state: %w", err)
	}
	var pending app.Pending
	if err := json.Unmarshal(value, &pending); err != nil {
		return app.Pending{}, app.ErrInvalidState
	}
	return pending, nil
}

func (s States) key(state string) string {
	sum := sha256.Sum256([]byte(state))
	return s.Prefix + "accounts:oauth:" + hex.EncodeToString(sum[:])
}
