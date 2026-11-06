# Quota governor

The quota governor acquires a Redis-backed permit before a Flickr call. Each
API key has an atomic token bucket for its per-second rate and an hourly
rolling 60-minute ledger. Three sorted sets per key store interactive, person
and photo grants, scored by Redis grant time in milliseconds. Each member uses
the time plus a per-key sequence for uniqueness. At acquisition, grants at or
before `now - 3,600,000 ms` are removed; usage is the sum of the three set sizes.
Each granted lane set and the sequence expire after two hours.
The acquisition Lua script checks both limits and changes their state in one
Redis call. The Go adapter uses go-redis script SHA caching and fails closed
when Redis or the key budget provider is unavailable.

The bucket refills at the key's per-second rate and never holds more tokens
than the key's burst. One Redis clock second also never grants more calls than
that burst. Refill during the same second used to be spendable immediately, so
a full bucket plus the rest of the second granted `burst + per_second_rate - 1`
calls (8 when the rate is 4 and the burst is 5). Those refilled tokens now stay
in the bucket until a later second. The sustained rate is unchanged, and neither
configured limit is raised. The script reads Redis `TIME`, so every process
shares one clock.

Every role acquires against the same key, `{prefix}quota:key:{id}:bucket`.
The crawl, scheduler, API, and media roles do not keep separate buckets for
one API key. A different API key has its own bucket.

Interactive calls may use the full hourly budget. Person and photo calls leave
the configured reserve available for interactive calls. Photo calls also stop
at `hourly_limit * (1 - person_lane_share)`, preserving the person lane's share
of the hourly capacity. The default `person_lane_share` is 0.30 (30 percent).

The person lane leaves the photo lane's full configured share available only
while a photo-class acquisition was attempted in the last 60 seconds. Every
photo attempt refreshes a per-key Redis demand marker atomically in the acquire
script, including attempts denied by rate, quota, cooldown or rebuild readiness.
When that marker expires, the person lane leaves only `photo_lane_floor_share`
(default 0.10, range 0–0.5) available. Administrators edit the floor in Admin
Defaults; users cannot override it. Both shares are fractions of the hourly
limit, with background calls always leaving the interactive reserve intact.

For a photo allocation `P`, granted photo calls `U`, hourly limit `H` and
reserve `R`, the person ceiling is `max(0, H - R) - max(0, P - U)`.
With recent photo demand, `P = min(floor(H * (1 - person_lane_share)),
max(0, H - R))`; otherwise `P` is the smaller of that allocation and
`floor(H * photo_lane_floor_share)`. The idle protection never exceeds the
photo allocation: at person share 0.75, floor share 0.5, hourly limit 3,600
and reserve 100, it protects 900 calls and the idle person ceiling is 2,600.
At defaults (3,600 hourly, 100 reserve, 10 percent floor), an idle photo lane
lets person work use 3,140 calls per hour. The simulated-hour acceptance target
is at least 95 percent of this computed ceiling. Person callers blocked by
recent photo demand retry no later than the marker's expiry.

Returning photo work can use the unspent floor immediately, subject to the
unchanged shared rate and clock-second burst cap. Already granted calls are
never revoked; renewed demand protects the full remaining photo share from
further person calls, but cannot recover capacity already spent. Interactive
calls continue to use the full hourly budget.

Deployment: run migrations and restart roles. The lead restores
`person_lane_share` to 0.30 after deployment; the migration does not change it.

Callers finish a permit with its result metadata. A Flickr quota-exceeded
result fills the rolling ledger to the key's hourly limit with synthetic
interactive grants scored at Redis now, without lowering existing counts. The
key cools down until the oldest counted grant expires; remaining synthetic
grants continue consuming budget until their own expiry. The counter and cooldown
are scoped by API key.

## Usage persistence and rebuild

A flusher copies each valid key's hourly usage from Redis into
`quota_usage(key_id, window_start, used, by_class)` every minute. A save never
lowers `used` for a window, and keys whose Redis counters are missing are
skipped so lost counters never overwrite stored usage. The per-class sorted set sizes supply `by_class`. Each snapshot
uses `window_start = Redis now - 60 minutes`; it describes that rolling
interval, not a new fixed window. Counts may decrease between snapshots as
grants expire; the existing upsert still never lowers one snapshot row.

When a governor is configured with a rebuilder, each key has a versioned `rolling:ready` marker
in Redis. If the marker is missing (at start or after Redis lost its keys),
`Acquire` fails closed: it blocks while the counters are rebuilt, then retries
once. If the rebuild fails it returns the governor-offline error; if the key
still is not ready it returns a retry delay. Concurrent acquisitions for one key
share a single rebuild.

The rebuild reads individual `api_call_log` grant rows in the last 60 minutes
with `outcome IS NULL`, retaining their exact timestamps and classes. Diagnostic
outcome rows do not consume a second permit. Stable log member identities make
replay idempotent. Restore retains existing sorted-set members and never lowers
counts. Aggregate `quota_usage` snapshots remain available to history readers;
they are not replayed because they cannot identify when individual calls expire.

The versioned marker is absent on first start after deployment, so the existing
fail-closed path rebuilds before granting. Restart all roles together; mixing
fixed-window and rolling-window governors is unsupported. No schema migration
or settings change is needed for this update.

`api_call_log(key_id, class, called_at)` is created by the
`api_call_log` migration. The governor writes one row for every granted call
(when a recorder is configured) before returning the permit; if the write
fails, `Acquire` returns the governor-offline error. The flusher deletes rows
older than two windows after each flush. A missing table is an error, so a
rebuild fails closed instead of treating the log as empty. Calls made after the
last flush and before a Redis loss are therefore recovered from the call log.

The initial `flickr.test.echo` used to validate a new public API key is logged
with `key_id IS NULL` because its key row does not exist yet. It has no quota
class and is excluded from per-key recovery and rollups.

The scheduler role builds one rebuilder, rebuilds every tracked key at start,
and runs the flusher in the background until shutdown, when it flushes a last
time. Failures of the start rebuild or of a flush are logged; `Acquire` keeps
failing closed for any key that is not ready.

## Quota state endpoints and events

`GET /keys/{id}/quota` returns the signed-in owner's key state: `used`,
`limit`, `reserve`, `reset_at` (expiry of the oldest counted grant, omitted when the
rolling ledger is empty), `state` and `lanes` (granted calls per interactive, person and
photo lane). A key that does not exist, is not valid, or belongs to someone else
is a 404.

`GET /admin/quota` lists every valid key with its owner, `node`, `state` and
`trend`. It sits under `/admin`, so the identity middleware applies
`RequireAdmin`. `node` is `NODE_ID` (the host name when unset).

`state` is `cooling` while the key's cooldown marker is set (Flickr rejected it
and it waits for the oldest counted grant to expire), `exhausted` when `used` reached `limit`,
and `ok` otherwise. Expired grants no longer count, including when the endpoint is read without
an intervening acquisition. `trend`
compares `used` with the previous stored rolling snapshot at the same
full-hour duration: `rising` or `falling` beyond a 10 percent tolerance, otherwise `steady`.
Without a previous window any usage is `rising`.

The scheduler role emits a `quota.state` event per valid key every minute to
OpenObserve (`/api/{org}/{stream}/_json`): `key_id`, `used`, `limit`, `reserve`,
`reset_at`, `state`, `trend`, `lane_interactive`, `lane_person`, `lane_photo` and
`node`. Emission is off until `OPENOBSERVE_USERNAME` is set. A failed emit is
logged and retried on the next tick.

## Rolling verification

Regression tests spend late in an hour and immediately after the old boundary,
assert the oldest expiry retry and endpoint reset, and simulate 24 hours of
waiting work with at least 95 percent utilization of the configured allowance.
Existing lane-demand, idle-floor and real-Redis clock-second tests remain gates.
After deployment, the lead should inspect a full day of grant log rows
(`outcome IS NULL`) per key with a rolling 60-minute count and verify none
exceeds the configured limit (3,600 at defaults). Pre-deployment simulations do
not establish that live observation.
