package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type snapshotReader struct {
	snapshot Snapshot
	err      error
	userID   int64
	called   bool
}

func (r *snapshotReader) Load(_ context.Context, userID int64) (Snapshot, error) {
	r.called = true
	r.userID = userID
	return r.snapshot, r.err
}

func TestHomeServiceReturnsUserSnapshot(t *testing.T) {
	generated := time.Date(2026, time.November, 9, 10, 0, 0, 0, time.UTC)
	computed := generated.Add(-time.Minute)
	want := Snapshot{GeneratedAt: generated, CountsComputedAt: &computed, Estimated: true, Contacts: ContactCounts{Total: 3, Direct: 1, Discovered: 2}, NewPhotos: []Photo{}, StarredContacts: []Contact{}, Activity: []Activity{}}
	reader := &snapshotReader{snapshot: want}
	got, err := (Service{Reader: reader}).Get(t.Context(), 8)
	if err != nil || got.GeneratedAt != generated || got.CountsComputedAt == nil || *got.CountsComputedAt != computed || !got.Estimated || got.Contacts.Total != 3 || reader.userID != 8 || !reader.called {
		t.Fatalf("Get() = %+v, err = %v, reader = %+v", got, err, reader)
	}
}

type batchRefresherStub struct {
	userID int64
	limit  int
	steps  []bool
	err    error
	calls  int
}

func (r *batchRefresherStub) RefreshBatch(_ context.Context, userID int64, limit int) (bool, error) {
	r.userID, r.limit = userID, limit
	r.calls++
	if r.err != nil {
		return false, r.err
	}
	done := r.steps[0]
	r.steps = r.steps[1:]
	return done, nil
}

func TestHomeRefresherProcessesOneBoundedBatchPerJob(t *testing.T) {
	processor := &batchRefresherStub{steps: []bool{false, false, true}}
	refresher := Refresher{Processor: processor, BatchSize: RefreshBatchSize}
	for index, wantDone := range []bool{false, false, true} {
		done, err := refresher.Refresh(t.Context(), 42)
		if err != nil || done != wantDone || processor.calls != index+1 {
			t.Fatalf("Refresh() = %t, %v after %d calls", done, err, processor.calls)
		}
	}
	if processor.userID != 42 || processor.limit != RefreshBatchSize || len(processor.steps) != 0 {
		t.Fatalf("refresher did not preserve bounded job steps: %+v", processor)
	}
}

func TestHomeRefresherReturnsBatchError(t *testing.T) {
	want := errors.New("batch failed")
	processor := &batchRefresherStub{err: want}
	if _, err := (Refresher{Processor: processor, BatchSize: RefreshBatchSize}).Refresh(t.Context(), 42); !errors.Is(err, want) {
		t.Fatalf("Refresh() error = %v, want wrapped batch error", err)
	}
}

func TestHomeRefresherRejectsInvalidRequest(t *testing.T) {
	for _, test := range []struct {
		name      string
		ctx       context.Context
		userID    int64
		batchSize int
	}{
		{name: "nil context", userID: 1, batchSize: 250},
		{name: "invalid user", ctx: t.Context(), batchSize: 250},
		{name: "missing batch size", ctx: t.Context(), userID: 1},
		{name: "batch exceeds cap", ctx: t.Context(), userID: 1, batchSize: RefreshBatchSize + 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			processor := &batchRefresherStub{steps: []bool{true}}
			_, err := (Refresher{Processor: processor, BatchSize: test.batchSize}).Refresh(test.ctx, test.userID)
			if err == nil || len(processor.steps) != 1 {
				t.Fatalf("Refresh() error = %v, steps = %d", err, len(processor.steps))
			}
		})
	}
}

func TestHomeRefresherRequiresProcessor(t *testing.T) {
	if _, err := (Refresher{BatchSize: RefreshBatchSize}).Refresh(t.Context(), 42); !errors.Is(err, ErrRefresherRequired) {
		t.Fatalf("Refresh() error = %v, want ErrRefresherRequired", err)
	}
}

func TestHomeRefresherHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	processor := &batchRefresherStub{steps: []bool{true}}
	if _, err := (Refresher{Processor: processor, BatchSize: RefreshBatchSize}).Refresh(ctx, 42); !errors.Is(err, context.Canceled) || len(processor.steps) != 1 {
		t.Fatalf("Refresh() error = %v, remaining batches = %d", err, len(processor.steps))
	}
}

func TestParseRefreshJobValidatesUser(t *testing.T) {
	for _, test := range []struct {
		name string
		args json.RawMessage
		want int64
	}{
		{name: "valid", args: json.RawMessage(`{"user_id":17}`), want: 17},
		{name: "invalid JSON", args: json.RawMessage(`{`)},
		{name: "missing user", args: json.RawMessage(`{"user_id":0}`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			job, err := ParseRefreshJob(test.args)
			if test.want > 0 {
				if err != nil || job.UserID != test.want {
					t.Fatalf("ParseRefreshJob() = %+v, %v", job, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ParseRefreshJob(%s) succeeded", test.args)
			}
		})
	}
}

func TestHomeServiceRejectsInvalidUserBeforeReading(t *testing.T) {
	reader := &snapshotReader{}
	_, err := (Service{Reader: reader}).Get(t.Context(), 0)
	if !errors.Is(err, ErrInvalidUser) || reader.called {
		t.Fatalf("Get() err = %v, reader called = %t", err, reader.called)
	}
}

func TestHomeServiceRejectsNilContextBeforeReading(t *testing.T) {
	reader := &snapshotReader{}
	_, err := (Service{Reader: reader}).Get(nil, 8)
	if !errors.Is(err, ErrInvalidContext) || reader.called {
		t.Fatalf("Get() err = %v, reader called = %t", err, reader.called)
	}
}

func TestHomeServiceRequiresReader(t *testing.T) {
	_, err := (Service{}).Get(t.Context(), 8)
	if !errors.Is(err, ErrReaderRequired) {
		t.Fatalf("Get() err = %v, want ErrReaderRequired", err)
	}
}

func TestHomeServiceWrapsReaderErrors(t *testing.T) {
	want := errors.New("database unavailable")
	_, err := (Service{Reader: &snapshotReader{err: want}}).Get(t.Context(), 8)
	if !errors.Is(err, want) {
		t.Fatalf("Get() err = %v, want wrapped reader error", err)
	}
}
