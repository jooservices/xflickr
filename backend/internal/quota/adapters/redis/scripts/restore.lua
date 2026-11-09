-- Same KEYS as acquire. ARGV: class, timestamp ms, stable member triples.
-- When the ready marker is absent the ledger is untrusted. Acquire already
-- refuses every grant until restore finishes, so no live grant can race a
-- rebuild. Clear the rolling lane sets, then replay the call log. ZADD NX
-- stays for idempotent re-entry of the same log rows. Running restore twice
-- yields the same count: the second call exits early once ready is set.
-- The ready marker lives as long as the ledger is live: restore sets it,
-- grants extend it, Redis loss removes it.
if redis.call('EXISTS', KEYS[6]) == 1 then return 0 end
-- Same lane keys rollingWindow already touches (interactive, person, photo).
for _, i in ipairs({2, 3, 5}) do
  redis.call('DEL', KEYS[i])
end
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
