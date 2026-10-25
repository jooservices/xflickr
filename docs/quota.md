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
