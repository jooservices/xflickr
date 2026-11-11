package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jooservices/xflickr/internal/platform/crypto"
)

var (
	// ErrTargetNotFound means the target does not exist for the requesting user.
	ErrTargetNotFound = errors.New("sync: target not found")
	// ErrInvalidTarget means the target metadata or token is invalid.
	ErrInvalidTarget = errors.New("sync: invalid target")
	// ErrInvalidTargetPage means target listing received an invalid owner or page.
	ErrInvalidTargetPage = errors.New("sync: invalid target page")
)

const maxTargetPageSize = 100

// Target is the user-owned metadata for one connected storage destination.
// It never includes the destination token.
type Target struct {
	ID       int64
	UserID   int64
	Provider string
	Label    string
	Status   string
	Config   json.RawMessage
}

// TargetRepository stores targets and accepts a sealer that receives the
// database-assigned target ID before inserting the row.
type TargetRepository interface {
	Create(context.Context, Target, func(int64) (crypto.Sealed, error)) (Target, error)
	Get(context.Context, int64, int64) (Target, error)
	List(context.Context, int64, int64, int) ([]Target, error)
}

// TargetSealer encrypts a destination token using associated data.
type TargetSealer interface {
	Seal([]byte, []byte) (crypto.Sealed, error)
}

// Targets implements the sync target use cases.
type Targets struct {
	Repository TargetRepository
	Sealer     TargetSealer
}

// Create validates and stores a target with a token sealed for its assigned ID.
func (s Targets) Create(ctx context.Context, target Target, token []byte) (Target, error) {
	if err := s.ready(ctx); err != nil {
		return Target{}, err
	}
	if s.Sealer == nil {
		return Target{}, errors.New("sync: target sealer is required")
	}
	if target.UserID <= 0 || len(token) == 0 {
		return Target{}, ErrInvalidTarget
	}
	target.Provider = strings.TrimSpace(target.Provider)
	target.Label = strings.TrimSpace(target.Label)
	if target.Provider == "" || target.Label == "" || !isJSONObject(target.Config) {
		return Target{}, ErrInvalidTarget
	}
	target.Config = bytes.Clone(target.Config)
	target.Status = "connected"

	created, err := s.Repository.Create(ctx, target, func(id int64) (crypto.Sealed, error) {
		plaintext := bytes.Clone(token)
		sealed, err := s.Sealer.Seal(plaintext, TargetTokenAAD(id))
		wipeTargetToken(plaintext)
		if err != nil {
			return crypto.Sealed{}, targetSealError{cause: err}
		}
		return sealed, nil
	})
	if err != nil {
		return Target{}, fmt.Errorf("sync: create target: %w", err)
	}
	return created, nil
}

// Get returns metadata for one target owned by userID.
func (s Targets) Get(ctx context.Context, userID, id int64) (Target, error) {
	if err := s.ready(ctx); err != nil {
		return Target{}, err
	}
	target, err := s.Repository.Get(ctx, userID, id)
	if err != nil {
		return Target{}, fmt.Errorf("sync: get target: %w", err)
	}
	return target, nil
}

// List returns one bounded page of targets owned by userID after afterID.
func (s Targets) List(ctx context.Context, userID, afterID int64, limit int) ([]Target, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	if userID <= 0 || afterID < 0 || limit <= 0 || limit > maxTargetPageSize {
		return nil, ErrInvalidTargetPage
	}
	targets, err := s.Repository.List(ctx, userID, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("sync: list targets: %w", err)
	}
	return targets, nil
}

// TargetTokenAAD binds a token envelope to the target row that owns it.
func TargetTokenAAD(id int64) []byte {
	return []byte("sync_targets:" + strconv.FormatInt(id, 10) + ":token")
}

func (s Targets) ready(ctx context.Context) error {
	if s.Repository == nil {
		return errors.New("sync: target service is incomplete")
	}
	if ctx == nil {
		return errors.New("sync: context is required")
	}
	return ctx.Err()
}

func isJSONObject(value json.RawMessage) bool {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(value, &object); err != nil {
		return false
	}
	return object != nil
}

func wipeTargetToken(token []byte) {
	for index := range token {
		token[index] = 0
	}
}

type targetSealError struct{ cause error }

func (err targetSealError) Error() string { return "sync: seal target token failed" }

func (err targetSealError) Unwrap() error { return err.cause }
