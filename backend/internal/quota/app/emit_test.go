package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/quota/domain"
)

type fakeStateReader struct {
	states []KeyState
	err    error
}

func (f fakeStateReader) All(context.Context) ([]KeyState, error) { return f.states, f.err }

type fakeSink struct {
	batches chan []Event
	err     error
}

func (f *fakeSink) Send(_ context.Context, events []Event) error {
	f.batches <- events
	return f.err
}

func TestEmitOnceSendsOneEventPerKey(t *testing.T) {
	reset := time.Date(2026, 10, 25, 13, 0, 0, 0, time.UTC)
	sink := &fakeSink{batches: make(chan []Event, 1)}
	emitter := Emitter{
		States: fakeStateReader{states: []KeyState{
			{Key: KeyInfo{ID: 1}, Used: 4, Limit: 10, Reserve: 2, ResetAt: reset, State: domain.StateOK, Trend: domain.TrendRising, Lanes: LaneUsage{Interactive: 1, Person: 2, Photo: 1}},
			{Key: KeyInfo{ID: 2}, Limit: 10, State: domain.StateOK, Trend: domain.TrendSteady},
		}},
		Sink: sink, Node: "node-a", Now: func() time.Time { return stateNow },
	}
	if err := emitter.EmitOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := <-sink.batches
	if len(events) != 2 {
		t.Fatalf("events = %v", events)
	}
	first := events[0]
	for key, want := range map[string]any{
		"event": "quota.state", "node": "node-a", "key_id": int64(1), "used": int64(4), "limit": int64(10),
		"reserve": int64(2), "state": "ok", "trend": "rising", "lane_interactive": int64(1), "lane_person": int64(2),
		"lane_photo": int64(1), "reset_at": "2026-10-25T13:00:00Z", "_timestamp": stateNow.UnixMicro(),
	} {
		if first[key] != want {
			t.Errorf("%s = %v (%T), want %v", key, first[key], first[key], want)
		}
	}
	if _, ok := events[1]["reset_at"]; ok {
		t.Fatal("idle key reported a reset time")
	}
}

func TestEmitOnceErrorsAndEmptyBatch(t *testing.T) {
	sink := &fakeSink{batches: make(chan []Event, 1)}
	if err := (Emitter{States: fakeStateReader{}, Sink: sink}).EmitOnce(context.Background()); err != nil || len(sink.batches) != 0 {
		t.Fatalf("empty batch = %v, sent %d", err, len(sink.batches))
	}
	boom := errors.New("boom")
	if err := (Emitter{States: fakeStateReader{err: boom}, Sink: sink}).EmitOnce(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("state error = %v", err)
	}
	sink.err = boom
	if err := (Emitter{States: fakeStateReader{states: []KeyState{{}}}, Sink: sink}).EmitOnce(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("sink error = %v", err)
	}
	if err := (Emitter{}).EmitOnce(context.Background()); err == nil {
		t.Fatal("unconfigured emitter accepted")
	}
}

func TestEmitterRunEmitsEveryIntervalAndReportsErrors(t *testing.T) {
	sink := &fakeSink{batches: make(chan []Event, 16), err: errors.New("down")}
	emitter := Emitter{States: fakeStateReader{states: []KeyState{{Key: KeyInfo{ID: 1}}}}, Sink: sink, Interval: 5 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 16)
	done := make(chan struct{})
	go func() {
		emitter.Run(ctx, func(err error) { errs <- err })
		close(done)
	}()
	for _, ch := range []<-chan time.Time{time.After(2 * time.Second)} {
		select {
		case <-sink.batches:
		case <-ch:
			t.Fatal("no event emitted")
		}
		select {
		case <-errs:
		case <-ch:
			t.Fatal("emit error not reported")
		}
	}
	cancel()
	<-done

	// Defaults to a one-minute interval and tolerates a missing error hook.
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	Emitter{}.Run(ctx, nil)
}
