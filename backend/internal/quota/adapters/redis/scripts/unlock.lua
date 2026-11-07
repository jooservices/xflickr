-- KEYS[1] = lock key, ARGV[1] = ownership token.
-- Delete the lock only when this caller still owns it.
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
