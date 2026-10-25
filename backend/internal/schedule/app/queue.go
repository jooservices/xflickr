package app

import (
	"context"
	"math"
	"time"
)

const QueueStateInterval = 60 * time.Second

// LaneEstimate is the user's active backlog and estimated service capacity for one lane.
type LaneEstimate struct {
	Lane                  string
	BacklogCalls          int64
	EffectiveCallsPerHour float64
	ETAHours              *float64
}

// PlanLaneBacklog is an aggregate count of active work for a plan and lane.
type PlanLaneBacklog struct {
	Plan         string
	Lane         string
	BacklogCalls int64
}

// ReadyQueueSize is the number of dispatchable River jobs in a worker queue.
type ReadyQueueSize struct {
	Queue     string
	ReadyJobs int64
}

// AdminQueue contains queue state safe for administrator reporting.
type AdminQueue struct {
	BacklogByPlan        []PlanLaneBacklog
	OldestItemAgeSeconds int64
	ReadyQueueSizes      []ReadyQueueSize
}

// QueueSnapshotStore reads queue statistics from the durable scheduler stores.
type QueueSnapshotStore interface {
	UserQueue(context.Context, int64) ([]LaneEstimate, error)
	AdminQueue(context.Context) (AdminQueue, error)
}

// Queue exposes queue snapshots while applying the shared ETA calculation.
type Queue struct{ Store QueueSnapshotStore }

func (q Queue) ForUser(ctx context.Context, userID int64) ([]LaneEstimate, error) {
	lanes, err := q.Store.UserQueue(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range lanes {
		lanes[i].ETAHours = EstimateETA(lanes[i].BacklogCalls, lanes[i].EffectiveCallsPerHour)
	}
	return lanes, nil
}

func (q Queue) ForAdmin(ctx context.Context) (AdminQueue, error) {
	return q.Store.AdminQueue(ctx)
}

// EstimateETA returns backlog / effective calls per hour. A nil estimate means
// the lane has backlog but no usable configured capacity.
func EstimateETA(backlogCalls int64, effectiveCallsPerHour float64) *float64 {
	if backlogCalls < 0 || math.IsNaN(effectiveCallsPerHour) || math.IsInf(effectiveCallsPerHour, 0) || effectiveCallsPerHour < 0 {
		return nil
	}
	if backlogCalls == 0 {
		zero := float64(0)
		return &zero
	}
	if effectiveCallsPerHour == 0 {
		return nil
	}
	eta := float64(backlogCalls) / effectiveCallsPerHour
	return &eta
}
