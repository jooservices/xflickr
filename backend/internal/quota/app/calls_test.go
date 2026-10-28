package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type callBatchWriterFunc func(context.Context, []CallRecord) error

func (f callBatchWriterFunc) InsertCalls(ctx context.Context, records []CallRecord) error {
	return f(ctx, records)
}

func TestCallRecorderRejectsOverflowWithoutBlocking(t *testing.T) {
	var dropped uint64
	recorder := NewCallRecorder(callBatchWriterFunc(func(context.Context, []CallRecord) error { return nil }), 1, 1, time.Hour, func() { dropped++ })
	record := CallRecord{KeyID: 1, Method: "flickr.test", Status: "ok"}
	if !recorder.Record(record) {
		t.Fatal("first record was rejected")
	}
	if recorder.Record(record) {
		t.Fatal("record was accepted into a full queue")
	}
	if recorder.Dropped() != 1 || dropped != 1 {
		t.Fatalf("dropped count = %d, metric callback count = %d", recorder.Dropped(), dropped)
	}
}

func TestCallRecorderBatchAndShutdownFlush(t *testing.T) {
	written := make(chan []CallRecord, 2)
	writer := callBatchWriterFunc(func(_ context.Context, records []CallRecord) error {
		written <- append([]CallRecord(nil), records...)
		return nil
	})
	recorder := NewCallRecorder(writer, 8, 2, time.Hour, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		recorder.Run(ctx, nil)
	}()

	for i := int64(1); i <= 2; i++ {
		if !recorder.Record(CallRecord{KeyID: 1, Method: "flickr.test", Status: "ok", Bytes: i}) {
			t.Fatal("record was rejected")
		}
	}
	select {
	case batch := <-written:
		if len(batch) != 2 || batch[0].Bytes != 1 || batch[1].Bytes != 2 {
			t.Fatalf("batch = %+v", batch)
		}
	case <-time.After(time.Second):
		t.Fatal("full batch was not written")
	}

	if !recorder.Record(CallRecord{KeyID: 1, Method: "flickr.test", Status: "fail"}) {
		t.Fatal("shutdown record was rejected")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("recorder did not stop")
	}
	select {
	case batch := <-written:
		if len(batch) != 1 || batch[0].Status != "fail" {
			t.Fatalf("shutdown batch = %+v", batch)
		}
	case <-time.After(time.Second):
		t.Fatal("queued record was not flushed on shutdown")
	}
	if recorder.Record(CallRecord{KeyID: 1, Method: "flickr.test", Status: "ok"}) || recorder.Dropped() != 1 {
		t.Fatalf("record accepted after shutdown; dropped = %d", recorder.Dropped())
	}
}

func TestCallRecorderReportsWriterErrors(t *testing.T) {
	boom := errors.New("database unavailable")
	gotError := make(chan error, 1)
	recorder := NewCallRecorder(callBatchWriterFunc(func(context.Context, []CallRecord) error { return boom }), 2, 1, time.Hour, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		recorder.Run(ctx, func(err error) { gotError <- err })
	}()
	if !recorder.Record(CallRecord{KeyID: 1, Method: "flickr.test", Status: "ok"}) {
		t.Fatal("record was rejected")
	}
	select {
	case err := <-gotError:
		if !errors.Is(err, boom) {
			t.Fatalf("reported error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("writer error was not reported")
	}
	cancel()
	<-done
}

func TestCallRecorderConcurrentOverflowAccounting(t *testing.T) {
	var mu sync.Mutex
	drops := 0
	recorder := NewCallRecorder(callBatchWriterFunc(func(context.Context, []CallRecord) error { return nil }), 1, 1, time.Hour, func() {
		mu.Lock()
		drops++
		mu.Unlock()
	})
	if !recorder.Record(CallRecord{KeyID: 1, Method: "flickr.test", Status: "ok"}) {
		t.Fatal("first record was rejected")
	}
	const workers = 64
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			recorder.Record(CallRecord{KeyID: 1, Method: "flickr.test", Status: "ok"})
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if drops != workers || recorder.Dropped() != workers {
		t.Fatalf("callback drops=%d atomic drops=%d; want %d", drops, recorder.Dropped(), workers)
	}
}
