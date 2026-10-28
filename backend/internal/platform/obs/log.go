package obs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
)

// NewHandler creates a JSON slog handler that also forwards records to the
// optional OpenTelemetry slog bridge. Context fields are added to both outputs.
func NewHandler(output io.Writer, level slog.Level, otelHandler slog.Handler) slog.Handler {
	jsonHandler := slog.NewJSONHandler(output, &slog.HandlerOptions{Level: level})
	handlers := []slog.Handler{jsonHandler}
	if otelHandler != nil {
		handlers = append(handlers, otelHandler)
	}
	return contextHandler{next: multiHandler{handlers: handlers}}
}

type contextHandler struct {
	next slog.Handler
}

func (h contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h contextHandler) Handle(ctx context.Context, record slog.Record) error {
	contextAttrs := slogAttrs(fieldsForLog(ctx))
	contextKeys := make(map[string]struct{}, len(contextAttrs))
	for _, attr := range contextAttrs {
		contextKeys[attr.Key] = struct{}{}
	}
	attrs := make([]slog.Attr, 0, record.NumAttrs()+len(contextAttrs))
	record.Attrs(func(attr slog.Attr) bool {
		if _, overridden := contextKeys[attr.Key]; !overridden {
			attrs = append(attrs, redactAttr(attr))
		}
		return true
	})
	attrs = append(attrs, contextAttrs...)
	redacted := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	redacted.AddAttrs(attrs...)
	if err := h.next.Handle(ctx, redacted); err != nil {
		return fmt.Errorf("handle structured log record: %w", err)
	}
	return nil
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{next: h.next.WithAttrs(redactAttrs(attrs))}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{next: h.next.WithGroup(name)}
}

type multiHandler struct {
	handlers []slog.Handler
}

func (h multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h multiHandler) Handle(ctx context.Context, record slog.Record) error {
	var failures []error
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, record.Level) {
			if err := handler.Handle(ctx, record); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}

func (h multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	children := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		children[i] = handler.WithAttrs(attrs)
	}
	return multiHandler{handlers: children}
}

func (h multiHandler) WithGroup(name string) slog.Handler {
	children := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		children[i] = handler.WithGroup(name)
	}
	return multiHandler{handlers: children}
}

func slogAttrs(flat []any) []slog.Attr {
	attrs := make([]slog.Attr, 0, len(flat)/2)
	for i := 0; i+1 < len(flat); i += 2 {
		key, ok := flat[i].(string)
		if !ok {
			continue
		}
		attrs = append(attrs, slog.Any(key, flat[i+1]))
	}
	return attrs
}

func redactAttrs(attrs []slog.Attr) []slog.Attr {
	redacted := make([]slog.Attr, len(attrs))
	for i, attr := range attrs {
		redacted[i] = redactAttr(attr)
	}
	return redacted
}

func redactAttr(attr slog.Attr) slog.Attr {
	if isSensitiveKey(attr.Key) {
		return slog.String(attr.Key, "[REDACTED]")
	}
	value := attr.Value.Resolve()
	if value.Kind() == slog.KindGroup {
		group := value.Group()
		for i := range group {
			group[i] = redactAttr(group[i])
		}
		return slog.Group(attr.Key, groupToArgs(group)...)
	}
	if value.Kind() == slog.KindAny {
		return slog.Any(attr.Key, redactAny(value.Any()))
	}
	return slog.Attr{Key: attr.Key, Value: value}
}

func groupToArgs(attrs []slog.Attr) []any {
	args := make([]any, 0, len(attrs)*2)
	for _, attr := range attrs {
		args = append(args, attr)
	}
	return args
}

func redactAny(value any) any {
	return redactAnyDepth(value, 0)
}

func redactAnyDepth(value any, depth int) any {
	if depth > 32 {
		return "[REDACTED]"
	}
	switch typed := value.(type) {
	case map[string]any:
		copyOfMap := make(map[string]any, len(typed))
		for key, nested := range typed {
			if isSensitiveKey(key) {
				copyOfMap[key] = "[REDACTED]"
			} else {
				copyOfMap[key] = redactAnyDepth(nested, depth+1)
			}
		}
		return copyOfMap
	case map[string]string:
		copyOfMap := make(map[string]string, len(typed))
		for key, nested := range typed {
			if isSensitiveKey(key) {
				copyOfMap[key] = "[REDACTED]"
			} else {
				copyOfMap[key] = nested
			}
		}
		return copyOfMap
	case []any:
		copyOfSlice := make([]any, len(typed))
		for i, nested := range typed {
			copyOfSlice[i] = redactAnyDepth(nested, depth+1)
		}
		return copyOfSlice
	default:
		encoded, err := json.Marshal(value)
		if err == nil {
			var decoded any
			if json.Unmarshal(encoded, &decoded) == nil {
				switch decoded.(type) {
				case map[string]any, []any:
					return redactAnyDepth(decoded, depth+1)
				}
			}
		}
		return value
	}
}
