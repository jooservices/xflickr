package app

import (
	"context"
	"errors"

	"github.com/jooservices/xflickr/internal/schedule/domain"
)

// WeightsRescoreStore runs one bounded, set-based pending rescore and tracks the
// weights fingerprint that last completed a full pass.
type WeightsRescoreStore interface {
	RescorePendingBatch(context.Context, int, domain.ScoreWeights, int) (int, error)
	RescoreExpiredSelectionsBatch(context.Context, int, domain.ScoreWeights, int) (int, error)
	ComputeWeightsFingerprint(context.Context, domain.ScoreWeights, int) (string, error)
	LoadWeightsFingerprint(context.Context) (string, error)
	StoreWeightsFingerprint(context.Context, string) error
}

// WeightsRescore refreshes pending scores when score inputs change and removes
// expired selection bonuses in bounded batches. It replaces the former per-tick
// aging rewrite.
type WeightsRescore struct {
	Store     WeightsRescoreStore
	BatchSize int
	MaxDepth  int
	Weights   domain.ScoreWeights
}

// RunBatch rescores one bounded set-based batch when the stored fingerprint does
// not match the current weights. When a batch returns fewer than BatchSize rows
// the fingerprint is advanced so the next tick becomes a no-op.
func (a WeightsRescore) RunBatch(ctx context.Context) (int, error) {
	if ctx == nil || a.Store == nil || a.BatchSize <= 0 || a.MaxDepth < 0 {
		return 0, errors.New("schedule: invalid weights rescore configuration")
	}
	want, err := a.Store.ComputeWeightsFingerprint(ctx, a.Weights, a.MaxDepth)
	if err != nil {
		return 0, err
	}
	have, err := a.Store.LoadWeightsFingerprint(ctx)
	if err != nil {
		return 0, err
	}
	if have == want {
		return a.Store.RescoreExpiredSelectionsBatch(ctx, a.BatchSize, a.Weights, a.MaxDepth)
	}
	count, err := a.Store.RescorePendingBatch(ctx, a.BatchSize, a.Weights, a.MaxDepth)
	if err != nil {
		return 0, err
	}
	if count < a.BatchSize {
		if err := a.Store.StoreWeightsFingerprint(ctx, want); err != nil {
			return count, err
		}
	}
	return count, nil
}
