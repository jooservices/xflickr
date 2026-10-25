# Queue state and ETA

`GET /me/crawl/queue` returns active work counts for the `person` and `sizes`
lanes. Active backlog includes pending, ready, running, and waiting work items.
Work kinds containing `size` are counted in the sizes lane; other crawl work is
counted in the person lane.

The effective calls per hour for each lane sums each distinct connected valid
API key's configured hourly allowance, capped by its per-second allowance and
the global daily limit. Both lanes preserve the configured interactive-call
reserve. The sizes lane is also capped by the configured person lane share, as
in the quota governor. ETA hours is the lane's backlog calls divided by that
capacity. A lane with backlog and zero configured capacity has a `null` ETA.

`GET /admin/queue` groups active backlog by plan and lane, reports the age in
seconds of the oldest active item, and counts available River jobs by worker
queue. Shared work without an owning user is grouped as `unassigned`.

The scheduler publishes a global `queue.state` event through the transactional
outbox at startup and once per minute while it is running. Its payload contains
active backlog counts by lane and the total available River job count.
