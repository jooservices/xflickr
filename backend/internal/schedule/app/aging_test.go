package app

import (
	"context"
	"errors"
	"testing"

	"github.com/jooservices/xflickr/internal/schedule/domain"
)

func TestWeightsFingerprintTriggersOneSetBasedRescore(t *testing.T) {
	store := &rescoreFake{fingerprint: "stale", computed: "current"}
	job := WeightsRescore{Store: store, BatchSize: 25, MaxDepth: 4, Weights: domain.ScoreWeights{AgeMinutes: .1}}

	got, err := job.RunBatch(context.Background())
	if err != nil || got != 25 || store.batch != 25 || store.calls != 1 {
		t.Fatalf("first RunBatch() = %d, %v store=%+v", got, err, store)
	}
	if store.fingerprint == store.computed {
		t.Fatal("fingerprint advanced before the full pending pass finished")
	}

	store.batchResult = 10
	got, err = job.RunBatch(context.Background())
	if err != nil || got != 10 || store.calls != 2 {
		t.Fatalf("final RunBatch() = %d, %v store=%+v", got, err, store)
	}
	if store.fingerprint != store.computed {
		t.Fatalf("fingerprint = %q, want %q", store.fingerprint, store.computed)
	}

	got, err = job.RunBatch(context.Background())
	if err != nil || got != 0 || store.calls != 2 {
		t.Fatalf("matching fingerprint still rescored: %d, %v calls=%d", got, err, store.calls)
	}
}

func TestWeightsRescoreValidatesDependencies(t *testing.T) {
	store := &rescoreFake{computed: "x"}
	if _, err := (WeightsRescore{}).RunBatch(context.Background()); err == nil {
		t.Fatal("accepted invalid dependencies")
	}
	if _, err := (WeightsRescore{Store: store, BatchSize: 1}).RunBatch(nil); err == nil {
		t.Fatal("accepted nil context")
	}
	store.computeErr = errors.New("compute failed")
	job := WeightsRescore{Store: store, BatchSize: 1, Weights: domain.ScoreWeights{}}
	if _, err := job.RunBatch(context.Background()); err == nil {
		t.Fatal("ignored fingerprint compute failure")
	}
}

func TestWeightsRescorePropagatesStoreErrors(t *testing.T) {
	store := &rescoreFake{fingerprint: "stale", computed: "current", loadErr: errors.New("load failed")}
	job := WeightsRescore{Store: store, BatchSize: 5, Weights: domain.ScoreWeights{}}
	if _, err := job.RunBatch(context.Background()); err == nil {
		t.Fatal("ignored fingerprint load failure")
	}

	store.loadErr = nil
	store.rescoreErr = errors.New("rescore failed")
	if _, err := job.RunBatch(context.Background()); err == nil {
		t.Fatal("ignored pending rescore failure")
	}

	store.rescoreErr = nil
	store.batchResult = 1
	store.storeErr = errors.New("store failed")
	got, err := job.RunBatch(context.Background())
	if err == nil || got != 1 {
		t.Fatalf("store fingerprint failure = %d, %v", got, err)
	}
}

type rescoreFake struct {
	fingerprint string
	computed    string
	batch       int
	batchResult int
	calls       int
	computeErr  error
	loadErr     error
	storeErr    error
	rescoreErr  error
}

func (s *rescoreFake) RescorePendingBatch(_ context.Context, batch int, _ domain.ScoreWeights, _ int) (int, error) {
	s.calls++
	s.batch = batch
	if s.rescoreErr != nil {
		return 0, s.rescoreErr
	}
	if s.batchResult != 0 {
		return s.batchResult, nil
	}
	return batch, nil
}

func (s *rescoreFake) ComputeWeightsFingerprint(context.Context, domain.ScoreWeights, int) (string, error) {
	return s.computed, s.computeErr
}

func (s *rescoreFake) LoadWeightsFingerprint(context.Context) (string, error) {
	return s.fingerprint, s.loadErr
}

func (s *rescoreFake) StoreWeightsFingerprint(_ context.Context, fp string) error {
	s.fingerprint = fp
	return s.storeErr
}
