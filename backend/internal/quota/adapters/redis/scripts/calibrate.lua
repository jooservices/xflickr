-- KEYS: hourly counter, window start, cooldown
-- ARGV: hourly limit
local parts = redis.call('TIME')
local now = tonumber(parts[1]) * 1000 + math.floor(tonumber(parts[2]) / 1000)
local start = tonumber(redis.call('GET', KEYS[2]) or '0')
if start == 0 or now >= start + 3600000 then
  return 0
end
local windowEnd = start + 3600000
redis.call('SET', KEYS[1], ARGV[1], 'PX', math.max(1, windowEnd - now))
redis.call('SET', KEYS[3], windowEnd, 'PX', math.max(1, windowEnd - now))
return windowEnd - now
