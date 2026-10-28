# Incident management

The internal `ops/app.Incidents` manager owns incident lifecycle actions. It is
not an HTTP endpoint; detectors call the application methods directly.

- `Open` validates input and deduplicates by `kind` and `scope`. Repeated
  opens of an active incident increment `count` and advance `last_seen`,
  while preserving its original severity and evidence.
- `Ack` moves an open incident to `acked` and records the administrator ID.
- `Close` closes an open or acknowledged incident.
- `AutoClose` closes active incidents for one detector kind and scope. It is
  safe to call repeatedly.
- Only a newly opened critical incident writes a `notification.requested`
  outbox event, atomically with the incident. The request targets administrators
  and the alert channel; delivery and detector implementations remain outside
  this ticket.

Incident details must be JSON objects. Notifications include the incident ID,
severity and title, and do not copy detail payloads into the outbox.
