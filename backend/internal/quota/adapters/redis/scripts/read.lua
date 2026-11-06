-- Same KEYS as acquire. Read and prune one consistent rolling snapshot.
local now, counts, _, oldest = rollingWindow(KEYS)
return {now, oldest, counts[2], counts[3], counts[5], redis.call('GET', KEYS[4]) or '0', redis.call('EXISTS', KEYS[6])}
