package obs

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestContextHandlerAddsSupportedFieldsAndTraceID(t *testing.T) {
	var output bytes.Buffer
	traceID, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	if err != nil {
		t.Fatal(err)
	}
	spanID, err := trace.SpanIDFromHex("0102030405060708")
	if err != nil {
		t.Fatal(err)
	}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	}))
	ctx = WithFields(ctx, map[string]any{
		"node":      "node-a",
		"role":      "crawl",
		"egress_ip": "192.0.2.10",
		"user_id":   int64(12),
		"key_id":    int64(34),
		"lane":      "interactive",
		"ignored":   "not allowed",
	})
	logger := slog.New(NewHandler(&output, slog.LevelDebug, nil))
	logger.InfoContext(ctx, "call finished")

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("unmarshal JSON log: %v", err)
	}
	for key, want := range map[string]any{
		"node": "node-a", "role": "crawl", "egress_ip": "192.0.2.10",
		"user_id": float64(12), "key_id": float64(34), "lane": "interactive",
		"trace_id": traceID.String(),
	} {
		if got := record[key]; got != want {
			t.Errorf("record[%q] = %#v, want %#v", key, got, want)
		}
	}
	if _, ok := record["ignored"]; ok {
		t.Fatal("unsupported context field was logged")
	}
}

func TestHandlerRedactsSensitiveAttributesAndContextOverride(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewHandler(&output, slog.LevelDebug, nil))
	ctx := WithFields(context.Background(), map[string]any{"user_id": int64(9)})
	type nestedCredentials struct {
		ClientToken string `json:"client_token"`
		Safe        string `json:"safe"`
	}
	logger.With("session_token", "bound-secret").InfoContext(ctx, "authorized",
		"user_id", int64(5),
		"nested", map[string]any{"password": "nested-secret", "safe": "ok"},
		"credentials", nestedCredentials{ClientToken: "struct-secret", Safe: "visible"},
	)
	encoded := output.String()
	for _, secret := range []string{"bound-secret", "nested-secret", "struct-secret", `"user_id":5`} {
		if strings.Contains(encoded, secret) {
			t.Errorf("log exposed or failed to override %q: %s", secret, encoded)
		}
	}
	if !strings.Contains(encoded, `"user_id":9`) || !strings.Contains(encoded, "[REDACTED]") || !strings.Contains(encoded, `"safe":"ok"`) || !strings.Contains(encoded, `"safe":"visible"`) {
		t.Fatalf("unexpected redacted log: %s", encoded)
	}
}

func TestWithFieldsMergesNearestAndIgnoresEmptyValues(t *testing.T) {
	ctx := WithFields(context.Background(), map[string]any{"role": "api", "node": "node-a"})
	ctx = WithFields(ctx, map[string]any{"role": "crawl", "lane": " ", "unknown": "value"})
	fields := FieldsFromContext(ctx)
	if fields["role"] != "crawl" || fields["node"] != "node-a" {
		t.Fatalf("merged fields = %#v", fields)
	}
	if _, ok := fields["lane"]; ok {
		t.Fatalf("empty lane was retained: %#v", fields)
	}
	if _, ok := fields["unknown"]; ok {
		t.Fatalf("unsupported field was retained: %#v", fields)
	}
}
