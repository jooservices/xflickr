-- Same KEYS as acquire; ARGV: hourly limit.
local now, _, used, oldest = rollingWindow(KEYS)
for i = used + 1, tonumber(ARGV[1]) do
  local sequence = redis.call('INCR', KEYS[8])
  redis.call('ZADD', KEYS[2], now, tostring(now) .. ':' .. tostring(sequence))
end
redis.call('PEXPIRE', KEYS[2], 7200000)
redis.call('PEXPIRE', KEYS[8], 7200000)
local untilTime = oldest + 3600000
redis.call('SET', KEYS[4], untilTime, 'PX', math.max(1, untilTime - now))
return untilTime - now
