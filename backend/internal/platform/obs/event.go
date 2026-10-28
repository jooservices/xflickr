package obs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Event emits a named business event to slog and to the active OpenTelemetry
// span, when one is present. Sensitive attribute names are redacted first.
func Event(ctx context.Context, name string, attrs map[string]any) {
	if ctx == nil {
		ctx = context.Background()
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	redacted := redactAnyMap(attrs)
	slogAttrs := []slog.Attr{slog.String("event.name", name)}
	flatAttrs := eventAttrs(redacted)
	for i := 0; i+1 < len(flatAttrs); i += 2 {
		key, _ := flatAttrs[i].(string)
		slogAttrs = append(slogAttrs, slog.Any(key, flatAttrs[i+1]))
	}
	slog.Default().LogAttrs(ctx, slog.LevelInfo, name, slogAttrs...)

	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	spanAttrs := make([]attribute.KeyValue, 0, len(redacted))
	for key, value := range redacted {
		if isSensitiveKey(key) {
			spanAttrs = append(spanAttrs, attribute.String(key, "[REDACTED]"))
			continue
		}
		spanAttrs = append(spanAttrs, toAttribute(key, value))
	}
	span.AddEvent(name, trace.WithAttributes(spanAttrs...))
}

func redactAnyMap(attrs map[string]any) map[string]any {
	redacted, _ := redactAny(attrs).(map[string]any)
	if redacted == nil {
		return map[string]any{}
	}
	return redacted
}

func toAttribute(key string, value any) attribute.KeyValue {
	switch typed := value.(type) {
	case string:
		return attribute.String(key, typed)
	case bool:
		return attribute.Bool(key, typed)
	case int:
		return attribute.Int(key, typed)
	case int64:
		return attribute.Int64(key, typed)
	case uint64:
		const maxInt64 = uint64(1<<63 - 1)
		if typed > maxInt64 {
			return attribute.String(key, strconv.FormatUint(typed, 10))
		}
		return attribute.Int64(key, int64(typed)) // #nosec G115 -- values above MaxInt64 are returned as strings.
	case float64:
		return attribute.Float64(key, typed)
	case []string:
		return attribute.StringSlice(key, typed)
	case []int64:
		return attribute.Int64Slice(key, typed)
	case []bool:
		return attribute.BoolSlice(key, typed)
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return attribute.String(key, fmt.Sprint(typed))
		}
		return attribute.String(key, string(encoded))
	}
}
