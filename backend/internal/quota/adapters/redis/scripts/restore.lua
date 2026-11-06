-- Same KEYS as acquire. ARGV: class, timestamp ms, stable member triples.
-- Existing members are retained; replaying the same log rows is idempotent.
if redis.call('EXISTS', KEYS[6]) == 1 then return 0 end
local now = redisNow()
for i = 1, #ARGV, 3 do
  local at = tonumber(ARGV[i + 1])
  if at > now - 3600000 and at <= now then
    local lane = laneKey(KEYS, ARGV[i])
    redis.call('ZADD', lane, 'NX', at, ARGV[i + 2])
    redis.call('PEXPIRE', lane, 7200000)
  end
end
redis.call('SET', KEYS[6], 1, 'PX', 7200000)
return 1
