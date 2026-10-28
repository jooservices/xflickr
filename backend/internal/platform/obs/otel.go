package obs

import (
	"context"
	"encoding/base64"
	"errors"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/metric"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultOpenObserveURL    = "http://127.0.0.1:15080"
	defaultOpenObserveOrg    = "default"
	defaultOpenObserveStream = "xflickr"
	defaultSpillPath         = "var/obs-spill.jsonl"
	batchSize                = 512
	queueSize                = defaultBufferSize
)

var environmentKeys = []string{
	"OPENOBSERVE_URL",
	"OPENOBSERVE_ORG",
	"OPENOBSERVE_STREAM",
	"OPENOBSERVE_USERNAME",
	"OPENOBSERVE_PASSWORD",
	"OPENOBSERVE_SPILL_PATH",
	"NODE_ID",
	"XFLICKR_VERSION",
	"LOG_LEVEL",
}

// Config contains OpenObserve export settings and process identity.
type Config struct {
	URL       string
	Org       string
	Stream    string
	Username  string
	Password  string
	Node      string
	Version   string
	LogLevel  slog.Level
	SpillPath string
}

// ConfigFromEnvironment reads the observability-related environment variables.
func ConfigFromEnvironment() Config {
	return configFromValues(processEnvironmentValues())
}

// ConfigFromEnvironmentWithDotEnv reads observability settings from an
// optional dotenv file, then overlays process environment values.
func ConfigFromEnvironmentWithDotEnv(path string) (Config, error) {
	values := make(map[string]string, len(environmentKeys))
	if path != "" {
		fileValues, err := godotenv.Read(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return Config{}, errors.New("read observability dotenv configuration")
		}
		for _, key := range environmentKeys {
			if value, ok := fileValues[key]; ok {
				values[key] = value
			}
		}
	}
	for key, value := range processEnvironmentValues() {
		values[key] = value
	}
	return configFromValues(values), nil
}

func processEnvironmentValues() map[string]string {
	values := make(map[string]string, len(environmentKeys))
	for _, key := range environmentKeys {
		if value, ok := os.LookupEnv(key); ok {
			values[key] = value
		}
	}
	return values
}

func configFromValues(values map[string]string) Config {
	value := func(key string) string { return values[key] }
	cfg := Config{
		URL:       strings.TrimSpace(value("OPENOBSERVE_URL")),
		Org:       strings.TrimSpace(value("OPENOBSERVE_ORG")),
		Stream:    strings.TrimSpace(value("OPENOBSERVE_STREAM")),
		Username:  value("OPENOBSERVE_USERNAME"),
		Password:  value("OPENOBSERVE_PASSWORD"),
		Node:      strings.TrimSpace(value("NODE_ID")),
		Version:   strings.TrimSpace(value("XFLICKR_VERSION")),
		SpillPath: strings.TrimSpace(value("OPENOBSERVE_SPILL_PATH")),
	}
	if cfg.URL == "" {
		cfg.URL = defaultOpenObserveURL
	}
	if cfg.Org == "" {
		cfg.Org = defaultOpenObserveOrg
	}
	if cfg.Stream == "" {
		cfg.Stream = defaultOpenObserveStream
	}
	if cfg.SpillPath == "" {
		cfg.SpillPath = defaultSpillPath
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(strings.TrimSpace(value("LOG_LEVEL")))); err == nil {
		cfg.LogLevel = level
	}
	return cfg
}

// Runtime owns the process-wide slog and OpenTelemetry providers.
type Runtime struct {
	mu sync.Mutex

	loggerProvider         *sdklog.LoggerProvider
	traceProvider          *sdktrace.TracerProvider
	metricProvider         *sdkmetric.MeterProvider
	logger                 *slog.Logger
	previousLogger         *slog.Logger
	previousLoggerProvider otellog.LoggerProvider
	previousTraceProvider  trace.TracerProvider
	previousMetricProvider metric.MeterProvider
	installed              bool
	otelInstalled          bool
	shutdown               bool
}

// Start installs JSON slog output and, when OpenObserve credentials are
// configured, asynchronous OTLP exporters for logs, traces, and metrics. A
// remote service outage does not prevent startup.
func Start(cfg Config) (*Runtime, error) {
	previousLogger := slog.Default()
	runtime := &Runtime{
		previousLogger:         previousLogger,
		previousLoggerProvider: otel.GetLoggerProvider(),
		previousTraceProvider:  otel.GetTracerProvider(),
		previousMetricProvider: otel.GetMeterProvider(),
		logger:                 slog.New(NewHandler(os.Stderr, cfg.LogLevel, nil)),
	}
	if cfg.LogLevel == 0 {
		runtime.logger = slog.New(NewHandler(os.Stderr, slog.LevelInfo, nil))
	}
	if cfg.Username == "" || strings.TrimSpace(cfg.URL) == "" {
		slog.SetDefault(runtime.logger)
		runtime.installed = true
		return runtime, nil
	}

	baseEndpoint, err := endpointURL(cfg, "logs")
	if err != nil {
		slog.SetDefault(runtime.logger)
		runtime.installed = true
		return runtime, err
	}
	traceEndpoint, err := endpointURL(cfg, "traces")
	if err != nil {
		slog.SetDefault(runtime.logger)
		runtime.installed = true
		return runtime, err
	}
	metricEndpoint, err := endpointURL(cfg, "metrics")
	if err != nil {
		slog.SetDefault(runtime.logger)
		runtime.installed = true
		return runtime, err
	}
	headerValues := map[string]string{
		"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(cfg.Username+":"+cfg.Password)),
		"stream-name":   cfg.Stream,
	}

	initCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	logExporter, err := otlploghttp.New(initCtx,
		otlploghttp.WithEndpointURL(baseEndpoint),
		otlploghttp.WithHeaders(headerValues),
		otlploghttp.WithTimeout(5*time.Second),
	)
	if err != nil {
		slog.SetDefault(runtime.logger)
		runtime.installed = true
		return runtime, errors.New("initialize OpenObserve log exporter")
	}
	traceExporter, err := otlptracehttp.New(initCtx,
		otlptracehttp.WithEndpointURL(traceEndpoint),
		otlptracehttp.WithHeaders(headerValues),
		otlptracehttp.WithTimeout(5*time.Second),
	)
	if err != nil {
		_ = logExporter.Shutdown(context.Background())
		slog.SetDefault(runtime.logger)
		runtime.installed = true
		return runtime, errors.New("initialize OpenObserve trace exporter")
	}
	metricExporter, err := otlpmetrichttp.New(initCtx,
		otlpmetrichttp.WithEndpointURL(metricEndpoint),
		otlpmetrichttp.WithHeaders(headerValues),
		otlpmetrichttp.WithTimeout(5*time.Second),
	)
	if err != nil {
		_ = logExporter.Shutdown(context.Background())
		_ = traceExporter.Shutdown(context.Background())
		slog.SetDefault(runtime.logger)
		runtime.installed = true
		return runtime, errors.New("initialize OpenObserve metric exporter")
	}

	resourceAttrs := []attribute.KeyValue{attribute.String("service.name", "xflickr")}
	if cfg.Version != "" {
		resourceAttrs = append(resourceAttrs, attribute.String("service.version", cfg.Version))
	}
	if cfg.Node != "" {
		resourceAttrs = append(resourceAttrs, attribute.String("node", cfg.Node))
	}
	telemetryResource := resource.NewSchemaless(resourceAttrs...)
	runtime.traceProvider = sdktrace.NewTracerProvider(
		sdktrace.WithResource(telemetryResource),
		sdktrace.WithBatcher(traceExporter,
			sdktrace.WithMaxQueueSize(queueSize),
			sdktrace.WithMaxExportBatchSize(batchSize),
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithExportTimeout(5*time.Second),
		),
	)
	runtime.metricProvider = sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(telemetryResource),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter,
			sdkmetric.WithInterval(30*time.Second),
			sdkmetric.WithTimeout(5*time.Second),
		)),
	)
	bufferedExporter := newBufferedLogExporter(logExporter, cfg.SpillPath, queueSize)
	runtime.loggerProvider = sdklog.NewLoggerProvider(
		sdklog.WithResource(telemetryResource),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(bufferedExporter,
			sdklog.WithMaxQueueSize(queueSize),
			sdklog.WithExportMaxBatchSize(batchSize),
			sdklog.WithExportInterval(5*time.Second),
			sdklog.WithExportTimeout(5*time.Second),
		)),
	)

	otel.SetTracerProvider(runtime.traceProvider)
	otel.SetMeterProvider(runtime.metricProvider)
	otel.SetLoggerProvider(runtime.loggerProvider)
	runtime.otelInstalled = true
	bridge := otelslog.NewHandler("xflickr", otelslog.WithLoggerProvider(runtime.loggerProvider))
	runtime.logger = slog.New(NewHandler(os.Stderr, cfg.LogLevel, bridge))
	slog.SetDefault(runtime.logger)
	runtime.installed = true
	return runtime, nil
}

// ForceFlush asks all configured providers to export buffered telemetry.
func (r *Runtime) ForceFlush(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var failures []error
	if r.traceProvider != nil {
		if err := r.traceProvider.ForceFlush(ctx); err != nil {
			failures = append(failures, err)
		}
	}
	if r.metricProvider != nil {
		if err := r.metricProvider.ForceFlush(ctx); err != nil {
			failures = append(failures, err)
		}
	}
	if r.loggerProvider != nil {
		if err := r.loggerProvider.ForceFlush(ctx); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// Shutdown flushes providers, spills unsent log records, and restores globals.
func (r *Runtime) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	if r.shutdown {
		r.mu.Unlock()
		return nil
	}
	r.shutdown = true
	r.mu.Unlock()

	if r.traceProvider != nil {
		_ = r.traceProvider.Shutdown(ctx)
	}
	if r.metricProvider != nil {
		_ = r.metricProvider.Shutdown(ctx)
	}
	var failures []error
	if r.loggerProvider != nil {
		if err := r.loggerProvider.Shutdown(ctx); err != nil {
			failures = append(failures, err)
		}
	}
	if r.installed {
		if slog.Default() == r.logger {
			slog.SetDefault(r.previousLogger)
		}
		if r.otelInstalled {
			otel.SetTracerProvider(r.previousTraceProvider)
			otel.SetMeterProvider(r.previousMetricProvider)
			otel.SetLoggerProvider(r.previousLoggerProvider)
		}
	}
	return errors.Join(failures...)
}

func endpointURL(cfg Config, signal string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(cfg.URL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("OPENOBSERVE_URL must be an absolute http(s) URL without credentials, query, or fragment")
	}
	org := strings.TrimSpace(cfg.Org)
	if org == "" || strings.ContainsAny(org, "/\\?#") {
		return "", errors.New("OPENOBSERVE_ORG must be a non-empty path segment")
	}
	basePath := strings.TrimRight(parsed.Path, "/")
	orgPath := "/api/" + org
	if !strings.HasSuffix(basePath, orgPath) {
		basePath += orgPath
	}
	parsed.Path = strings.TrimRight(basePath, "/") + "/v1/" + signal
	parsed.RawPath = ""
	return parsed.String(), nil
}
