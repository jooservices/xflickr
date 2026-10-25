package app

import (
	"context"
	"errors"
	"testing"

	"github.com/jooservices/xflickr/internal/schedule/domain"
)

func TestSplitShareProportionsAndLendsUnusedCapacity(t *testing.T) {
	got, err := SplitShare(100, []ShareGroup{{ID: 1, Share: .2, Available: 100}, {ID: 2, Share: .8, Available: 100}})
	if err != nil || got[1] != 20 || got[2] != 80 {
		t.Fatalf("SplitShare() = %v, %v", got, err)
	}
	got, err = SplitShare(100, []ShareGroup{{ID: 1, Share: .2, Available: 0}, {ID: 2, Share: .8, Available: 100}})
	if err != nil || got[1] != 0 || got[2] != 100 {
		t.Fatalf("SplitShare() lending = %v, %v", got, err)
	}
}

func TestSplitShareValidation(t *testing.T) {
	for _, input := range []struct {
		capacity int
		groups   []ShareGroup
	}{{-1, nil}, {1, []ShareGroup{{ID: 1, Share: 0, Available: 1}}}, {1, []ShareGroup{{ID: 1, Share: -1, Available: 1}}}, {1, []ShareGroup{{ID: 1, Share: 1, Available: -1}}}} {
		if _, err := SplitShare(input.capacity, input.groups); err == nil {
			t.Fatalf("SplitShare(%+v) accepted", input)
		}
	}
	got, err := SplitShare(5, nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("SplitShare empty = %v, %v", got, err)
	}
}

func TestPlanShareSimulationFreeBacklogProgresses(t *testing.T) {
	pro, free := 100, 100
	oldestFreeWait := 0
	for minute := 1; minute <= 50 && (pro > 0 || free > 0); minute++ {
		allocations, err := SplitShare(10, []ShareGroup{{ID: 1, Share: .2, Available: free}, {ID: 2, Share: .8, Available: pro}})
		if err != nil {
			t.Fatal(err)
		}
		free -= allocations[1]
		pro -= allocations[2]
		if free > 0 && minute > oldestFreeWait {
			oldestFreeWait = minute
		}
	}
	if pro != 0 || free != 0 {
		t.Fatalf("simulation left backlog: pro=%d free=%d", pro, free)
	}
	if oldestFreeWait > 50 {
		t.Fatalf("oldest free work waited %d minutes, limit is 50", oldestFreeWait)
	}
}

func TestAgingRunsOneBatch(t *testing.T) {
	store := &agingFake{}
	aging := Aging{Store: store, BatchSize: 25, MaxDepth: 4, Weights: domain.ScoreWeights{AgeMinutes: .1}}
	got, err := aging.RunBatch(context.Background())
	if err != nil || got != 3 || store.batch != 25 || store.maxDepth != 4 {
		t.Fatalf("RunBatch() = %d, %v store=%+v", got, err, store)
	}
	if _, err := (Aging{}).RunBatch(context.Background()); err == nil {
		t.Fatal("aging accepted invalid dependencies")
	}
	if _, err := (Aging{Store: store, BatchSize: 1}).RunBatch(nil); err == nil {
		t.Fatal("aging accepted nil context")
	}
	store.err = errors.New("store failure")
	if _, err := aging.RunBatch(context.Background()); err == nil {
		t.Fatal("aging ignored store failure")
	}
}

type agingFake struct {
	batch, maxDepth int
	err             error
}

func (s *agingFake) RecomputeScores(_ context.Context, batch int, _ domain.ScoreWeights, depth int) (int, error) {
	s.batch, s.maxDepth = batch, depth
	return 3, s.err
}
