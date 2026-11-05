package obs

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
)

// sentryFlushTimeout bounds how long shutdown waits for buffered events.
const sentryFlushTimeout = 2 * time.Second

// sentryLogContext is the event context that carries the log record attributes.
const sentryLogContext = "log"

// sentryReporter forwards error-level log records to Sentry as issues. It owns a
// dedicated client and scope so no global Sentry state is installed.
type sentryReporter struct {
	client *sentry.Client
	scope  *sentry.Scope
}

// newSentryReporter returns nil without error when no DSN is configured, so an
// unconfigured process behaves exactly as before. An invalid DSN returns a
// generic error that never echoes the configured value.
func newSentryReporter(cfg Config, transport sentry.Transport) (*sentryReporter, error) {
	dsn := strings.TrimSpace(cfg.SentryDSN)
	if dsn == "" {
		return nil, nil
	}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:              dsn,
		Environment:      strings.TrimSpace(cfg.SentryEnvironment),
		Release:          cfg.Version,
		ServerName:       cfg.Node,
		AttachStacktrace: true,
		Transport:        transport,
	})
	if err != nil {
		return nil, errors.New("initialize Sentry client")
	}
	return &sentryReporter{client: client, scope: sentry.NewScope()}, nil
}

// Handler returns the slog handler for the reporter, or nil when disabled.
func (r *sentryReporter) Handler() slog.Handler {
	if r == nil {
		return nil
	}
	return sentryHandler{reporter: r}
}

// Flush waits for buffered events. A disabled reporter has nothing to flush.
func (r *sentryReporter) Flush(timeout time.Duration) bool {
	if r == nil {
		return true
	}
	return r.client.Flush(timeout)
}

// sentryHandler is an slog.Handler that reports records at error level or above.
type sentryHandler struct {
	reporter *sentryReporter
	attrs    []slog.Attr
	groups   []string
}

func (sentryHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelError
}

func (h sentryHandler) Handle(_ context.Context, record slog.Record) error {
	extra := make(map[string]any, len(h.attrs)+record.NumAttrs())
	var cause error
	collect := func(groups []string, attr slog.Attr) {
		collectSentryAttr(extra, &cause, groups, attr)
	}
	for _, attr := range h.attrs {
		collect(nil, attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		collect(h.groups, attr)
		return true
	})

	var event *sentry.Event
	if cause != nil {
		event = h.reporter.client.EventFromException(cause, sentry.LevelError)
	} else {
		event = h.reporter.client.EventFromMessage(record.Message, sentry.LevelError)
	}
	event.Message = record.Message
	event.Timestamp = record.Time
	event.Logger = "slog"
	event.Contexts = map[string]sentry.Context{sentryLogContext: extra}
	h.reporter.client.CaptureEvent(event, &sentry.EventHint{}, h.reporter.scope)
	return nil
}

// WithAttrs binds attributes under the groups opened so far.
func (h sentryHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	bound := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	bound = append(bound, h.attrs...)
	for _, attr := range attrs {
		bound = append(bound, prefixAttr(h.groups, attr))
	}
	h.attrs = bound
	return h
}

func (h sentryHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	h.groups = append(append([]string(nil), h.groups...), name)
	return h
}

// prefixAttr nests an attribute under the given groups so pre-bound attributes
// keep the group names that were open when they were added.
func prefixAttr(groups []string, attr slog.Attr) slog.Attr {
	for i := len(groups) - 1; i >= 0; i-- {
		attr = slog.Attr{Key: groups[i], Value: slog.GroupValue(attr)}
	}
	return attr
}

// collectSentryAttr flattens an attribute into dotted keys and remembers the
// first error value so it can be reported as an exception.
func collectSentryAttr(extra map[string]any, cause *error, groups []string, attr slog.Attr) {
	value := attr.Value.Resolve()
	if value.Kind() == slog.KindGroup {
		nested := groups
		if attr.Key != "" {
			nested = append(append([]string(nil), groups...), attr.Key)
		}
		for _, child := range value.Group() {
			collectSentryAttr(extra, cause, nested, child)
		}
		return
	}
	key := strings.Join(append(append([]string(nil), groups...), attr.Key), ".")
	if err, ok := value.Any().(error); ok {
		if *cause == nil {
			*cause = err
		}
		extra[key] = err.Error()
		return
	}
	extra[key] = value.Any()
}
