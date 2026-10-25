package app

import (
	"context"
	"errors"
	"math"
	"testing"
)

type queueStoreStub struct {
	lanes []LaneEstimate
	admin AdminQueue
	err   error
}

func (s queueStoreStub) UserQueue(context.Context, int64) ([]LaneEstimate, error) {
	return s.lanes, s.err
}

func (s queueStoreStub) AdminQueue(context.Context) (AdminQueue, error) { return s.admin, s.err }

func TestEstimateETA(t *testing.T) {
	tests := []struct {
		name    string
		backlog int64
		rate    float64
		want    float64
		wantNil bool
	}{
		{name: "fractional hours", backlog: 120, rate: 60, want: 2},
		{name: "empty backlog", backlog: 0, rate: 0, want: 0},
		{name: "no capacity", backlog: 2, wantNil: true},
		{name: "negative backlog", backlog: -1, rate: 1, wantNil: true},
		{name: "negative rate", backlog: 1, rate: -1, wantNil: true},
		{name: "infinite rate", backlog: 1, rate: math.Inf(1), wantNil: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateETA(tt.backlog, tt.rate)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("EstimateETA() = %v, want nil", *got)
				}
				return
			}
			if got == nil || math.Abs(*got-tt.want) > 1e-9 {
				t.Fatalf("EstimateETA() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestQueueForUserAppliesETA(t *testing.T) {
	q := Queue{Store: queueStoreStub{lanes: []LaneEstimate{{Lane: "person", BacklogCalls: 12, EffectiveCallsPerHour: 3}, {Lane: "sizes", BacklogCalls: 4}}}}
	got, err := q.ForUser(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ETAHours == nil || *got[0].ETAHours != 4 || got[1].ETAHours != nil {
		t.Fatalf("ForUser() = %+v", got)
	}
}

func TestQueuePropagatesStoreErrors(t *testing.T) {
	wantErr := errors.New("store unavailable")
	q := Queue{Store: queueStoreStub{err: wantErr}}
	if _, err := q.ForUser(context.Background(), 1); !errors.Is(err, wantErr) {
		t.Fatalf("ForUser() error = %v", err)
	}
	if _, err := q.ForAdmin(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("ForAdmin() error = %v", err)
	}
}
