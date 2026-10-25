# Documentation

- [Environment and shared services](ENVIRONMENT.md) — workstation, runtime, and
  database rules.
- [Business specification](SPEC.md) — product rules and definition of done.
- [Sessions, CSRF, and roles](sessions.md) — session cookie, double-submit
  CSRF, and role checks.
- [Admin user management](admin-users.md) — list, disable and promote users,
  last-administrator rule, and audit port.
- [Plans](plans.md) — configurable entitlements, administrator updates, and user
  plan assignment.
- [API keys](api-keys.md) — bring-your-own Flickr key validation, sealing, and
  quota caps.
- [Quota governor](quota.md) — per-key token bucket, hourly budget, lane shares,
  and cooldown behavior.
- [Flickr accounts](flickr-accounts.md) — OAuth connect flow, sealed tokens,
  account status, and events.
- [Catalog people and photos](catalog.md) — partitioned photo metadata,
  idempotent upserts, compressed source payloads, and size status.
- [Crawl interests](crawl-interests.md) — per-user person depth, lowest-depth
  deduplication, progress reporting, contact priority controls, and pause/resume.
- [Crawl engine](crawl-engine.md) — facet interface, registry, page job flow
  and error outcomes.
- [Web app shells](web-shells.md) — user and admin shells, route guard, tokens,
  state components, and EN/VI i18n.
- [Schedule backlog](schedule.md) — deduplicated work items, cursors, and state
  transitions.
- [Durable job queue](job-queue.md) — transactional River jobs, worker queues,
  retries, and dead letter events.
- [Queue state and ETA](queue-state.md) — backlog snapshots, lane capacity,
  ETA estimates, and the `queue.state` event.
- [Architecture decision records](adr/README.md) — durable architecture choices.
- [UI view catalogue](ui/README.md) — all views, overlays, and the spec gate.
- [User interface proof of concept](ui/poc/README.md) — static design prototype.
- [UI design system](ui/design-system.md) — themes, typography, iconography, and
  accessibility rules.
- [UI component library](ui/components.md) — shared component props, states,
  keyboard behaviour, and the gallery mockup.
- [UI token source contract](ui/tokens.md) — machine-readable token schema and
  POC coverage requirements.
- [UX patterns](ui/patterns.md) — loading, empty, error, live update, responsive,
  and test id rules shared by every screen.
- [Accessibility](ui/a11y.md) — WCAG 2.2 AA checklist, focus, keyboard, and live
  region rules.
- [Internationalisation and copy](ui/i18n.md) — key naming, English and
  Vietnamese rules, formatting, and plurals.
- [Photo lane selection](photo-lane.md) — catalog-driven getSizes selection, ordering and dedupe.
