# Crawl observations and historical recovery

The crawl engine binds every page to the work item's XFlickr user and an eligible
connected account belonging to that user before acquiring quota. The account ID
is pinned for the HTTP request and the page commit. A key suggested by the shared
key pool is used only if it belongs to that user; otherwise resolution selects
that user's first eligible account/key. If no account remains eligible, the item
waits without making a Flickr call. No account is inferred from a photo owner or
work subject.

The composition root wraps the transactional catalog writer once, for every
facet. People and photos are upserted and observed in the same transaction as
cursor advancement. The primary key `(user_id, flickr_account_id, record_kind,
record_id)` makes repeated pages and crash reruns idempotent and preserves the
first committed observation time. Duplicate records within a page produce one
observation write. An error rolls back both catalog and observations. The engine's
existing locked-state/cursor check also rejects stale page commits.

## Observed record kinds

This table is the single inventory; facets do not each implement observation
recording. A future visibility policy for another kind must extend this inventory
and its centralized writer before that policy is enabled.

| Kind | Observation behavior and reason |
| --- | --- |
| Person | Every `Result.People` upsert is observed, including sparse photo owners and owners returned by album/gallery facets. People reads consult person observations and visible photos. |
| Photo | Every `Result.Photos` upsert is observed, including photos, favorites and reconciliation pages. API, download and search visibility depend on photo observations. |
| Favorite | No separate observation: connected-account ownership scopes the relationship and the referenced photo uses the shared photo policy. Its photo and returned owner are observed by the same writer. |
| Album | No separate observation: reads scope albums to the user's connected account and apply photo visibility to album contents and counts. Returned people are observed. |
| Gallery | No separate observation: no gallery visibility policy or catalog read endpoint currently uses observations. Returned people are observed. |
| Group / membership | No separate observation: no group visibility policy or catalog read endpoint currently uses observations. |
| Sizes / enrichment | No new catalog record observation: these update an existing photo, whose original observed access remains authoritative. |

`crawl_observations` contains optional facet/page facts. It is distinct from
`observations`, which grants catalog visibility; populating one does not populate
the other.

## Guarded legacy backfill

Legacy work item history has no account ID or returned page IDs, and completed
cursors can be overwritten. It cannot establish provenance in a multi-user or
multi-account database. The lead approved a narrower legacy attestation: when the
database contains exactly one user and one connected Flickr account, every stored
person and photo came from that account's crawl.

Build the command, then run it from the intended environment's directory so its
private `.env` is loaded. The lead supplies the explicit IDs; do not copy API keys
or OAuth credentials into command arguments.

```sh
(cd backend && go build -o ../.tmp/xflickr ./cmd/xflickr)
./.tmp/xflickr backfill-observations --user-id <user-id> --account-id <account-id> --batch-size 500
```

The command reports user/account counts, catalog counts, committed batches,
inserted observations and run time as JSON. It refuses attribution unless the
specified user/account are the database's only user/account and belong together,
including when an extra account has a non-OK status. On refusal the catalog counts
identify the size of the unresolved legacy catalog; no record is attributed by
that batch. An operator must resolve provenance outside this command in a
multi-user/account installation. There are no Flickr calls.

Each bounded batch locks user/account tables against registration or account
changes, rechecks the singleton condition, and commits independently. Numeric photo
IDs and person NSIDs use indexed keyset pagination. A restart can use the same
command: existing observations are skipped, committed timestamps stay unchanged,
and missing observations are filled. If the database stops satisfying the guard
between batches, earlier commits remain and the next batch refuses. Catalog counts
are collected once, rather than scanning the entire catalog for every batch.

Backfilled `first_seen_at` is **approximate**: the record's `last_seen_at` when
present, otherwise this command's run time. People/photos have no immutable local
creation timestamp. Only observations recorded by the new crawl path carry the
exact first-observed time. The approximation is acceptable for the ordering of
“new from your contacts”; no marker column is added. Existing observations are
never overwritten.

Tests use only the generated worktree database. Singleton backfill fixtures use
connection-local temporary tables cloned from its migrated schema, preserving all
unrelated users and records. The full-stack browser scenario connects through fake
Flickr and reads actual observed catalog results without SQL observation inserts.
The base's catalog screens are placeholders, so the browser navigates the real
catalog JSON endpoints after visiting Crawl; catalog UI implementation stays out
of scope. `web/e2e/support/catalog.ts` is absent on this base.
