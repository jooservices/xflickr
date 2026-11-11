# Key-to-node assignment

The cluster application API stores an initial assignment for a Flickr API key
and returns the logical queue for an already assigned key. This is an internal
backend API; it adds no REST route or UI.

## Initial assignment

`cluster/app.Assignments.AssignInitial` accepts a positive `api_keys.id` and an
explicit node name. The repository stores the assignment only when the key is
valid and the node is eligible. Eligibility requires `state = 'online'`, the
`crawl` role, a heartbeat at or after `now - domain.WarningAfter`, and no active
cooling window.

The stored row contains `key_id`, `node`, `since`, and `reason = 'initial'`.
The key ID is the primary key, so an assignment is immutable in this slice:
repeated calls return the original node and timestamp, including when a caller
supplies another node. Concurrent initial assignments also return the single
stored row. Deleting an API key cascades to its assignment; deleting an
assigned node is restricted.

## Queue lookup

`cluster/app.Assignments.QueueFor` returns `crawl@<node>` for a stored
assignment. It returns `cluster/app.ErrAssignmentUnavailable` when the key has
no assignment. The method returns the logical queue name and does not encode a
River queue name.

## Scope

Callers choose the target node explicitly. Least-loaded node selection,
scheduler pumping, worker subscriptions to per-node queues, dispatch health
checks, and moving assignments are deferred. Existing scheduler and worker
behavior remains unchanged. A future dispatcher must check node health before
using a stored assignment; this API continues to return the assignment even
if its node later becomes unhealthy.
