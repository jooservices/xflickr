package obs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/log/logtest"
	"go.opentelemetry.io/otel/trace"
)

type fakeLogExporter struct {
	err      error
	exports  [][]sdklog.Record
	shutdown bool
}

func (f *fakeLogExporter) Export(_ context.Context, records []sdklog.Record) error {
	clone := make([]sdklog.Record, len(records))
	for i := range records {
		clone[i] = records[i].Clone()
	}
	f.exports = append(f.exports, clone)
	return f.err
}

func (f *fakeLogExporter) ForceFlush(context.Context) error { return f.err }

func (f *fakeLogExporter) Shutdown(context.Context) error {
	f.shutdown = true
	return nil
}

func TestEventBufferOverflowKeepsNewestRecords(t *testing.T) {
	buffer := newEventBuffer(3)
	buffer.add([]sdklog.Record{testRecord("one"), testRecord("two"), testRecord("three"), testRecord("four"), testRecord("five")})
	got := recordBodies(buffer.snapshot())
	want := []string{"three", "four", "five"}
	if !equalStrings(got, want) {
		t.Fatalf("buffer bodies = %#v, want %#v", got, want)
	}
	if size, dropped := buffer.stats(); size != 3 || dropped != 2 {
		t.Fatalf("buffer stats = (%d, %d), want (3, 2)", size, dropped)
	}
}

func TestEventBufferDefaultCapacityAndDiscardEdges(t *testing.T) {
	buffer := newEventBuffer(0)
	if buffer.limit != defaultBufferSize {
		t.Fatalf("default buffer limit = %d, want %d", buffer.limit, defaultBufferSize)
	}
	buffer.add([]sdklog.Record{testRecord("one"), testRecord("two"), testRecord("three")})
	buffer.discard(-1)
	buffer.discard(0)
	if got := recordBodies(buffer.snapshot()); !equalStrings(got, []string{"one", "two", "three"}) {
		t.Fatalf("records after non-positive discard = %#v", got)
	}
	buffer.discard(1)
	if got := recordBodies(buffer.snapshot()); !equalStrings(got, []string{"two", "three"}) {
		t.Fatalf("records after partial discard = %#v", got)
	}
	buffer.discard(99)
	if size, _ := buffer.stats(); size != 0 {
		t.Fatalf("buffer size after oversized discard = %d", size)
	}
}

func TestRedactLogRecordsReplacesSensitiveAttributes(t *testing.T) {
	records := []sdklog.Record{(logtest.RecordFactory{
		Body: attribute.StringValue("body"),
		Attributes: []attribute.KeyValue{
			attribute.String("api_token", "sensitive-value"),
			attribute.String("user_id", "safe-value"),
		},
	}).NewRecord()}

	redactLogRecords(records)
	attributes := make(map[string]string)
	count := 0
	records[0].WalkAttributes(func(kv attribute.KeyValue) bool {
		count++
		attributes[string(kv.Key)] = kv.Value.AsString()
		return true
	})
	if count != 2 || attributes["api_token"] != "[REDACTED]" || attributes["user_id"] != "safe-value" {
		t.Fatal("log record redaction did not replace the sensitive attribute")
	}
}

func TestBufferedExporterSpillsUnsentLatestRecordsOnShutdown(t *testing.T) {
	spillPath := filepath.Join(t.TempDir(), "nested", "obs-spill.jsonl")
	delegate := &fakeLogExporter{err: errors.New("receiver unavailable")}
	exporter := newBufferedLogExporter(delegate, spillPath, 2)
	if err := exporter.Export(context.Background(), []sdklog.Record{
		testRecord("one"), testRecord("two"), testRecordWithAttribute("three", "api_token", "sensitive"),
	}); err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	pending := exporter.buffer.snapshot()
	var bufferedToken string
	pending[len(pending)-1].WalkAttributes(func(kv attribute.KeyValue) bool {
		if string(kv.Key) == "api_token" {
			bufferedToken = kv.Value.AsString()
		}
		return true
	})
	if bufferedToken != "[REDACTED]" {
		t.Fatal("failed export retained a sensitive attribute in the retry buffer")
	}
	if err := exporter.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if !delegate.shutdown {
		t.Fatal("delegate was not shut down")
	}
	contents, err := os.ReadFile(spillPath)
	if err != nil {
		t.Fatalf("ReadFile(spill): %v", err)
	}
	if got := string(contents); !containsAll(got, `"body":"two"`, `"body":"three"`, `"api_token":"[REDACTED]"`) || containsAny(got, `"body":"one"`, "sensitive") {
		t.Fatalf("spill does not contain the latest redacted records: %s", got)
	}
	info, err := os.Stat(spillPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("spill file mode = %o, want 600", info.Mode().Perm())
	}
}

func TestBufferedExporterRetriesAndClearsBufferAfterRecovery(t *testing.T) {
	delegate := &fakeLogExporter{err: errors.New("receiver unavailable")}
	exporter := newBufferedLogExporter(delegate, filepath.Join(t.TempDir(), "spill.jsonl"), 10)
	if err := exporter.Export(context.Background(), []sdklog.Record{testRecord("first")}); err != nil {
		t.Fatal(err)
	}
	delegate.err = nil
	if err := exporter.Export(context.Background(), []sdklog.Record{testRecord("second")}); err != nil {
		t.Fatal(err)
	}
	if size, _ := exporter.buffer.stats(); size != 0 {
		t.Fatalf("buffer size after recovery = %d, want 0", size)
	}
	if got := recordBodies(delegate.exports[len(delegate.exports)-1]); !equalStrings(got, []string{"first", "second"}) {
		t.Fatalf("recovery export = %#v", got)
	}
}

func TestBufferedExporterForceFlushAndClosedBehavior(t *testing.T) {
	delegate := &fakeLogExporter{}
	exporter := newBufferedLogExporter(delegate, filepath.Join(t.TempDir(), "spill.jsonl"), 10)
	exporter.buffer.add([]sdklog.Record{testRecord("pending")})
	if err := exporter.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush() error = %v", err)
	}
	if size, _ := exporter.buffer.stats(); size != 0 {
		t.Fatalf("buffer size after successful ForceFlush() = %d", size)
	}
	if err := exporter.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := exporter.Export(context.Background(), []sdklog.Record{testRecord("after shutdown")}); !errors.Is(err, sdklog.ErrExporterShutdown) {
		t.Fatalf("Export() after shutdown = %v", err)
	}
	if err := exporter.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush() after shutdown = %v", err)
	}
}

func TestSpillRecordsWritesOneJSONRecordPerLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := spillRecords(path, []sdklog.Record{testRecord("one"), testRecord("two")}); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := splitLines(string(contents))
	if len(lines) != 2 {
		t.Fatalf("spill lines = %d, want 2", len(lines))
	}
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("spill line is not JSON: %v", err)
		}
	}
}

func TestSpillRecordsDefaultPathAndErrorPaths(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := spillRecords("", []sdklog.Record{testRecord("default-path")}); err != nil {
		t.Fatalf("spillRecords() default path error = %v", err)
	}
	if _, err := os.Stat(filepath.Join("var", "obs-spill.jsonl")); err != nil {
		t.Fatalf("default spill path was not created: %v", err)
	}

	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := spillRecords(filepath.Join(blocker, "child", "events.jsonl"), []sdklog.Record{testRecord("x")}); err == nil {
		t.Fatal("spillRecords() succeeded when the parent path is a file")
	}
	if err := spillRecords(t.TempDir(), []sdklog.Record{testRecord("x")}); err == nil {
		t.Fatal("spillRecords() succeeded when the output path is a directory")
	}
}

func TestRecordForSpillIncludesEventTraceAndRedactedAttributes(t *testing.T) {
	traceID, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	if err != nil {
		t.Fatal(err)
	}
	spanID, err := trace.SpanIDFromHex("0102030405060708")
	if err != nil {
		t.Fatal(err)
	}
	record := (logtest.RecordFactory{
		EventName: "spill.test", Body: attribute.StringValue("body"),
		Timestamp: time.Unix(1, 0).UTC(), ObservedTimestamp: time.Unix(2, 0).UTC(),
		TraceID: traceID, SpanID: spanID,
		Attributes: []attribute.KeyValue{attribute.String("safe", "value"), attribute.String("access_token", "secret")},
	}).NewRecord()
	entry := recordForSpill(record)
	if entry["event_name"] != "spill.test" || entry["trace_id"] != traceID.String() || entry["span_id"] != spanID.String() {
		t.Fatalf("spill entry metadata = %#v", entry)
	}
	attrs := entry["attributes"].(map[string]any)
	if attrs["safe"] != "value" || attrs["access_token"] != "[REDACTED]" {
		t.Fatalf("spill attributes = %#v", attrs)
	}
}

func testRecord(body string) sdklog.Record {
	return (logtest.RecordFactory{
		Timestamp:         time.Unix(1, 0).UTC(),
		ObservedTimestamp: time.Unix(1, 0).UTC(),
		SeverityText:      "INFO",
		Body:              attribute.StringValue(body),
	}).NewRecord()
}

func testRecordWithAttribute(body, key, value string) sdklog.Record {
	return (logtest.RecordFactory{
		Timestamp:         time.Unix(1, 0).UTC(),
		ObservedTimestamp: time.Unix(1, 0).UTC(),
		SeverityText:      "INFO",
		Body:              attribute.StringValue(body),
		Attributes:        []attribute.KeyValue{attribute.String(key, value)},
	}).NewRecord()
}

func recordBodies(records []sdklog.Record) []string {
	bodies := make([]string, len(records))
	for i := range records {
		bodies[i] = records[i].Body().AsString()
	}
	return bodies
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func containsAll(value string, substrings ...string) bool {
	for _, substring := range substrings {
		if !strings.Contains(value, substring) {
			return false
		}
	}
	return true
}

func containsAny(value string, substrings ...string) bool {
	for _, substring := range substrings {
		if strings.Contains(value, substring) {
			return true
		}
	}
	return false
}

func splitLines(value string) []string {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}
