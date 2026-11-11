# Node heartbeat client

The heartbeat client sends one `POST /internal/nodes/heartbeat` request to a
control node. Its caller provides the control URL, node token, HTTP client, and
metadata collected for that heartbeat:

- `Version` is limited to 64 bytes.
- `EgressIP` is empty or a valid IP address.
- `Metrics` is a JSON object no larger than 64 KiB. An empty value is sent as
  `{}`.

The request includes all three metadata fields and a bearer token. The response
must contain a node name, a supported state, and a valid RFC3339 heartbeat
timestamp. The client reads at most 64 KiB of response data and closes the body.

Each call makes one request. The client does not retry and does not follow
redirects, so the bearer token is not forwarded to another URL. Errors include
the HTTP status when available, but never include the token or response body.
The request context controls cancellation.

This slice only provides the one-shot client. Its caller supplies the metadata;
this code does not measure the egress IP or system resources, schedule periodic
heartbeats, configure the node token, or start the `node-agent` role.
