package app

import (
	"context"
	"errors"
	"testing"
)

type fakeDepthStore struct {
	policy DepthPolicy
	err    error
	setErr error
	set    int
}

func (s *fakeDepthStore) Policy(context.Context, int64) (DepthPolicy, error) {
	return s.policy, s.err
}

func (s *fakeDepthStore) SetDepth(_ context.Context, _ int64, depth int) error {
	s.set = depth
	if s.setErr == nil {
		s.policy.Depth = depth
	}
	return s.setErr
}

func TestDepthSetStoresWithinTheCap(t *testing.T) {
	store := &fakeDepthStore{policy: DepthPolicy{Depth: 2, Default: 2, Max: 4}}
	got, err := (Depth{Store: store}).Set(t.Context(), 7, 3)
	if err != nil || got.Depth != 3 || store.set != 3 {
		t.Fatalf("Set() = %+v, %v (stored %d)", got, err, store.set)
	}
}

func TestDepthSetRejectsInvalidAndCappedValues(t *testing.T) {
	store := &fakeDepthStore{policy: DepthPolicy{Depth: 2, Default: 2, Max: 4}}
	service := Depth{Store: store}
	if _, err := service.Set(t.Context(), 7, 0); !errors.Is(err, ErrDepthInvalid) {
		t.Fatalf("zero depth error = %v", err)
	}
	if got, err := service.Set(t.Context(), 7, 5); !errors.Is(err, ErrDepthAboveCap) || got.Max != 4 || store.set != 0 {
		t.Fatalf("capped depth = %+v, %v", got, err)
	}
	store.policy.Max = 0
	if _, err := service.Set(t.Context(), 7, 9); err != nil {
		t.Fatalf("uncapped depth error = %v", err)
	}
}

func TestDepthPropagatesStoreFailures(t *testing.T) {
	if _, err := (Depth{}).Get(t.Context(), 1); !errors.Is(err, ErrDepthStoreRequired) {
		t.Fatalf("missing store error = %v", err)
	}
	boom := errors.New("down")
	if _, err := (Depth{Store: &fakeDepthStore{err: boom}}).Set(t.Context(), 1, 2); !errors.Is(err, boom) {
		t.Fatalf("policy error = %v", err)
	}
	store := &fakeDepthStore{policy: DepthPolicy{Max: 4}, setErr: ErrDepthAboveCap}
	if _, err := (Depth{Store: store}).Set(t.Context(), 1, 2); !errors.Is(err, ErrDepthAboveCap) {
		t.Fatalf("store error = %v", err)
	}
}
