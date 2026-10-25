-- KEYS: token bucket, hourly counter, window start, cooldown
-- ARGV: rate, burst, hourly limit, reserve, person share, class
local nowParts = redis.call('TIME')
local now = tonumber(nowParts[1]) * 1000 + math.floor(tonumber(nowParts[2]) / 1000)
local rate = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local hourly = tonumber(ARGV[3])
local reserve = tonumber(ARGV[4])
local personShare = tonumber(ARGV[5])
local class = ARGV[6]

local cooldown = tonumber(redis.call('GET', KEYS[4]) or '0')
if cooldown > now then
  return {0, cooldown - now, 'cooldown'}
end

local start = tonumber(redis.call('GET', KEYS[3]) or '0')
local used = tonumber(redis.call('GET', KEYS[2]) or '0')
if start == 0 or now >= start + 3600000 then
  start = now
  used = 0
  redis.call('SET', KEYS[3], start, 'PX', 7200000)
  redis.call('SET', KEYS[2], used, 'PX', 7200000)
  redis.call('DEL', KEYS[4])
end

local tokenData = redis.call('HMGET', KEYS[1], 'tokens', 'updated')
local tokens = tonumber(tokenData[1])
local updated = tonumber(tokenData[2])
if not tokens then tokens = burst end
if not updated then updated = now end
tokens = math.min(burst, tokens + math.max(0, now - updated) * rate / 1000)
if tokens < 1 then
  local wait = math.ceil((1 - tokens) * 1000 / rate)
  redis.call('HSET', KEYS[1], 'tokens', tokens, 'updated', now)
  redis.call('PEXPIRE', KEYS[1], 7200000)
  return {0, wait, 'rate'}
end

local ceiling = hourly
if class == 'photo' then
  ceiling = math.min(math.floor(hourly * (1 - personShare)), math.max(0, hourly - reserve))
elseif class == 'person' then
  ceiling = math.max(0, hourly - reserve)
end
if used >= ceiling then
  return {0, math.max(1, start + 3600000 - now), 'hourly'}
end

redis.call('HSET', KEYS[1], 'tokens', tokens - 1, 'updated', now)
redis.call('PEXPIRE', KEYS[1], 7200000)
redis.call('SET', KEYS[2], used + 1, 'PX', 7200000)
return {1, 0, 'ok'}
