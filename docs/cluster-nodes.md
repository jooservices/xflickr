# Cluster nodes screen: quota states

The first `/admin/nodes` slice displays active API keys and their quota states.
It reads `key_id`, optional `user_id`, `label`, and `state` from the existing
`GET /api/v1/admin/quota` response through the shared `useAdminQuota` query.
The Flickr secret is never rendered. A missing user ID appears as `—`.

The quota response's `node` value identifies the node that reported quota
state. It does not identify a key assignment, so this table does not display
it or call itself key placement. An empty key list means there are no active
keys in this response; it does not mean that no nodes have joined.

The page keeps its last successful response during refresh. It marks retained
data as possibly out of date after a refresh error or when the snapshot is at
least 20 seconds old. Initial loading uses the shared 200 ms skeleton delay;
initial request failures offer Retry; a 403 replaces the page with the shared
forbidden state. The admin shell's Live control governs the existing quota
polling: polling stops while Live is off and resumes through the shared query
when Live is turned back on.

This slice does not show node health, capacity, resources, workers, egress IP,
429 counts, or assigned keys. It adds no node actions, node details, token
creation, SSE, API contract changes, or backend queries. Those behaviors
remain for later slices.
