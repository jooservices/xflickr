-- Shared by the atomic governor, snapshot, calibration and restore scripts.
local function redisNow()
  local parts = redis.call('TIME')
  return tonumber(parts[1]) * 1000 + math.floor(tonumber(parts[2]) / 1000)
end

local function laneKey(keys, class)
  if class == 'person' then return keys[3] end
  if class == 'photo' then return keys[5] end
  return keys[2]
end

local function rollingWindow(keys)
  local now = redisNow()
  local counts = {}
  local oldest = now
  local used = 0
  for _, i in ipairs({2, 3, 5}) do
    redis.call('ZREMRANGEBYSCORE', keys[i], '-inf', now - 3600000)
    counts[i] = redis.call('ZCARD', keys[i])
    used = used + counts[i]
    local first = redis.call('ZRANGE', keys[i], 0, 0, 'WITHSCORES')
    if #first > 0 then oldest = math.min(oldest, tonumber(first[2])) end
  end
  return now, counts, used, oldest
end
