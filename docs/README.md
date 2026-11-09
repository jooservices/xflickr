# Documentation

- [Download progress events](download-progress.md)
- [Health and readiness probes](health-probes.md)

- [Crawl observations and historical recovery](crawl-observations.md) — transactional provenance, observed kinds, and guarded batched backfill.
- [Sharing settings and observations](sharing.md)
- [Ops monitoring](ops-monitoring.md) — crawl health log line, admin status
  endpoint, granted-call and quota-wait counters, OpenObserve stream and alerts.
- [Operations metrics collection](metrics.md) — minute samples, catalogue,
  retention and rollups for the admin operations dashboard (JOOXF-368 phase 1).
- [Reading queue metric samples](metrics-series-read.md) — bounded admin reads
  of one stored queue, role, and node series (JOOXF-163 slice 1).
- [Incident management](incident-management.md) — deduplication, acknowledgement,
  closure, and critical incident notification events.
- [Structured logging and telemetry](observability.md) — context fields, OTLP
  export, event helper, offline log spill behavior, and cheap crawl-health
  snapshot estimates / lock / timeout.
- [Database report](db-report.md) — read-only before/after report of table,
  index, queue, worker and crawl-throughput figures.
- [Production runtime](production-migration.md) — the main clone on the ESXi
  services: accounts, data move, switch, rollback.

- [Database layout and partitioning](database.md) — partition keys, measured
  owner-vs-id query cost, migration cost, and api_call_log retention.
- [Environment and shared services](ENVIRONMENT.md) — workstation, runtime, and
  database rules.
- [Database index inventory](database.md) — keep/drop decisions, sizes, scan
  evidence, and the periodic `pg_stat_user_indexes` review runbook.
- [Develop environment](develop-environment.md) — the WSL application container,
  startup, logs, updates, and safe reset options.
- [Business specification](SPEC.md) — product rules and definition of done.
- [Sessions, CSRF, and roles](sessions.md) — session cookie, double-submit
  CSRF, and role checks.
- [Identity REST API](identity.md) — registration, browser sessions, the
  authenticated user, password changes, and sharing routes.
- [Saved searches](saved-searches.md) — user-owned query persistence and its
  internal application and PostgreSQL operations.
- [Passkey credential storage](passkeys.md) — the WebAuthn credential table and
  the current storage-only scope; passkey login is not available yet.
- [Social sign-in and account linking](social-signin.md) — provider-neutral
  Google, Facebook, X and Apple sign-in, first-time registration, link and
  unlink rules, and error states.
- [Admin user management](admin-users.md) — list, disable and promote users,
  last-administrator rule, and audit port.
- [Node registry and heartbeat](nodes.md) — one-time node tokens, isolated internal listener, and heartbeat health.
- [Key-to-node assignment](key-node-assignment.md) — immutable initial assignments, eligibility, and logical queue lookup.
- [Node heartbeat client](node-heartbeat-client.md) — one-shot authenticated
  heartbeat requests and client limits.
- [Feature flags](feature-flags.md) — administrator-managed rollout flags,
  audience rules, API routes, and cache behavior.
- [Plans](plans.md) — configurable entitlements, administrator updates, and user
  plan assignment.
- [API keys](api-keys.md) — bring-your-own Flickr key validation, sealing, and
  quota caps.
- [Admin API quota view](admin-quota.md) — live per-key quota state and the
  fields the current API does not expose yet.
- [Quota governor](quota.md) — per-key token bucket, clock-second burst cap,
  rolling 60-minute budget, ready-before-grant rebuild, demand-based lane lending and admin floor, and cooldown behavior.
- [ADR 0007: Ledger ready before any grant](adr/0007-ledger-ready-before-grant.md) — grants never set ready; every role rebuilds first.
- [Flickr accounts](flickr-accounts.md) — OAuth connect flow, sealed tokens,
  account status, and events.
- [Catalog people and photos](catalog.md) — partitioned photo metadata,
  idempotent upserts, compressed source payloads, and size status.
- [Search index bootstrap](search-index.md) — the versioned Elasticsearch
  photo index, mapping, alias, and this slice's limits.
- [Golden relevance evaluator](golden-relevance.md) — the fixed recall@3
  threshold, sample fixture, and deferred real-search evaluation.
- [Photo viewer](photo-viewer.md) — the testable lightbox slice, current API
  limits, and deferred gallery integration.
- [Temporary media storage](media.md) — blob storage port, local storage cap,
  and five-minute expiry cleanup.
- [Cloud storage provider contract](cloud-sync-providers.md) — provider port,
  capabilities, upload verification, and reusable adapter contract tests.
- [Dropbox metadata check](dropbox-provider.md) — known-path metadata checks,
  App-folder paths, content hash semantics, and current slice limits.
- [Cloud sync engine](cloud-sync-engine.md) — temporary blob ownership, upload
  verification order, cleanup, and retry boundaries.
- [Google Drive provider slice](google-drive-provider.md) — My Drive root
  folder lookup and creation, error mapping, and deferred provider work.
- [Google Photos raw upload slice](sync-gphotos.md) — standalone upload-token
  client and its current scope limits.
- [AI captioning](ai-captioning.md) — caption port, Ollama vision requests,
  timeout, concurrency, response validation, and circuit breaker behaviour.
- [Fake AI Captioner](fake-ai-captioner.md)
- [AI enrichment freshness](ai-re-enrichment.md) — the model-version freshness
  predicate, retained previous results, and current slice limitations.
- [AI worker caption persistence](ai-worker-caption.md) — the internal
  single-photo caption path, catalog upsert, errors, and deferred worker wiring.
- [Search ranking fusion](search-rrf.md) — BM25 and vector candidate ranking
  merged with Reciprocal Rank Fusion.
- [AI embedding](ai-embedding.md) — embedding port, bounded Ollama batches,
  vector dimensions, provenance, and shared circuit breaker behaviour.
- [Mail delivery](mail.md) — mail port, SMTP adapter, deterministic context
  deadline errors, Mailpit sink, and fake sender for tests.
- [Downloads](downloads.md) — visible-photo requests, per-item state, and media
  queue handoff.
- [Crawl interests](crawl-interests.md) — per-user person depth, lowest-depth
  deduplication, progress reporting, contact priority controls, and pause/resume.
- [Contact screens](contact-screens.md) — session-scoped contact interests,
  paginated list and detail sections, progress summaries, and contact actions.
- [Crawl engine](crawl-engine.md) — facet interface, registry, page job flow
  and error outcomes.
- [Group membership crawl](group-membership-crawl.md) — response validation,
  failure behavior, transactional membership writes, and slice limits.
- [Catalog reconciliation](reconciliation.md) — full photo and favorite scans,
  safe tombstones, and visibility change events.
- [Web app shells](web-shells.md) — user and admin shells, route guard, tokens,
  state components, and EN/VI i18n.
- [Schedule backlog](schedule.md) — deduplicated work items, cursors, static
  score ordering, and state transitions.
- [Database performance notes](database.md) — work_items/river_job bloat,
  static scores, HOT/autovacuum, and people upsert ordering.
- [Rebuild work_items runbook](runbooks/rebuild-work-items.md) — lead-only
  REINDEX / pg_repack / VACUUM FULL for live bloat.
- [Durable job queue](job-queue.md) — transactional River jobs, worker queues,
  retries, retention, and dead letter events.
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
- [Photo lane selection](photo-lane.md) — on-demand getSizes selection, restricted placeholders, ordering and dedupe.
- [Optional photo facets](photo-facets.md) — per-photo Flickr info, EXIF, contexts, comments, and favorites.
- [Internationalisation](i18n.md) — English default, Vietnamese switch, catalogs, glossary, and CI guards.
- [Browser E2E](e2e.md) — the real full-stack Playwright suite, the route sweep and how to add a journey.
- [E2E view coverage](ui/e2e-coverage.md) — every catalogue view mapped to its E2E scenario.
- [Engineering standards](engineering-standards.md) — principles, backend structure, code style, tests, documentation and delivery rules, and the reviewer checklist.
- [REST API rules](rest-api.md) — the binding rule set for every route, the contract, the conformance test and the MCP tools.
- [Fake Flickr server](fake-flickr.md) — seeded REST and OAuth responses, scenarios, and generated images.

# API reconciliation

The current web API to contract and backend route mapping is generated in
[`api-reconciliation.md`](api-reconciliation.md) by
`docker/scripts/api-usage-check.sh`.

- [Deleted entities and restricted facets](entity-state.md) — persistent gone state, caller restrictions and reopening rules.
- [Download sizes continuation](downloads.md) — transactional sizes outcomes, idempotent handoff, and unavailable manifest failures.

- [REST API route mapping](rest-api-mapping.md) — complete route audit, target contracts, consumers, open decisions and module migration order (JOOXF-314).

- [Rolling quota ledger decision](adr/0005-rolling-quota-ledger.md) — atomic grant sets, recovery and deployment constraints.
- [Crawl call audit](crawl-call-audit.md) — per-facet call yield, numeric-count retry fix, one-hour fixture evidence and proposed follow-ups (JOOXF-310).
- [REST building blocks](rest-building-blocks.md) — additive Slice 0 helpers, discovery, idempotency guarantees and reusable conformance cases.
- [Coverage gates](coverage-gate.md) — per-package and per-file 85 percent gates, changed-file diff, and the shrinking baseline.
- [Security scanning](security-scanning.md) — web dependency audit in CI and remaining scanner coverage.
