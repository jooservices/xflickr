// Package obs provides structured logging and OpenTelemetry wiring.
package obs

import (
	"context"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

type fieldsContextKey struct{}

var contextFieldNames = map[string]struct{}{
	"node":      {},
	"role":      {},
	"egress_ip": {},
	"user_id":   {},
	"key_id":    {},
	"lane":      {},
}

// WithFields attaches the supported observability fields to ctx. Nested calls
// merge with existing values; values from the nearest context take precedence.
func WithFields(ctx context.Context, fields map[string]any) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	merged := FieldsFromContext(ctx)
	for key, value := range fields {
		if _, ok := contextFieldNames[key]; !ok || value == nil {
			continue
		}
		if text, ok := value.(string); ok && strings.TrimSpace(text) == "" {
			continue
		}
		merged[key] = value
	}
	return context.WithValue(ctx, fieldsContextKey{}, merged)
}

// FieldsFromContext returns a copy of the supported fields attached to ctx.
func FieldsFromContext(ctx context.Context) map[string]any {
	if ctx == nil {
		return map[string]any{}
	}
	fields, _ := ctx.Value(fieldsContextKey{}).(map[string]any)
	copyOfFields := make(map[string]any, len(fields))
	for key, value := range fields {
		copyOfFields[key] = value
	}
	return copyOfFields
}

func fieldsForLog(ctx context.Context) []any {
	fields := FieldsFromContext(ctx)
	spanContext := trace.SpanContextFromContext(ctx)
	if spanContext.IsValid() {
		fields["trace_id"] = spanContext.TraceID().String()
	}
	keys := []string{"node", "role", "egress_ip", "user_id", "key_id", "lane", "trace_id"}
	attrs := make([]any, 0, len(keys)*2)
	for _, key := range keys {
		if value, ok := fields[key]; ok {
			attrs = append(attrs, key, value)
		}
	}
	return attrs
}

func eventAttrs(attrs map[string]any) []any {
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := make([]any, 0, len(attrs)*2)
	for _, key := range keys {
		if isSensitiveKey(key) {
			result = append(result, key, "[REDACTED]")
			continue
		}
		result = append(result, key, redactAny(attrs[key]))
	}
	return result
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(key)
	return strings.Contains(key, "secret") || strings.Contains(key, "token") || strings.Contains(key, "password")
}
