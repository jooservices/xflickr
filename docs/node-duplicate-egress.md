# Duplicate egress IP detection

After a node heartbeat is saved, XFlickr checks crawl nodes for one other
eligible node with the same non-empty egress IP. A node is eligible when its
effective state is online: it is not disabled, its cooling window has ended,
and its last heartbeat is within `domain.WarningAfter` (60 seconds). The lookup
excludes the node that sent the heartbeat and uses `nodes_duplicate_egress_idx`.

When a match exists, XFlickr opens a `duplicate_egress_ip` warning incident
titled `Duplicate egress IP`, with an empty object as its detail. Its scope is
the two node names sorted lexically with each name length-prefixed. This keeps
pairs distinct when a node name contains `|`. Repeated heartbeats for the same
pair update the same open incident.

Detection and incident errors are logged without error text or token material.
They do not change a heartbeat that was already saved; a later heartbeat tries
again.

This slice checks one matching node after each heartbeat. It does not reconcile
all pairs in a watchdog, close incidents when the duplicate disappears, or
deduplicate incidents after acknowledgement. It does not change the heartbeat
API or add UI behavior.
