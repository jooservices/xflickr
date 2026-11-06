# 0004: Separate entity disappearance from facet restrictions

Status: Accepted (JOOXF-309, lead decisions 24957 and 24969)

## Context

Flickr list failures identify the addressed subject. Permission errors describe
access to a facet and cannot safely establish global disappearance. Photo
not-found responses also occur for existing restricted photos.

## Decision

Keep one `entity_states` registry for global gone markers and one
`entity_facet_restrictions` registry keyed by subject, facet and caller. Preserve
the first outcome independently of work-item completion and catalog upserts.
Keep photo deletion in reconciliation's existing tombstones. Place eligibility
and failure ports in the crawl application and implement storage in PostgreSQL;
workers and the scheduler check eligibility before spending quota.

## Consequences

Missing IDs can be retained even without catalog rows. A restricted facet cannot
silence unrelated facets or callers. Person restrictions have an admin-managed
recheck interval; photos reopen only on a new version or visibility change.
Eligibility adds indexed database reads. Queued-job cleanup remains JOOXF-311;
in-flight calls are not cancelled by the marker.
