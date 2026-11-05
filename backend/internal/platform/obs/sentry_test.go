package obs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
)

const testSentryDSN = "https://public@sentry.example.invalid/1"

// recordingTransport captures events instead of sending them.
type recordingTransport struct {
	mu      sync.Mutex
	events  []*sentry.Event
	flushed int
	closed  int
	// stuck makes Flush report that events remain unsent.
	stuck bool
}

func (t *recordingTransport) Configure(sentry.ClientOptions) {}
func (t *recordingTransport) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed++
}

func (t *recordingTransport) SendEvent(event *sentry.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, event)
}

func (t *recordingTransport) Flush(time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.flushed++
	return !t.stuck
}

func (t *recordingTransport) FlushWithContext(context.Context) bool { return t.Flush(0) }

func (t *recordingTransport) captured() []*sentry.Event {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]*sentry.Event(nil), t.events...)
}

// logAttrs returns the log record attributes carried by a captured event.
func logAttrs(event *sentry.Event) map[string]interface{} {
	return event.Contexts[sentryLogContext]
}

func newTestReporter(t *testing.T) (*sentryReporter, *recordingTransport) {
	t.Helper()
	transport := &recordingTransport{}
	reporter, err := newSentryReporter(Config{
		SentryDSN:         testSentryDSN,
		SentryEnvironment: "test",
		Version:           "1.2.3",
		Node:              "node-a",
	}, transport)
	if err != nil || reporter == nil {
		t.Fatalf("newSentryReporter() = %v, %v", reporter, err)
	}
	return reporter, transport
}

func TestSentryReporterDisabledWithoutDSN(t *testing.T) {
	reporter, err := newSentryReporter(Config{SentryDSN: "  "}, nil)
	if err != nil || reporter != nil {
		t.Fatalf("empty DSN must disable Sentry quietly, got %v, %v", reporter, err)
	}
	if reporter.Handler() != nil {
		t.Fatal("a disabled reporter must not provide a handler")
	}
	if !reporter.Flush(time.Millisecond) {
		t.Fatal("flushing a disabled reporter must succeed")
	}
}

func TestSentryReporterRejectsInvalidDSNWithoutLeakingIt(t *testing.T) {
	reporter, err := newSentryReporter(Config{SentryDSN: "not a dsn with secret-key"}, nil)
	if err == nil || reporter != nil {
		t.Fatalf("invalid DSN must fail, got %v, %v", reporter, err)
	}
	if strings.Contains(err.Error(), "secret-key") {
		t.Fatalf("error leaks the DSN: %v", err)
	}
}

func TestSentryHandlerSendsOnlyErrorLevelAsEvents(t *testing.T) {
	reporter, transport := newTestReporter(t)
	logger := slog.New(reporter.Handler())

	logger.Info("routine")
	logger.Warn("degraded")
	logger.Error("crawl page failed", "facet", "photos", "attempt", 3)

	events := transport.captured()
	if len(events) != 1 {
		t.Fatalf("captured %d events, want exactly the error", len(events))
	}
	event := events[0]
	if event.Message != "crawl page failed" || event.Level != sentry.LevelError {
		t.Fatalf("unexpected event message/level: %q %q", event.Message, event.Level)
	}
	if event.Environment != "test" || event.Release != "1.2.3" || event.ServerName != "node-a" {
		t.Fatalf("event misses process identity: %+v", event)
	}
	if logAttrs(event)["facet"] != "photos" || logAttrs(event)["attempt"] != int64(3) {
		t.Fatalf("event misses log attributes: %#v", logAttrs(event))
	}
}

func TestSentryHandlerEnabledFollowsErrorThreshold(t *testing.T) {
	reporter, _ := newTestReporter(t)
	handler := reporter.Handler()
	if handler.Enabled(context.Background(), slog.LevelWarn) || !handler.Enabled(context.Background(), slog.LevelError) {
		t.Fatal("only error and above must be enabled")
	}
}

func TestSentryHandlerReportsErrorAttributeAsException(t *testing.T) {
	reporter, transport := newTestReporter(t)
	slog.New(reporter.Handler()).Error("job failed", "error", errors.New("database unavailable"))

	events := transport.captured()
	if len(events) != 1 || len(events[0].Exception) == 0 {
		t.Fatalf("an error attribute must become an exception: %+v", events)
	}
	if events[0].Exception[0].Value != "database unavailable" || events[0].Message != "job failed" {
		t.Fatalf("unexpected exception event: %+v", events[0])
	}
}

func TestSentryHandlerFlattensGroupsAndPreboundAttributes(t *testing.T) {
	reporter, transport := newTestReporter(t)
	logger := slog.New(reporter.Handler()).With("role", "crawl").WithGroup("request")
	logger.Error("bad request", slog.Group("flickr", "code", 105), "path", "/x")

	extra := logAttrs(transport.captured()[0])
	if extra["role"] != "crawl" || extra["request.flickr.code"] != int64(105) || extra["request.path"] != "/x" {
		t.Fatalf("attributes were not flattened: %#v", extra)
	}
}

func TestSentryReceivesOnlyRedactedAttributesThroughNewHandler(t *testing.T) {
	reporter, transport := newTestReporter(t)
	var output bytes.Buffer
	logger := slog.New(NewHandler(&output, slog.LevelInfo, nil, reporter.Handler()))

	logger.Error("auth failed", "api_secret", "super-secret-value", "user", "member")

	events := transport.captured()
	if len(events) != 1 {
		t.Fatalf("captured %d events, want 1", len(events))
	}
	for key, value := range logAttrs(events[0]) {
		if strings.Contains(key, "super-secret-value") || value == "super-secret-value" {
			t.Fatalf("secret reached Sentry: %#v", logAttrs(events[0]))
		}
	}
	if logAttrs(events[0])["user"] != "member" {
		t.Fatalf("non-secret attribute was lost: %#v", logAttrs(events[0]))
	}
}

func TestSentryReporterFlushDelegatesToTransport(t *testing.T) {
	reporter, transport := newTestReporter(t)
	if !reporter.Flush(time.Second) || transport.flushed == 0 {
		t.Fatal("flush must reach the transport")
	}
}

func TestSentryHandlerKeepsGroupsOpenedBeforeBoundAttributes(t *testing.T) {
	reporter, transport := newTestReporter(t)
	handler := reporter.Handler().WithGroup("").WithGroup("job").WithAttrs([]slog.Attr{slog.String("kind", "photo.sizes")})
	slog.New(handler).Error("job failed")

	if got := logAttrs(transport.captured()[0])["job.kind"]; got != "photo.sizes" {
		t.Fatalf("bound attribute lost its group: %#v", logAttrs(transport.captured()[0]))
	}
}

func TestConfigFromEnvironmentReadsSentrySettings(t *testing.T) {
	t.Setenv("SENTRY_DSN", "  "+testSentryDSN+"  ")
	t.Setenv("SENTRY_ENVIRONMENT", " develop ")
	cfg := ConfigFromEnvironment()
	if cfg.SentryDSN != testSentryDSN || cfg.SentryEnvironment != "develop" {
		t.Fatalf("sentry settings = %q, %q", cfg.SentryDSN, cfg.SentryEnvironment)
	}
}

func TestStartDeliversErrorRecordsToSentryAndFlushesOnShutdown(t *testing.T) {
	var mu sync.Mutex
	received := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		received++
		mu.Unlock()
		_, _ = w.Write([]byte(`{"id":"1"}`))
	}))
	defer server.Close()

	previous := slog.Default()
	runtime, err := Start(Config{
		SentryDSN: "http://public@" + strings.TrimPrefix(server.URL, "http://") + "/1",
		LogLevel:  slog.LevelInfo,
	})
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	slog.Error("crawl page failed", "error", errors.New("boom"))
	if err := runtime.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush() = %v", err)
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if received == 0 {
		t.Fatal("Sentry received no request after the error record was flushed")
	}
	if slog.Default() != previous {
		t.Fatal("Shutdown() did not restore the previous logger")
	}
}

func TestStartContinuesWhenSentryDSNIsInvalid(t *testing.T) {
	previous := slog.Default()
	runtime, err := Start(Config{SentryDSN: "not a dsn with secret-key", LogLevel: slog.LevelInfo})
	if err != nil || runtime == nil {
		t.Fatalf("an invalid Sentry DSN must not stop startup, got %v, %v", runtime, err)
	}
	if runtime.sentry != nil {
		t.Fatal("invalid DSN must leave Sentry disabled")
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() = %v", err)
	}
	if slog.Default() != previous {
		t.Fatal("Shutdown() did not restore the previous logger")
	}
}

func TestRuntimeReportsSentryEventsLeftUnsent(t *testing.T) {
	reporter, transport := newTestReporter(t)
	transport.stuck = true
	runtime := &Runtime{sentry: reporter}

	if err := runtime.ForceFlush(context.Background()); !errors.Is(err, errSentryUnsent) {
		t.Fatalf("ForceFlush() = %v, want the unsent-events error", err)
	}
	if err := runtime.Shutdown(context.Background()); !errors.Is(err, errSentryUnsent) {
		t.Fatalf("Shutdown() = %v, want the unsent-events error", err)
	}
}

func TestShutdownClosesSentryClientEvenWhenFlushTimesOut(t *testing.T) {
	reporter, transport := newTestReporter(t)
	transport.stuck = true
	logger := slog.New(reporter.Handler())
	runtime := &Runtime{sentry: reporter}

	_ = runtime.Shutdown(context.Background())
	if transport.closed == 0 {
		t.Fatal("Shutdown() must close the Sentry client")
	}
	logger.Error("logged after shutdown") // must not panic on a closed client
}

func TestSentryReporterCloseOfDisabledReporterSucceeds(t *testing.T) {
	var reporter *sentryReporter
	if !reporter.Close(time.Millisecond) {
		t.Fatal("closing a disabled reporter must succeed")
	}
}

func TestSentryHandlerPrefersAttributeNamedError(t *testing.T) {
	reporter, transport := newTestReporter(t)
	slog.New(reporter.Handler()).Error("job failed",
		"cause", errors.New("secondary"), "error", errors.New("primary"))

	events := transport.captured()
	if got := events[0].Exception[len(events[0].Exception)-1].Value; got != "primary" {
		t.Fatalf("exception = %q, want the attribute named error", got)
	}
	if logAttrs(events[0])["cause"] != "secondary" {
		t.Fatalf("other error values must stay in the log context: %#v", logAttrs(events[0]))
	}
}

func TestSentryHandlerFallsBackToFirstErrorValue(t *testing.T) {
	reporter, transport := newTestReporter(t)
	slog.New(reporter.Handler()).Error("job failed",
		"err", errors.New("first"), "cause", errors.New("second"))

	events := transport.captured()
	if got := events[0].Exception[len(events[0].Exception)-1].Value; got != "first" {
		t.Fatalf("exception = %q, want the first error value", got)
	}
}
