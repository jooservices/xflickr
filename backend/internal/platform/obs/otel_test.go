package obs

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/metric"
	collectorlog "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestStartExportsLogsTracesAndMetricsToFakeOTLPHTTP(t *testing.T) {
	type observedRequest struct {
		path          string
		authorization string
		stream        string
		contentType   string
		bodyBytes     int
		body          []byte
	}
	var mu sync.Mutex
	var requests []observedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requestBody, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read fake OTLP request: %v", err)
		}
		mu.Lock()
		requests = append(requests, observedRequest{
			path: request.URL.Path, authorization: request.Header.Get("Authorization"),
			stream: request.Header.Get("stream-name"), contentType: request.Header.Get("Content-Type"),
			bodyBytes: len(requestBody), body: requestBody,
		})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
		var response proto.Message
		switch {
		case strings.HasSuffix(request.URL.Path, "/v1/logs"):
			response = &collectorlog.ExportLogsServiceResponse{}
		case strings.HasSuffix(request.URL.Path, "/v1/traces"):
			response = &collectortrace.ExportTraceServiceResponse{}
		case strings.HasSuffix(request.URL.Path, "/v1/metrics"):
			response = &collectormetric.ExportMetricsServiceResponse{}
		default:
			http.NotFound(w, request)
			return
		}
		body, err := proto.Marshal(response)
		if err != nil {
			t.Errorf("marshal fake OTLP response: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	previousLogger := slog.Default()
	runtime, err := Start(Config{
		URL:       server.URL,
		Org:       "test-org",
		Stream:    "xflickr-tests",
		Username:  "test-user",
		Password:  "test-password",
		Node:      "node-test",
		Version:   "test-version",
		LogLevel:  slog.LevelDebug,
		SpillPath: filepath.Join(t.TempDir(), "spill.jsonl"),
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := runtime.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown() error = %v", err)
		}
		if slog.Default() != previousLogger {
			t.Error("Shutdown() did not restore the previous slog logger")
		}
	})

	ctx, span := otel.Tracer("obs-test").Start(context.Background(), "integration-span")
	ctx = WithFields(ctx, map[string]any{"role": "test", "user_id": int64(7)})
	Event(ctx, "business.test", map[string]any{"count": int64(3), "password": "must-not-export-event"})
	slog.Default().InfoContext(ctx, "fake exporter integration")
	var directRecord otellog.Record
	directRecord.SetBody(attribute.StringValue("direct telemetry event"))
	directRecord.AddAttributes(attribute.String("api_token", "must-not-export-otlp"))
	otel.GetLoggerProvider().Logger("obs-test").Emit(ctx, directRecord)
	counter, err := otel.Meter("obs-test").Int64Counter("obs.test.counter")
	if err != nil {
		t.Fatalf("create counter: %v", err)
	}
	counter.Add(ctx, 1, metric.WithAttributes(attribute.String("case", "integration")))
	span.End()

	flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := runtime.ForceFlush(flushCtx); err != nil {
		t.Fatalf("ForceFlush() error = %v", err)
	}

	mu.Lock()
	gotRequests := append([]observedRequest(nil), requests...)
	mu.Unlock()
	wantPaths := map[string]bool{
		"/api/test-org/v1/logs":    false,
		"/api/test-org/v1/traces":  false,
		"/api/test-org/v1/metrics": false,
	}
	wantAuthorization := "Basic " + base64.StdEncoding.EncodeToString([]byte("test-user:test-password"))
	for _, request := range gotRequests {
		if _, ok := wantPaths[request.path]; ok {
			wantPaths[request.path] = true
		}
		if request.authorization != wantAuthorization || request.stream != "xflickr-tests" {
			t.Errorf("OTLP headers = (%q, %q), want configured basic auth and stream", request.authorization, request.stream)
		}
		if request.contentType != "application/x-protobuf" || request.bodyBytes == 0 {
			t.Errorf("OTLP request content-type/body = (%q, %d)", request.contentType, request.bodyBytes)
		}
		if strings.HasSuffix(request.path, "/v1/logs") && (bytes.Contains(request.body, []byte("must-not-export-event")) || bytes.Contains(request.body, []byte("must-not-export-otlp"))) {
			t.Error("OTLP log payload contains an unredacted sensitive attribute")
		}
	}
	if diff := cmp.Diff(map[string]bool{
		"/api/test-org/v1/logs": true, "/api/test-org/v1/traces": true, "/api/test-org/v1/metrics": true,
	}, wantPaths); diff != "" {
		t.Fatalf("exported OTLP paths mismatch (-want +got):\n%s\nrequests: %#v", diff, gotRequests)
	}
}

func TestEndpointURLRejectsCredentialBearingURLWithoutLeakingIt(t *testing.T) {
	_, err := endpointURL(Config{URL: "https://user:private@example.test?token=secret", Org: "xflickr"}, "logs")
	if err == nil {
		t.Fatal("endpointURL() accepted URL credentials and query")
	}
	if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("endpoint error leaked URL secrets: %v", err)
	}
}

func TestEndpointURLPreservesBasePathAndRejectsInvalidOrganization(t *testing.T) {
	got, err := endpointURL(Config{URL: "https://observe.example/prefix/api/my-org", Org: "my-org"}, "traces")
	if err != nil || got != "https://observe.example/prefix/api/my-org/v1/traces" {
		t.Fatalf("endpointURL() = %q, %v", got, err)
	}
	if _, err := endpointURL(Config{URL: "https://observe.example", Org: "bad/org"}, "logs"); err == nil {
		t.Fatal("endpointURL() accepted an organization with a path separator")
	}
}

func TestStartWithoutCredentialsKeepsJSONLoggingAndShutdownSafe(t *testing.T) {
	for _, key := range []string{
		"OPENOBSERVE_URL", "OPENOBSERVE_ORG", "OPENOBSERVE_STREAM", "OPENOBSERVE_USERNAME",
		"OPENOBSERVE_PASSWORD", "OPENOBSERVE_SPILL_PATH", "NODE_ID", "XFLICKR_VERSION", "LOG_LEVEL",
	} {
		t.Setenv(key, "")
	}
	previous := slog.Default()
	cfg := ConfigFromEnvironment()
	if cfg.URL != defaultOpenObserveURL || cfg.Org != defaultOpenObserveOrg || cfg.Stream != defaultOpenObserveStream || cfg.SpillPath != defaultSpillPath || cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("default config = %#v", cfg)
	}
	runtime, err := Start(cfg)
	if err != nil {
		t.Fatalf("Start() without credentials = %v", err)
	}
	if err := runtime.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush() without exporters = %v", err)
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() without exporters = %v", err)
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown() = %v", err)
	}
	if slog.Default() != previous {
		t.Fatal("Shutdown() did not restore the previous logger")
	}
	var nilRuntime *Runtime
	if err := nilRuntime.ForceFlush(context.Background()); err != nil {
		t.Fatalf("nil Runtime.ForceFlush() = %v", err)
	}
	if err := nilRuntime.Shutdown(context.Background()); err != nil {
		t.Fatalf("nil Runtime.Shutdown() = %v", err)
	}
}

func TestStartInvalidEndpointFallsBackWithoutLeakingURL(t *testing.T) {
	previous := slog.Default()
	runtime, err := Start(Config{
		URL:      "https://user:secret@example.test?token=private",
		Org:      "test-org",
		Username: "test-user",
		Password: "test-password",
	})
	if err == nil || runtime == nil {
		t.Fatalf("Start() = (%v, %v), want fallback runtime and validation error", runtime, err)
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private") {
		t.Fatalf("Start() error leaked URL secrets: %v", err)
	}
	if shutdownErr := runtime.Shutdown(context.Background()); shutdownErr != nil {
		t.Fatalf("fallback Runtime.Shutdown() = %v", shutdownErr)
	}
	if slog.Default() != previous {
		t.Fatal("Shutdown() did not restore the previous logger")
	}
}

func TestOpenObserveFailureKeepsWorkingAndSpillsLogOnShutdown(t *testing.T) {
	var mu sync.Mutex
	var logRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/v1/logs") {
			mu.Lock()
			logRequests++
			mu.Unlock()
			http.Error(w, "receiver unavailable", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		var response proto.Message
		switch {
		case strings.HasSuffix(request.URL.Path, "/v1/traces"):
			response = &collectortrace.ExportTraceServiceResponse{}
		case strings.HasSuffix(request.URL.Path, "/v1/metrics"):
			response = &collectormetric.ExportMetricsServiceResponse{}
		default:
			http.NotFound(w, request)
			return
		}
		body, err := proto.Marshal(response)
		if err != nil {
			t.Errorf("marshal fake OTLP response: %v", err)
			http.Error(w, "invalid fixture", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	previousLogger := slog.Default()
	spillPath := filepath.Join(t.TempDir(), "offline", "obs-spill.jsonl")
	runtime, err := Start(Config{
		URL: server.URL, Org: "offline-org", Stream: "xflickr-offline",
		Username: "test-user", Password: "test-password", LogLevel: slog.LevelDebug,
		SpillPath: spillPath,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := runtime.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown() error = %v", err)
		}
		if slog.Default() != previousLogger {
			t.Error("Shutdown() did not restore the previous slog logger")
		}
	})

	slog.Default().InfoContext(context.Background(), "offline business event", "api_token", "do-not-spill")
	flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := runtime.ForceFlush(flushCtx); err != nil {
		t.Fatalf("ForceFlush() failed while receiver returned 400: %v", err)
	}
	cancel()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	if err := runtime.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() failed after spilling: %v", err)
	}
	cancelShutdown()

	contents, err := os.ReadFile(spillPath)
	if err != nil {
		t.Fatalf("ReadFile(spill): %v", err)
	}
	if got := string(contents); !strings.Contains(got, "offline business event") || !strings.Contains(got, `"api_token":"[REDACTED]"`) || strings.Contains(got, "do-not-spill") {
		t.Fatalf("offline spill does not contain the redacted log: %s", got)
	}
	mu.Lock()
	gotLogRequests := logRequests
	mu.Unlock()
	if gotLogRequests < 2 {
		t.Fatalf("failed log export attempts = %d, want a retry before spill", gotLogRequests)
	}
}

func TestConfigFromEnvironment(t *testing.T) {
	t.Setenv("OPENOBSERVE_URL", "http://localhost:15080")
	t.Setenv("OPENOBSERVE_ORG", "test-org")
	t.Setenv("OPENOBSERVE_STREAM", "test-stream")
	t.Setenv("OPENOBSERVE_USERNAME", "user")
	t.Setenv("OPENOBSERVE_PASSWORD", "password")
	t.Setenv("OPENOBSERVE_SPILL_PATH", "tmp/obs.jsonl")
	t.Setenv("NODE_ID", "node-a")
	t.Setenv("LOG_LEVEL", "debug")
	cfg := ConfigFromEnvironment()
	if cfg.URL != "http://localhost:15080" || cfg.Org != "test-org" || cfg.Stream != "test-stream" || cfg.Username != "user" || cfg.Password != "password" || cfg.SpillPath != "tmp/obs.jsonl" || cfg.Node != "node-a" || cfg.LogLevel != slog.LevelDebug {
		t.Fatal("ConfigFromEnvironment() did not load the configured values")
	}
}

func TestConfigFromEnvironmentWithDotEnvAndProcessPrecedence(t *testing.T) {
	for _, key := range environmentKeys {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}
	path := filepath.Join(t.TempDir(), ".env")
	contents := strings.Join([]string{
		"OPENOBSERVE_URL=https://observe.example",
		"OPENOBSERVE_ORG=dotenv-org",
		"OPENOBSERVE_STREAM=dotenv-stream",
		"OPENOBSERVE_USERNAME=dotenv-user",
		"OPENOBSERVE_PASSWORD=dotenv-password",
		"OPENOBSERVE_SPILL_PATH=var/dotenv-spill.jsonl",
		"NODE_ID=dotenv-node",
		"LOG_LEVEL=debug",
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write dotenv fixture: %v", err)
	}
	t.Setenv("OPENOBSERVE_USERNAME", "process-user")
	t.Setenv("NODE_ID", "process-node")

	cfg, err := ConfigFromEnvironmentWithDotEnv(path)
	if err != nil {
		t.Fatalf("ConfigFromEnvironmentWithDotEnv() error = %v", err)
	}
	if cfg.URL != "https://observe.example" || cfg.Org != "dotenv-org" || cfg.Stream != "dotenv-stream" || cfg.SpillPath != "var/dotenv-spill.jsonl" || cfg.LogLevel != slog.LevelDebug {
		t.Fatal("dotenv observability settings were not loaded")
	}
	if cfg.Username != "process-user" || cfg.Node != "process-node" {
		t.Fatal("process environment did not override dotenv values")
	}
	if cfg.Password != "dotenv-password" {
		t.Fatal("dotenv password was not loaded")
	}
}

func TestConfigFromEnvironmentWithDotEnvAllowsMissingOptionalFile(t *testing.T) {
	for _, key := range environmentKeys {
		t.Setenv(key, "")
	}
	cfg, err := ConfigFromEnvironmentWithDotEnv(filepath.Join(t.TempDir(), "missing.env"))
	if err != nil {
		t.Fatalf("ConfigFromEnvironmentWithDotEnv() with missing file error = %v", err)
	}
	if cfg.URL != defaultOpenObserveURL || cfg.Org != defaultOpenObserveOrg || cfg.Stream != defaultOpenObserveStream || cfg.SpillPath != defaultSpillPath || cfg.LogLevel != slog.LevelInfo {
		t.Fatal("missing optional dotenv file did not use observability defaults")
	}
}
