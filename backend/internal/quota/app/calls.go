package app

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"
)

// CallRecord is the non-sensitive metadata stored for one upstream call.
// Credential material and request/response bodies must never be added here.
type CallRecord struct {
	At              time.Time
	KeyID           int64
	UserID          *int64
	AccountID       *int64
	FlickrAccount   string
	Node            string
	Method          string
	Status          string
	ErrorCode       *int64
	LatencyMS       int64
	Bytes           int64
	Lane            string
	Facet           string
	Attempt         int
	StartedAt       time.Time
	HTTPStatus      *int
	FlickrStat      string
	FlickrErrorCode *int64
	ErrorMessage    string
	ResponseBytes   int64
	NodeID          string
	RequestID       string
	Outcome         string
}

// CallBatchWriter persists a batch of call records.
type CallBatchWriter interface {
	InsertCalls(context.Context, []CallRecord) error
}

// CallRecorder accepts records without waiting for persistence and flushes
// them in bounded batches on a background worker.
type CallRecorder struct {
	writer    CallBatchWriter
	queue     chan CallRecord
	batchSize int
	interval  time.Duration
	onDrop    func()
	started   atomic.Bool
	stopped   atomic.Bool
	dropped   atomic.Uint64
}

// NewCallRecorder creates a recorder with bounded storage. The callback must
// be non-blocking; it is intended for incrementing a metric.
func NewCallRecorder(writer CallBatchWriter, queueSize, batchSize int, interval time.Duration, onDrop func()) *CallRecorder {
	if queueSize <= 0 {
		queueSize = 10_000
	}
	if batchSize <= 0 || batchSize > queueSize {
		batchSize = 500
	}
	if interval <= 0 {
		interval = time.Second
	}
	return &CallRecorder{writer: writer, queue: make(chan CallRecord, queueSize), batchSize: batchSize, interval: interval, onDrop: onDrop}
}

// Record queues one record without blocking. It returns false when the
// bounded queue is full or the worker has stopped.
func (r *CallRecorder) Record(record CallRecord) bool {
	if r == nil || r.writer == nil || r.queue == nil || r.stopped.Load() {
		return r.drop()
	}
	if record.At.IsZero() {
		record.At = time.Now().UTC()
	} else {
		record.At = record.At.UTC()
	}
	if record.KeyID <= 0 || record.Method == "" || record.Status == "" || record.LatencyMS < 0 || record.Bytes < 0 {
		return r.drop()
	}
	select {
	case r.queue <- record:
		return true
	default:
		return r.drop()
	}
}

func (r *CallRecorder) drop() bool {
	if r == nil {
		return false
	}
	r.dropped.Add(1)
	if r.onDrop != nil {
		r.onDrop()
	}
	return false
}

// Dropped returns the number of records rejected by validation, a full queue,
// or a stopped worker.
func (r *CallRecorder) Dropped() uint64 {
	if r == nil {
		return 0
	}
	return r.dropped.Load()
}

// Run flushes at batchSize or interval and drains queued records on shutdown.
// A failed batch is reported and discarded so a database outage cannot stall
// crawl work or make memory usage unbounded.
func (r *CallRecorder) Run(ctx context.Context, onError func(error)) {
	if r == nil || ctx == nil || r.writer == nil || !r.started.CompareAndSwap(false, true) {
		return
	}
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	batch := make([]CallRecord, 0, r.batchSize)
	for {
		select {
		case record := <-r.queue:
			batch = r.append(ctx, batch, record, onError)
		case <-ticker.C:
			batch = r.flush(ctx, batch, onError)
		case <-ctx.Done():
			r.stopped.Store(true)
			r.drain(ctx, batch, onError)
			return
		}
	}
}

func (r *CallRecorder) append(ctx context.Context, batch []CallRecord, record CallRecord, onError func(error)) []CallRecord {
	batch = append(batch, record)
	if len(batch) >= r.batchSize {
		return r.flush(ctx, batch, onError)
	}
	return batch
}

func (r *CallRecorder) flush(ctx context.Context, batch []CallRecord, onError func(error)) []CallRecord {
	if len(batch) == 0 {
		return batch
	}
	if err := r.writer.InsertCalls(ctx, batch); err != nil && onError != nil {
		onError(fmt.Errorf("quota: insert call log batch: %w", err))
	}
	return batch[:0]
}

func (r *CallRecorder) drain(ctx context.Context, batch []CallRecord, onError func(error)) {
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for {
		select {
		case record := <-r.queue:
			batch = r.append(shutdownCtx, batch, record, onError)
		default:
			r.flush(shutdownCtx, batch, onError)
			return
		}
	}
}

// ValidateCallRecord is shared by storage adapters before persisting a batch.
func ValidateCallRecord(record CallRecord) error {
	if record.KeyID <= 0 {
		return errors.New("quota: call record requires a key")
	}
	if record.Method == "" || record.Status == "" {
		return errors.New("quota: call record requires key, method and status")
	}
	if record.At.IsZero() {
		return errors.New("quota: call record time is invalid")
	}
	if record.LatencyMS < 0 || record.Bytes < 0 || record.ResponseBytes < 0 || record.Attempt < 0 {
		return errors.New("quota: call record latency and bytes are invalid")
	}
	if len(record.ErrorMessage) > 512 {
		return errors.New("quota: call record error message is too long")
	}
	switch record.Outcome {
	case "", "success", "failure", "throttled", "quota_wait":
	default:
		return errors.New("quota: call record outcome is invalid")
	}
	if record.RequestID != "" && len(record.RequestID) != 36 {
		return errors.New("quota: call record request id is invalid")
	}
	return nil
}
