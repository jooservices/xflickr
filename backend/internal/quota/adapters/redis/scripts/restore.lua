-- KEYS: hourly counter, window start, per-class counts, ready marker
-- ARGV: window start ms (0 for none), used, then class/count pairs
-- Restores counters only when the ready marker is absent, so a concurrent
-- rebuild or live traffic is never overwritten.
if redis.call('EXISTS', KEYS[4]) == 1 then
  return 0
end
local parts = redis.call('TIME')
local now = tonumber(parts[1]) * 1000 + math.floor(tonumber(parts[2]) / 1000)
local start = tonumber(ARGV[1])
if start > 0 and now < start + 3600000 then
  redis.call('SET', KEYS[2], start, 'PX', 7200000)
  redis.call('SET', KEYS[1], ARGV[2], 'PX', 7200000)
  redis.call('DEL', KEYS[3])
  for i = 3, #ARGV, 2 do
    redis.call('HSET', KEYS[3], ARGV[i], ARGV[i + 1])
  end
  if #ARGV >= 3 then redis.call('PEXPIRE', KEYS[3], 7200000) end
end
redis.call('SET', KEYS[4], 1, 'PX', 7200000)
return 1
