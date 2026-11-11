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

## Quota snapshot detector

The internal quota detector accepts a snapshot for one API key. It opens a
`quota_exhausted` warning incident when `limit > 0` and `used >= limit`.
Snapshots require a positive key ID and limit and a non-negative usage count.
The incident scope is `key:<decimal key ID>`; its detail contains only the
detection, automatic-action and human-step text plus the usage and limit. The
human step is: “Check the key and reset time in API quota; follow key policy.”

This detector consumes a supplied snapshot and does not call Flickr, rotate a
key, restart a crawl, or close incidents when quota remains. It is not connected
to a scheduler: the shared Detector and watchdog contract is pending JOOXF-159.
Acknowledged incidents are not deduplicated by the current repository, so this
slice does not claim a complete scheduled incident lifecycle or production
deduplication behavior.
