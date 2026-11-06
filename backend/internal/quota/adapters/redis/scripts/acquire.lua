-- KEYS: bucket, interactive set, person set, cooldown, photo set, ready, photo demand, sequence
-- ARGV: rate, burst, hourly limit, reserve, person share, class, require ready (1/0), idle photo protection
local now, counts, used, oldest = rollingWindow(KEYS)
local rate = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local hourly = tonumber(ARGV[3])
local reserve = tonumber(ARGV[4])
local personShare = tonumber(ARGV[5])
local class = ARGV[6]
local idlePhotoProtection = tonumber(ARGV[8])

-- Every photo attempt signals demand, including attempts refused below.
if class == 'photo' then
  redis.call('SET', KEYS[7], '1', 'PX', 60000)
end

-- Fail closed until the counters were rebuilt after a Redis loss.
if ARGV[7] == '1' and redis.call('EXISTS', KEYS[6]) == 0 then
  return {0, 1000, 'rebuild'}
end

local cooldown = tonumber(redis.call('GET', KEYS[4]) or '0')
if cooldown > now then
  return {0, cooldown - now, 'cooldown'}
end

local tokenData = redis.call('HMGET', KEYS[1], 'tokens', 'updated', 'second', 'granted')
local tokens = tonumber(tokenData[1])
local updated = tonumber(tokenData[2])
local second = tonumber(tokenData[3])
local granted = tonumber(tokenData[4])
if not tokens then tokens = burst end
if not updated then updated = now end
-- Grants are capped per Redis clock second. Refill stays fractional so the
-- sustained rate does not change, but a full bucket plus refill inside the
-- same second would otherwise allow burst + rate - 1 grants (8 at 4/s, burst 5).
local thisSecond = math.floor(now / 1000)
if second ~= thisSecond then
  second = thisSecond
  granted = 0
end
tokens = math.min(burst, tokens + math.max(0, now - updated) * rate / 1000)
local wait = 0
if granted >= burst then
  wait = math.max(1, (second + 1) * 1000 - now)
end
if tokens < 1 then
  local tokenWait = math.ceil((1 - tokens) * 1000 / rate)
  if tokenWait > wait then wait = tokenWait end
end
if wait > 0 then
  redis.call('HSET', KEYS[1], 'tokens', tokens, 'updated', now, 'second', second, 'granted', granted)
  redis.call('PEXPIRE', KEYS[1], 7200000)
  return {0, wait, 'rate'}
end

-- Preserve the full photo allocation only while demand is recent. Already
-- spent calls stay charged; otherwise unused allocation above the floor is lent.
local ceiling = hourly
local photoShareCap = math.min(math.floor(hourly * (1 - personShare)), math.max(0, hourly - reserve))
local photoUsed = counts[5]
if class == 'photo' then
  ceiling = math.max(0, hourly - reserve)
  if photoUsed >= photoShareCap then ceiling = 0 end
elseif class == 'person' then
  local protected = photoShareCap
  if redis.call('EXISTS', KEYS[7]) == 0 then
    protected = idlePhotoProtection
  end
  ceiling = math.max(0, hourly - reserve) - math.max(0, protected - photoUsed)
end
if used >= ceiling then
  local retry = math.max(1, oldest + 3600000 - now)
  if class == 'person' then
    local demandTTL = redis.call('PTTL', KEYS[7])
    if demandTTL > 0 then retry = math.min(retry, demandTTL) end
  end
  return {0, retry, 'hourly'}
end

redis.call('HSET', KEYS[1], 'tokens', tokens - 1, 'updated', now, 'second', second, 'granted', granted + 1)
redis.call('PEXPIRE', KEYS[1], 7200000)
local lane = laneKey(KEYS, class)
local sequence = redis.call('INCR', KEYS[8])
redis.call('PEXPIRE', KEYS[8], 7200000)
redis.call('ZADD', lane, now, tostring(now) .. ':' .. tostring(sequence))
redis.call('PEXPIRE', lane, 7200000)
redis.call('SET', KEYS[6], 1, 'PX', 7200000)
return {1, 0, 'ok', now}
