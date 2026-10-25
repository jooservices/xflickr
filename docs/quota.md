# Quota governor

The quota governor acquires a Redis-backed permit before a Flickr call. Each
API key has an atomic token bucket for its per-second rate and an hourly
counter whose one-hour window starts with the first acquisition in that window.
The acquisition Lua script checks both limits and changes their state in one
Redis call. The Go adapter uses go-redis script SHA caching and fails closed
when Redis or the key budget provider is unavailable.

Interactive calls may use the full hourly budget. Person and photo calls leave
the configured reserve available for interactive calls. Photo calls also stop
at `hourly_limit * (1 - person_lane_share)`, preserving the person lane's share
of the hourly capacity. The default person lane share is 30 percent.

Callers finish a permit with its result metadata. A Flickr quota-exceeded
result recalibrates the hourly counter to the key's hourly limit and places the
key in cooldown until the current hourly window ends. The counter and cooldown
are scoped by API key.

## Usage persistence and rebuild

A flusher copies each valid key's hourly usage from Redis into
`quota_usage(key_id, window_start, used, by_class)` every minute. A save never
lowers `used` for a window, and keys whose Redis counters are missing are
skipped so lost counters never overwrite stored usage. The governor counts
granted calls per class (`byclass` hash) so `by_class` can be stored.

When a governor is configured with a rebuilder, each key has a `ready` marker
in Redis. If the marker is missing (at start or after Redis lost its keys),
`Acquire` fails closed: it blocks while the counters are rebuilt, then retries
once. If the rebuild fails it returns the governor-offline error; if the key
still is not ready it returns a retry delay. Concurrent acquisitions for one key
share a single rebuild.

The rebuild reads the newest `quota_usage` window that started within the last
hour and the `api_call_log` calls since that window began. Per class it takes
the larger of the two, so it can over-count the window but never under-count
it. With no stored row, the earliest logged call starts the window.

`api_call_log(key_id, class, called_at)` is created by the
`api_call_log` migration. The governor writes one row for every granted call
(when a recorder is configured) before returning the permit; if the write
fails, `Acquire` returns the governor-offline error. The flusher deletes rows
older than two windows after each flush. A missing table is an error, so a
rebuild fails closed instead of treating the log as empty. Calls made after the
last flush and before a Redis loss are therefore recovered from the call log.

The scheduler role builds one rebuilder, rebuilds every tracked key at start,
and runs the flusher in the background until shutdown, when it flushes a last
time. Failures of the start rebuild or of a flush are logged; `Acquire` keeps
failing closed for any key that is not ready.
