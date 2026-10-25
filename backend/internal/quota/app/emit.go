package app

import (
	"context"
	"errors"
	"time"
)

// StateEventName is the event emitted for every active key.
const StateEventName = "quota.state"

// DefaultEmitInterval is how often quota.state events are emitted.
const DefaultEmitInterval = time.Minute

// Event is one structured event sent to the observability backend.
type Event map[string]any

// EventSink delivers events in one batch.
type EventSink interface {
	Send(context.Context, []Event) error
}

// StateReader lists the state of every active key.
type StateReader interface {
	All(context.Context) ([]KeyState, error)
}

// Emitter sends a quota.state event per active key every interval.
type Emitter struct {
	States   StateReader
	Sink     EventSink
	Node     string
	Interval time.Duration
	Now      func() time.Time
}

// EmitOnce reads every active key's state and sends one event per key.
func (e Emitter) EmitOnce(ctx context.Context) error {
	if ctx == nil || e.States == nil || e.Sink == nil {
		return errors.New("quota: emitter is not configured")
	}
	states, err := e.States.All(ctx)
	if err != nil {
		return err
	}
	if len(states) == 0 {
		return nil
	}
	now := time.Now()
	if e.Now != nil {
		now = e.Now()
	}
	events := make([]Event, 0, len(states))
	for _, state := range states {
		events = append(events, e.event(state, now))
	}
	return e.Sink.Send(ctx, events)
}

func (e Emitter) event(state KeyState, now time.Time) Event {
	event := Event{
		"_timestamp":       now.UnixMicro(),
		"event":            StateEventName,
		"node":             e.Node,
		"key_id":           state.Key.ID,
		"used":             state.Used,
		"limit":            state.Limit,
		"reserve":          state.Reserve,
		"state":            string(state.State),
		"trend":            string(state.Trend),
		"lane_interactive": state.Lanes.Interactive,
		"lane_person":      state.Lanes.Person,
		"lane_photo":       state.Lanes.Photo,
	}
	if !state.ResetAt.IsZero() {
		event["reset_at"] = state.ResetAt.UTC().Format(time.RFC3339)
	}
	return event
}

// Run emits every interval until ctx ends. A failed emit is passed to onError
// (when set) and the loop continues.
func (e Emitter) Run(ctx context.Context, onError func(error)) {
	interval := e.Interval
	if interval <= 0 {
		interval = DefaultEmitInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := e.EmitOnce(ctx); err != nil && onError != nil {
				onError(err)
			}
		}
	}
}
