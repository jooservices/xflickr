package obs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

const defaultBufferSize = 10_000

type eventBuffer struct {
	mu      sync.Mutex
	limit   int
	records []sdklog.Record
	dropped uint64
}

func newEventBuffer(limit int) *eventBuffer {
	if limit < 1 {
		limit = defaultBufferSize
	}
	return &eventBuffer{limit: limit}
}

func (b *eventBuffer) add(records []sdklog.Record) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range records {
		b.records = append(b.records, records[i].Clone())
		if len(b.records) > b.limit {
			copy(b.records, b.records[len(b.records)-b.limit:])
			b.records = b.records[:b.limit]
			b.dropped++
		}
	}
}

func (b *eventBuffer) snapshot() []sdklog.Record {
	b.mu.Lock()
	defer b.mu.Unlock()
	copyOfRecords := make([]sdklog.Record, len(b.records))
	for i := range b.records {
		copyOfRecords[i] = b.records[i].Clone()
	}
	return copyOfRecords
}

func (b *eventBuffer) discard(count int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if count <= 0 {
		return
	}
	if count >= len(b.records) {
		b.records = nil
		return
	}
	b.records = append([]sdklog.Record(nil), b.records[count:]...)
}

func (b *eventBuffer) stats() (int, uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.records), b.dropped
}

type bufferedLogExporter struct {
	mu        sync.Mutex
	delegate  sdklog.Exporter
	buffer    *eventBuffer
	spillPath string
	closed    bool
}

func newBufferedLogExporter(delegate sdklog.Exporter, spillPath string, capacity int) *bufferedLogExporter {
	return &bufferedLogExporter{delegate: delegate, buffer: newEventBuffer(capacity), spillPath: spillPath}
}

func (e *bufferedLogExporter) Export(ctx context.Context, records []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return sdklog.ErrExporterShutdown
	}
	pending := e.buffer.snapshot()
	batch := make([]sdklog.Record, 0, len(pending)+len(records))
	batch = append(batch, pending...)
	current := make([]sdklog.Record, len(records))
	for i := range records {
		current[i] = records[i].Clone()
	}
	redactLogRecords(current)
	batch = append(batch, current...)
	redactLogRecords(batch)
	if err := e.delegate.Export(ctx, batch); err != nil {
		e.buffer.add(current)
		return nil
	}
	e.buffer.discard(len(pending))
	return nil
}

func (e *bufferedLogExporter) ForceFlush(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	pending := e.buffer.snapshot()
	if len(pending) > 0 {
		redactLogRecords(pending)
		if err := e.delegate.Export(ctx, pending); err == nil {
			e.buffer.discard(len(pending))
		}
	}
	// Export errors are retained for a later flush or shutdown spill.
	_ = e.delegate.ForceFlush(ctx)
	return nil
}

func (e *bufferedLogExporter) Shutdown(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	pending := e.buffer.snapshot()
	if len(pending) > 0 {
		redactLogRecords(pending)
		if err := e.delegate.Export(ctx, pending); err == nil {
			e.buffer.discard(len(pending))
		}
	}
	var spillErr error
	if remaining := e.buffer.snapshot(); len(remaining) > 0 {
		spillErr = spillRecords(e.spillPath, remaining)
	}
	_ = e.delegate.Shutdown(ctx)
	return spillErr
}

func redactLogRecords(records []sdklog.Record) {
	for i := range records {
		record := records[i].Clone()
		var attrs []attribute.KeyValue
		record.WalkAttributes(func(kv attribute.KeyValue) bool {
			key := string(kv.Key)
			if isSensitiveKey(key) {
				kv = attribute.String(key, "[REDACTED]")
			}
			attrs = append(attrs, kv)
			return true
		})
		record.SetAttributes(attrs...)
		records[i] = record
	}
}

func spillRecords(path string, records []sdklog.Record) error {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "var/obs-spill.jsonl"
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create observability spill directory: %w", err)
		}
	}
	// #nosec G304 -- this is the explicit local spill path from application configuration.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open observability spill file: %w", err)
	}
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("set observability spill file permissions: %w", err)
	}
	encoder := json.NewEncoder(file)
	for _, record := range records {
		if err := encoder.Encode(recordForSpill(record)); err != nil {
			return err
		}
	}
	return nil
}

func recordForSpill(record sdklog.Record) map[string]any {
	attributes := make(map[string]any, record.AttributesLen())
	record.WalkAttributes(func(kv attribute.KeyValue) bool {
		key := string(kv.Key)
		if isSensitiveKey(key) {
			attributes[key] = "[REDACTED]"
		} else {
			attributes[key] = redactAny(kv.Value.AsInterface())
		}
		return true
	})
	entry := map[string]any{
		"timestamp":          record.Timestamp().Format(time.RFC3339Nano),
		"observed_timestamp": record.ObservedTimestamp().Format(time.RFC3339Nano),
		"severity":           record.SeverityText(),
		"body":               record.Body().AsInterface(),
		"attributes":         attributes,
	}
	if eventName := record.EventName(); eventName != "" {
		entry["event_name"] = eventName
	}
	if traceID := record.TraceID(); traceID.IsValid() {
		entry["trace_id"] = traceID.String()
	}
	if spanID := record.SpanID(); spanID.IsValid() {
		entry["span_id"] = spanID.String()
	}
	return entry
}
