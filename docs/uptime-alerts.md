# Scheduler uptime alert

The first uptime-alert slice watches whether the scheduler continues to send
`event = crawl.health` to the configured OpenObserve logs stream. It does not
cover service probes or registered nodes.

## Signal and timing

The `xflickr_crawl_health_stale` alert fires when no `crawl.health` record has
arrived within 90 seconds. `crawl.health.key` records are ignored, so the
scheduler does not need to emit per-key telemetry for this alert to work.

OpenObserve evaluates the query every minute over a two-minute window. With an
on-time evaluation, detection is expected about 90–150 seconds after the last
event, plus scheduler and ingestion delay. This does not promise detection
within exactly 60 seconds.

The signal indicates stale scheduler telemetry. A stopped scheduler, failed
health snapshot, lost telemetry, or delayed OpenObserve ingestion can all cause
it; the alert does not identify which condition occurred.

## Configuration

The provisioner renders alert definitions from
`deploy/openobserve/alerts/`. `OPENOBSERVE_STREAM` selects the stream and
defaults to `xflickr`; `OPENOBSERVE_ALERT_DESTINATION` selects the existing
notification destination and defaults to `xflickr_telegram`. The alert remains
enabled and uses a two-minute period, one-minute frequency, and 15-minute
silence interval.

## Limits

The offline tests verify the 90-second rule and the query's stale-to-fresh
transition. They do not verify OpenObserve SQL parsing or behavior for an
empty/no-data window on the deployed version. See the official
[OpenObserve Alert Library](https://openobserve.ai/docs/user-guide/analytics/alerts/alert-library/)
for its distinction between no data and an alert condition.

When a fresh event arrives, the query no longer matches. Recovery notifications
depend on the OpenObserve version and configuration and were not verified here.
This change updates repository configuration only; it does not modify or test a
live OpenObserve server or notification destination.
