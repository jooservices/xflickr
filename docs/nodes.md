# Node registry and heartbeat

Administrators register nodes at POST /admin/nodes and receive a 256-bit bearer token once. The database stores only the SHA-256 token hash. Keep the response token in the node secret store; it cannot be read back. Disabled nodes cannot authenticate.

Nodes send POST /internal/nodes/heartbeat with the bearer token and optional version, egress IP, and JSON metrics. Every accepted heartbeat updates the current node row and emits one structured node.heartbeat event. The database does not retain heartbeat history.

The persisted operational states are online, cooling, offline, and disabled. Effective health is derived from the latest heartbeat: up to 60 seconds is online, over 60 seconds through 5 minutes is warning, and over 5 minutes is offline. Disabled state and an active cooling window take precedence.

The internal endpoint is served only on a separate listener configured with INTERNAL_HTTP_ADDR. The value must be an explicit IPv4 or IPv6 interface address and port; wildcard addresses are rejected. Leaving it blank disables the listener. The public HTTP listener always returns 404 for internal paths. Configure the internal address on a private LAN interface before connecting node agents.

Node roles must be unique lowercase identifiers. Heartbeat metrics must be a JSON object no larger than 64 KiB. The endpoint does not change crawler quotas or perform remote Flickr calls.
