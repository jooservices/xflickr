# Structured logging and telemetry

The application installs a JSON `slog` handler when it starts through
`RunWithHealth`. It writes local logs to stderr and, when OpenObserve username
and URL are configured, forwards logs to OpenTelemetry in batches. Observability
settings load from the optional `.env` file, with process environment values
taking precedence. Traces and metrics use asynchronous OTLP/HTTP exporters as
well. The exporters use
`OPENOBSERVE_URL` as the host, append `/api/<org>/v1/{logs,traces,metrics}`, and
send HTTP Basic authentication plus the configured `stream-name` header.

## Context fields and redaction

`obs.WithFields(ctx, fields)` attaches the supported fields `node`, `role`,
`egress_ip`, `user_id`, `key_id`, and `lane`. The handler adds these fields to
JSON and OpenTelemetry log records. It also adds `trace_id` when the context
contains a valid OpenTelemetry span. Authenticated requests attach the internal
user ID; crawl calls attach their key ID, user ID, and work lane. Callers should
only attach trusted egress IP values, rather than copying untrusted forwarding
headers.

Attribute names containing `secret`, `token`, or `password` are replaced with
`[REDACTED]` in structured records, business events, and log spill output.
Credentials are supplied only in the OTLP Authorization header and are never
included in the configured endpoint URL.

## Business events

Use `obs.Event(ctx, name, attrs)` to write a named structured event and add the
same event to the active trace span. Flickr transport calls emit `flickr.call`
with method, status, latency, response bytes, and classified error code.

## Offline buffering

Logs are exported asynchronously with a 10,000-record SDK queue and 512-record
batches. If the OpenObserve request fails, the exporter keeps the newest 10,000
records in memory and retries them with the next batch. Any records still
unsent at shutdown are appended as JSON Lines to
`OPENOBSERVE_SPILL_PATH` (default `var/obs-spill.jsonl`). The spill directory is
created with mode `0750` and the file with mode `0600`. The application does
not replay spill files automatically; they are retained for operator recovery.

The exporter does not perform a startup health check, so a remote OpenObserve
outage does not block process startup. Metrics and traces use the OpenTelemetry
SDK batch/periodic processors; only log records use the 10,000-record fallback
buffer and shutdown spill file.
