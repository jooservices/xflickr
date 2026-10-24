// Package app implements the API key use cases: validate, seal, update, and delete.
package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jooservices/xflickr/internal/accounts/domain"
	"github.com/jooservices/xflickr/internal/platform/crypto"
)

var (
	// ErrKeyInvalid means Flickr rejected the key, or the key or secret is
	// empty. Nothing is stored.
	ErrKeyInvalid = errors.New("accounts: flickr api key is invalid")
	// ErrKeyCheckFailed means the Flickr check did not complete. Nothing is stored.
	ErrKeyCheckFailed = errors.New("accounts: flickr key check failed")
	// ErrNotFound means the key does not exist for this user.
	ErrNotFound = errors.New("accounts: api key not found")
	// ErrKeyExists means this user already stored the same public key.
	ErrKeyExists = errors.New("accounts: api key already exists")
	// ErrInvalidInput means the label or request shape was rejected.
	ErrInvalidInput = errors.New("accounts: invalid api key input")
)

// Probe checks a Flickr API key with one cheap call before it is stored.
type Probe interface {
	Echo(context.Context, string) error
}

// Sealer seals a secret with the associated data for that key row.
type Sealer interface {
	Seal(plaintext, aad []byte) (crypto.Sealed, error)
}

// Settings reads the administrator caps that bound key limits.
type Settings interface {
	Policy(context.Context, int64) (domain.Policy, error)
}

// Repository stores keys and publishes deletion in the same transaction.
// List and Get never return the sealed secret.
type Repository interface {
	Create(context.Context, domain.APIKey, func(int64) (crypto.Sealed, error)) (domain.APIKey, error)
	Update(context.Context, int64, int64, func(domain.APIKey) (domain.APIKey, *crypto.Sealed, error)) (domain.APIKey, error)
	Delete(context.Context, int64, int64) error
	List(context.Context, int64) ([]domain.APIKey, error)
	Get(context.Context, int64, int64) (domain.APIKey, error)
}

// CreateInput is a new key supplied by the signed-in user.
type CreateInput struct {
	Label          string
	Key            string
	Secret         string
	PerHourLimit   *int
	PerSecondLimit *int
	Burst          *int
}

// UpdateInput changes label, limits, or the secret. Nil fields stay as stored.
// The public key cannot be changed.
type UpdateInput struct {
	Label          *string
	Secret         *string
	PerHourLimit   *int
	PerSecondLimit *int
	Burst          *int
}

// Keys is the API key use case set.
type Keys struct {
	Repository Repository
	Probe      Probe
	Sealer     Sealer
	Settings   Settings
}

// Create checks the key with flickr.test.echo, then seals the secret and
// stores the row. A rejected key returns ErrKeyInvalid and writes nothing.
func (k Keys) Create(ctx context.Context, userID int64, input CreateInput) (domain.APIKey, error) {
	if err := k.ready(ctx); err != nil {
		return domain.APIKey{}, err
	}
	label, err := domain.NormalizeLabel(input.Label)
	if err != nil {
		return domain.APIKey{}, ErrInvalidInput
	}
	publicKey, err := domain.NormalizeCredential(input.Key)
	if err != nil {
		return domain.APIKey{}, ErrKeyInvalid
	}
	secret, err := domain.NormalizeCredential(input.Secret)
	if err != nil {
		return domain.APIKey{}, ErrKeyInvalid
	}
	policy, err := k.Settings.Policy(ctx, userID)
	if err != nil {
		return domain.APIKey{}, err
	}
	limits, err := policy.Resolve(input.PerHourLimit, input.PerSecondLimit, input.Burst)
	if err != nil {
		return domain.APIKey{}, err
	}
	if err := k.Probe.Echo(ctx, publicKey); err != nil {
		if errors.Is(err, ErrKeyInvalid) {
			return domain.APIKey{}, ErrKeyInvalid
		}
		// The key and secret stay out of the log. Nothing is stored.
		slog.WarnContext(ctx, "accounts: flickr key check failed")
		return domain.APIKey{}, ErrKeyCheckFailed
	}
	return k.Repository.Create(ctx, domain.APIKey{
		UserID:         userID,
		Label:          label,
		PublicKey:      publicKey,
		PerHourLimit:   limits.PerHour,
		PerSecondLimit: limits.PerSecond,
		Burst:          limits.Burst,
		Status:         domain.StatusValid,
	}, func(id int64) (crypto.Sealed, error) {
		return k.seal(secret, id)
	})
}

// Update changes label and limits within the administrator caps. A new secret
// is sealed with the key id and the repository marks connected accounts
// reconnect_required.
func (k Keys) Update(ctx context.Context, userID, id int64, input UpdateInput) (domain.APIKey, error) {
	if err := k.ready(ctx); err != nil {
		return domain.APIKey{}, err
	}
	policy, err := k.Settings.Policy(ctx, userID)
	if err != nil {
		return domain.APIKey{}, err
	}
	return k.Repository.Update(ctx, userID, id, func(current domain.APIKey) (domain.APIKey, *crypto.Sealed, error) {
		if current.Status == domain.StatusRevoked {
			return domain.APIKey{}, nil, domain.ErrRevoked
		}
		next := current
		if input.Label != nil {
			label, err := domain.NormalizeLabel(*input.Label)
			if err != nil {
				return domain.APIKey{}, nil, ErrInvalidInput
			}
			next.Label = label
		}
		if input.PerHourLimit != nil {
			if err := policy.PerHour.Enforce(*input.PerHourLimit); err != nil {
				return domain.APIKey{}, nil, err
			}
			next.PerHourLimit = *input.PerHourLimit
		}
		if input.PerSecondLimit != nil {
			if err := policy.PerSecond.Enforce(*input.PerSecondLimit); err != nil {
				return domain.APIKey{}, nil, err
			}
			next.PerSecondLimit = *input.PerSecondLimit
		}
		if input.Burst != nil {
			if err := policy.Burst.Enforce(*input.Burst); err != nil {
				return domain.APIKey{}, nil, err
			}
			next.Burst = *input.Burst
		}
		if input.Secret == nil {
			return next, nil, nil
		}
		secret, err := domain.NormalizeCredential(*input.Secret)
		if err != nil {
			return domain.APIKey{}, nil, ErrKeyInvalid
		}
		sealed, err := k.seal(secret, current.ID)
		if err != nil {
			return domain.APIKey{}, nil, err
		}
		return next, &sealed, nil
	})
}

// Delete removes the key. The repository publishes key.deleted in the same
// transaction so queued work for the key stops.
func (k Keys) Delete(ctx context.Context, userID, id int64) error {
	if err := k.ready(ctx); err != nil {
		return err
	}
	return k.Repository.Delete(ctx, userID, id)
}

// List returns the user's keys. Secrets are not part of the result.
func (k Keys) List(ctx context.Context, userID int64) ([]domain.APIKey, error) {
	if err := k.ready(ctx); err != nil {
		return nil, err
	}
	return k.Repository.List(ctx, userID)
}

// Get returns one key owned by the user. The secret is not part of the result.
func (k Keys) Get(ctx context.Context, userID, id int64) (domain.APIKey, error) {
	if err := k.ready(ctx); err != nil {
		return domain.APIKey{}, err
	}
	return k.Repository.Get(ctx, userID, id)
}

func (k Keys) ready(ctx context.Context) error {
	if k.Repository == nil || k.Probe == nil || k.Sealer == nil || k.Settings == nil {
		return errors.New("accounts: api key service is incomplete")
	}
	if ctx == nil {
		return errors.New("accounts: context is required")
	}
	return ctx.Err()
}

func (k Keys) seal(secret string, id int64) (crypto.Sealed, error) {
	plaintext := []byte(secret)
	sealed, err := k.Sealer.Seal(plaintext, domain.SecretAAD(id))
	wipeBytes(plaintext)
	if err != nil {
		return crypto.Sealed{}, err
	}
	return sealed, nil
}

func wipeBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
