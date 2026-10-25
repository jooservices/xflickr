package app

import (
	"context"
	"errors"
	"sort"
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

func TestSplitShareLargestRemainder(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
		groups   []ShareGroup
		want     map[int64]int
	}{
		{"one unit follows largest fractional share", 1, []ShareGroup{{ID: 1, Share: .2, Available: 10}, {ID: 2, Share: .8, Available: 10}}, map[int64]int{1: 0, 2: 1}},
		{"two units preserve the weighted split", 2, []ShareGroup{{ID: 1, Share: .2, Available: 10}, {ID: 2, Share: .8, Available: 10}}, map[int64]int{1: 0, 2: 2}},
		{"three groups use remainders", 2, []ShareGroup{{ID: 1, Share: .5, Available: 10}, {ID: 2, Share: .3, Available: 10}, {ID: 3, Share: .2, Available: 10}}, map[int64]int{1: 1, 2: 1, 3: 0}},
		{"equal remainder tie prefers larger share", 1, []ShareGroup{{ID: 2, Share: .5, Available: 10}, {ID: 1, Share: .5, Available: 10}}, map[int64]int{1: 1, 2: 0}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := SplitShare(test.capacity, test.groups)
			if err != nil {
				t.Fatal(err)
			}
			for id, want := range test.want {
				if got[id] != want {
					t.Errorf("allocation[%d] = %d, want %d (all: %v)", id, got[id], want, got)
				}
			}
		})
	}
}

func TestSplitShareRotatesEqualRemainderTies(t *testing.T) {
	groups := []ShareGroup{{ID: 1, Share: .5, Available: 10}, {ID: 2, Share: .5, Available: 10}}
	progress := map[int64]int{}
	for rotation := 0; rotation < 2; rotation++ {
		allocation, err := SplitShare(1, groups, rotation)
		if err != nil {
			t.Fatal(err)
		}
		for id, count := range allocation {
			progress[id] += count
		}
	}
	if progress[1] != 1 || progress[2] != 1 {
		t.Fatalf("progress after rotating one-unit allocations = %v, want both groups served", progress)
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
	type item struct {
		id       int
		enqueued int
	}
	pro, free := make([]item, 100), make([]item, 100)
	for i := 0; i < 100; i++ {
		pro[i] = item{id: i}
		free[i] = item{id: i}
	}
	weights := domain.ScoreWeights{PlanWeight: 10, AgeMinutes: .1}
	maxFreeWait := 0
	proFinishedAt, freeFinishedAt := 0, 0
	for minute := 1; minute <= 50 && (len(pro) > 0 || len(free) > 0); minute++ {
		allocations, err := SplitShare(10, []ShareGroup{{ID: 1, Share: .2, Available: len(free)}, {ID: 2, Share: .8, Available: len(pro)}})
		if err != nil {
			t.Fatal(err)
		}
		for _, plan := range []struct {
			id      int64
			weight  int
			backlog *[]item
		}{{1, 1, &free}, {2, 4, &pro}} {
			backlog := *plan.backlog
			sort.Slice(backlog, func(i, j int) bool {
				scoreI, err := domain.Score(domain.ScoreInput{PlanWeight: plan.weight, MaxDepth: 4, AgeMinutes: float64(minute - backlog[i].enqueued)}, weights)
				if err != nil {
					t.Fatal(err)
				}
				scoreJ, err := domain.Score(domain.ScoreInput{PlanWeight: plan.weight, MaxDepth: 4, AgeMinutes: float64(minute - backlog[j].enqueued)}, weights)
				if err != nil {
					t.Fatal(err)
				}
				if scoreI != scoreJ {
					return scoreI > scoreJ
				}
				return backlog[i].id < backlog[j].id
			})
			count := min(allocations[plan.id], len(backlog))
			if plan.id == 1 && count > 0 {
				wait := minute - backlog[0].enqueued
				if wait > maxFreeWait {
					maxFreeWait = wait
				}
			}
			*plan.backlog = backlog[count:]
			if len(*plan.backlog) == 0 {
				if plan.id == 1 && freeFinishedAt == 0 {
					freeFinishedAt = minute
				}
				if plan.id == 2 && proFinishedAt == 0 {
					proFinishedAt = minute
				}
			}
		}
	}
	if len(pro) != 0 || len(free) != 0 {
		t.Fatalf("simulation left backlog: pro=%d free=%d", len(pro), len(free))
	}
	if maxFreeWait > 50 {
		t.Fatalf("oldest free work waited %d minutes, limit is 50", maxFreeWait)
	}
	if freeFinishedAt == 0 || freeFinishedAt > 50 || proFinishedAt == 0 {
		t.Fatalf("completion times: free=%d pro=%d, expected free by minute 50 while pro backlog is finite", freeFinishedAt, proFinishedAt)
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
