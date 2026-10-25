package app

import (
	"context"
	"errors"

	"github.com/jooservices/xflickr/internal/schedule/domain"
)

// AgingStore refreshes a bounded batch of stored priority scores.
type AgingStore interface {
	RecomputeScores(context.Context, int, domain.ScoreWeights, int) (int, error)
}

// Aging runs one bounded score refresh. The scheduler invokes it each pump tick.
type Aging struct {
	Store     AgingStore
	BatchSize int
	MaxDepth  int
	Weights   domain.ScoreWeights
}

func (a Aging) RunBatch(ctx context.Context) (int, error) {
	if ctx == nil || a.Store == nil || a.BatchSize <= 0 || a.MaxDepth < 0 {
		return 0, errors.New("schedule: invalid aging configuration")
	}
	return a.Store.RecomputeScores(ctx, a.BatchSize, a.Weights, a.MaxDepth)
}
