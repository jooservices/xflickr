# Database

Schema migrations live in `backend/migrations` and run through
`xflickr migrate`. This page records operational database practice that is not
already covered by a feature document.

## Index inventory

Live scan counts below come from the production audit of `pg_stat_user_indexes`
on 2026-11-09, covering statistics accumulated since 2026-11-02 (about seven
days). Index statistics reset on `pg_stat_reset` and after crash recovery; treat
any count as relative to the window since the last reset. Partition child names
follow the column list (`photos_pNN_…`); drop or create through the partitioned
parent so every child follows.

Sizes are approximate per partition × 16 partitions unless noted.

| Index (parent) | Table | Size (approx.) | Scans (2026-11-02..09) | Queries that use it | Decision |
| --- | --- | --- | --- | --- | --- |
| `photos_pkey` | photos | PK | n/a | lookups by `id` (`photos.go`, downloads) | keep (primary key) |
| `photos_last_update_idx` | photos | ~54 MB × 16 | 0 | none match a global `last_update` predicate; `reads.go` lists by `owner_nsid` then orders by `last_update` | **drop** (JOOXF-371) |
| `photos_owner_nsid_idx` | photos | ~27 MB × 16 | 3 | redundant leading prefix of `photos_owner_last_seen_idx` and `photos_sizes_pending_idx` | **drop** (JOOXF-371) |
| `photos_owner_last_seen_idx` | photos | ~82 MB × 16 | 0 | `engine.go` tombstone by `owner_nsid` + `last_seen_at`; JOOXF-345 `RepairZeroCheckpoints` `MAX(last_seen_at) WHERE owner_nsid` | keep (needed by JOOXF-345) |
| `photos_sizes_pending_idx` | photos | ~127 MB × 16 (as `owner_nsid, uploaded_at, id`) | heavy | `photolane.sql` pending-size selection | keep |
| `photos_sizes_status_idx` | photos | small | used | `ops/store.go` counts by `sizes_status` | keep |
| `favorites_pkey` | favorites | PK | n/a | upsert / delete by `(person_nsid, photo_id)` | keep (primary key) |
| `favorites_page_idx` | favorites | ~150–240 MB × 16 | 0 | `favorites.go` page by `person_nsid, fave_date, photo_id`; JOOXF-345 `MAX(fave_date) WHERE person_nsid` | keep (needed by JOOXF-345 + catalog reads) |
| `favorites_active_person_seen_idx` | favorites | partial | used by reconcile | periodic reconcile active rows by `person_nsid, seen_at` | keep |
| `observations_pkey` | observations | ~2.1 GB of ~4.3 GB total | n/a | observation upserts / visibility joins by PK | keep (primary key only; no secondary indexes) |
| `album_photos_pkey` | album_photos | PK | n/a | membership by `(album_id, photo_id)` | keep (primary key) |
| `album_photos_order_idx` | album_photos | | used | `albums.go` / `reads.go` order by `position, photo_id` | keep |
| `people_pkey` | people | PK | n/a | nsid lookups | keep (primary key) |
| `groups_pkey` | groups | PK | n/a | nsid lookups | keep (primary key) |
| `albums_pkey` / `albums_owner_updated_idx` | albums | | used | `albums.go` list by `owner_nsid, updated_at, id` | keep |
| `galleries_pkey` / `galleries_owner_updated_idx` | galleries | | used | gallery list by owner | keep |
| `person_groups_pkey` | person_groups | PK | n/a | membership pairs | keep (primary key) |
| `crawl_interests_pkey` | crawl_interests | PK | n/a | per-user person rows | keep (primary key) |
| `crawl_interests_person_nsid_idx` | crawl_interests | | used | `interests.go` by `person_nsid` | keep |
| `crawl_interests_paused_idx` | crawl_interests | partial | used | pause checks / enqueue filters | keep |
| `crawl_interests_selected_idx` / `_starred_idx` / `_depth_idx` | crawl_interests | | used | `photolane.sql` ranking streams | keep |
| `work_items_failed_updated_idx` | work_items | partial | not in audit window | Reclaim self-heal by failed state and `updated_at` | keep (JOOXF-386) |

Existing `work_items` and River indexes are owned by JOOXF-370 and are out of
scope for the index-drop audit above.

## Scheduler plan backlog count

JOOXF-385 adds the partial index
`work_items_pending_plan_backlog_idx` on `(key_id, worker_type, user_id,
not_before)` for pending rows. The scheduler probes each plan's users and
counts at most the queue limit, so an empty plan does not walk another plan's
backlog. The index does not rewrite the table.

On the Dev Workspaces VM worktree database, the integration test generated
400,000 pending sync rows for the pro plan and left the free plan empty while
measuring `EXPLAIN (ANALYZE, BUFFERS)`:

| Query | Execution | Shared buffers | `work_items` Seq Scan |
| --- | ---: | ---: | --- |
| Previous unbounded group count | 221.90 ms | 13,374 | Yes |
| Bounded per-plan count | 0.23 ms | 16 | No |

The new partial index occupied 15,843,328 bytes with that 400,000-row fixture.
The test then added a low-score free-plan row after the pro backlog and
verified that `SplitShare` allocated it, with the remaining 99 slots assigned
to pro. The fixture runs inside a transaction and is rolled back.

After deployment, remeasure on production with `db-report` and a 40-second
`pg_stat_activity` sample; these worktree figures do not establish production
runtime behavior.

Foreign-key note: after dropping `photos_owner_nsid_idx`, deletes of `people`
still have a supporting index because `photos_owner_last_seen_idx` leads with
`owner_nsid`. Never drop a unique constraint, primary key, or the only index
whose leading column matches an FK without checking `pg_constraint`.

## Periodic index review

Run at least seven days after any statistics reset (or after a major release
that changes query shapes):

```sql
WITH idx AS (
    SELECT s.indexrelid,
           coalesce(p.inhparent, s.indexrelid) AS root,
           s.idx_scan,
           pg_relation_size(s.indexrelid) AS bytes
    FROM pg_stat_user_indexes s
    LEFT JOIN pg_inherits p ON p.inhrelid = s.indexrelid
    WHERE s.schemaname = 'public'
)
SELECT root::regclass AS index_name,
       count(*) AS partitions,
       sum(idx_scan) AS scans,
       pg_size_pretty(sum(bytes)) AS size
FROM idx
GROUP BY root
ORDER BY sum(bytes) DESC, sum(idx_scan) ASC;
```

Scan counts are summed over the partitions of each parent index: a partition
with zero scans does not make the parent unused while other partitions use it.
Only a parent whose total is zero is a drop candidate. Before dropping, run the
migration when no long transaction is open: `DROP INDEX` on a partitioned index
needs an exclusive lock and the migration sets `lock_timeout` so it fails fast
and can be repeated.

For each large index with zero or near-zero `idx_scan`, grep
`backend/internal` (including sqlc `*.sql`) for predicates that match the
index columns, check in-flight PRs that may need it, and only then open a
ticket to drop it with a reversible migration.

## Database layout and partitioning

Live size snapshot (owner audit, 2026-11-09, database `xflickr`):

| Table | Approx size | Rows | Partitioning today |
|---|---:|---:|---|
| photos | 62 GB (56 GB heap + 5.9 GB indexes) | 27.7M | `HASH (id)` × 16 |
| favorites | 7.7 GB (5.6 GB indexes) | 30M | `HASH (person_nsid)` × 16 |
| observations | 4.3 GB | — | `HASH (record_id)` × 16 |
| album_photos | — | — | `HASH (photo_id)` × 16 |
| api_call_log | ~500 MB | — | `RANGE (called_at)` daily |

Synthetic measurement dataset (worktree only):
`docker/scripts/synthetic-catalog.sh` builds 100,000 owners, 3,000,000 photos
and 3,000,000 favorites, plus comparison tables `photos_by_owner`
(`HASH (owner_nsid)` × 16) and `favorites_by_person`. Keep it out of CI.
Recreate the worktree database afterwards with
`python3 docker/scripts/worktree_env.py jooxf372`.

## Partitioning

### Hottest queries (from code)

| Query | File | Filter | Partitions touched today |
|---|---|---|---:|
| Photo lane candidate join | `catalog/adapters/postgres/photolane.sql` | `photos.owner_nsid = ranked.person_nsid` | 16 (`HASH (id)`) |
| List person photos | `catalog/adapters/postgres/reads.go` (~148) | `photos.owner_nsid = $2` | 16 |
| Person photo counts | `reads.go` (~77, ~124) | `photos.owner_nsid = people.nsid` | 16 |
| Get photo by id | `reads.go` (~347), `photos.go` (~28), `photo_facets.go` | `photos.id = $1` | 1 |
| List favorites | `favorites.go` (~57–62) | `favorites.person_nsid = $1` | 1 (already `HASH (person_nsid)`) |
| Album primary / album photos | `reads.go` (~246, ~316, ~325) | `photos.id = …` | 1 per id lookup |

Favorites are already partitioned by the hot filter column. The open question
is therefore almost entirely about `photos` (and secondarily `observations` /
`album_photos`, which are not owner-filtered the same way).

### Measured comparison (synthetic 3M)

Script: `docker/scripts/synthetic-catalog.sh` then
`docker/scripts/measure-partitions.sql`. `ANALYZE` ran before EXPLAIN
(ANALYZE, BUFFERS). Three timed runs on worktree `xflickr_wtjooxf372`
(2026-11-09). p50 is the median execution time; p95 is the max of the three
samples (cold run 1 still dominated by shared reads).

| Query | Layout | Partitions | p50 / p95 exec (ms) | Warm buffers (hit/read) | Notes |
|---|---|---:|---:|---|---|
| owner list LIMIT 50 | `HASH (id)` | 16 | 1.98 / 208.2 | 84 / 0 | Append over 16 children |
| owner list LIMIT 50 | `HASH (owner_nsid)` | 1 | 0.62 / 11.0 | 33 / 0 | Prunes to one child |
| id lookup | `HASH (id)` | 1 | 0.10 / 16.3 | 7 / 0 | Current PK `(id)` |
| id lookup | `HASH (owner_nsid)` id alone | 16 | 294 / 82504 | 19081 / 0 | No prune; scans all children |
| id+owner lookup | `HASH (owner_nsid)` | 1 | 0.06 / 0.11 | 4 / 0 | Needs both columns |
| favorites by person | `HASH (person_nsid)` | 1 | 0.67 / 11.2 | 33 / 0 | Already optimal (current) |
| favorites by person | comparison copy | 1 | 0.32 / 6.8 | 33 / 0 | Same key as production |

Warm owner-list latency improves about 3× with `HASH (owner_nsid)` (1.98 ms →
0.62 ms) and cold I/O improves more (~208 ms → ~11 ms). The id-alone lookup
on the owner-hashed table is the regressor: warm ~294 ms and cold ~82 s versus
~0.1 ms on today's `HASH (id)`.

Comparison copy wall time (CREATE partitions + PK/indexes +
`INSERT SELECT` for 3M photos and 3M favorites): **145 seconds** on this
worktree host.

### Primary keys and foreign keys under `HASH (owner_nsid)`

PostgreSQL requires every unique index on a partitioned table to include the
partition key. Moving photos to `HASH (owner_nsid)` forces
`PRIMARY KEY (owner_nsid, id)`. There is then no table-level `UNIQUE (id)`, so
every foreign key that today references `photos (id)` stops compiling:

- `album_photos.photo_id`
- `albums.primary_photo_id`
- `assets.photo_id`
- `download_items.photo_id`
- `favorites.photo_id`
- `galleries.primary_photo_id`
- `gallery_photos.photo_id`
- `group_pool_photos.photo_id`
- `photo_comments.photo_id`
- `photo_contexts.photo_id`
- `photo_enrichments.photo_id`
- `photo_exif.photo_id`
- `photo_facet_status.photo_id`
- `photo_favorites.photo_id`
- `photo_info.photo_id`

Serving those relationships after a repartition would need one of:

1. denormalise `owner_nsid` into every child table and change each FK to
   `(owner_nsid, photo_id)`, or
2. keep a global non-partitioned id map (`photo_id → owner_nsid`) and join it
   on every id lookup, or
3. abandon declarative FKs and enforce in application code.

Id-only readers today (`photos.go`, `photo_facets.go`, catalog
`GET /photos/{id}`, download writers, enrichment writers) would all need the
owner alongside the id, or an extra map lookup.

### Migration cost for the live ~79 GB database

Online strategy (if ever approved):

1. Create `photos_new` partitioned by `HASH (owner_nsid)` with
   `PRIMARY KEY (owner_nsid, id)` and the same secondary indexes.
2. Batched copy by owner ranges (or by hash remainder) with dual-write or a
   trigger / logical replication stream so new upserts land in both tables.
3. Cut over with a short lock: rename, recreate FKs (or switch to the chosen
   FK strategy), refresh sequences.
4. Rollback: keep the old table until the first full vacuum after cutover
   proves stable.

Disk: roughly another 62 GB for photos plus indexes during dual residency
(about 70+ GB headroom), then drop the old table. Duration from the synthetic
145 s comparison copy: row-count scale alone (27.7M / 3M ≈ 9.2×) suggests
about 10–20 minutes for a photos-sized heap+index copy if density matched the
synthetic rows; live photos are ~2 KB/row and 62 GB on disk, so a realistic
online backfill is **hours**, not minutes, once dual-write catch-up and FK
rebuild are included. Risks: FK redesign mistakes, dual-write drift,
long-running autovacuum debt after cutover, and every id-only code path
silently probing 16 partitions if owner is omitted.

### Recommendation

**LATER for photos/favorites repartition.** Favorites already use
`HASH (person_nsid)`. Photos owner queries would benefit from
`HASH (owner_nsid)`, but the FK and id-lookup redesign is a multi-ticket
migration with large dual-disk cost; do not start it in the same release as
cheaper wins (retention, unused indexes, work_items bloat).

**GO for api_call_log retention** (implemented in this ticket). Daily
`RANGE` partitions already exist; dropping expired children with
`DETACH PARTITION … CONCURRENTLY` then `DROP TABLE` frees space without
locking writers, and rollups keep hourly aggregates.

Ticket plan if photos repartition is reopened:

1. Design FK strategy (denormalise owner vs global map) and list every id-only
   reader.
2. Prototype dual-write on a worktree copy of production-shaped data.
3. Online backfill + cutover runbook with measured disk and duration.
4. Application changes for id+owner reads, then cutover.

## api_call_log retention

Settings (admin registry, not user-overridable):

| Key | Default | Bounds |
|---|---:|---|
| `maintenance.api_call_log_retention_days` | 30 | 7–365 |
| `maintenance.api_call_log_dry_run` | false | boolean |

Behaviour:

- Quota partition maintenance still creates the retention window and the
  7-day future horizon idempotently (`quota/adapters/postgres` create-only).
- A separate `platform/maintenance` job runs hourly (same interval as the
  existing maintainer), takes a cluster-wide `pg_try_advisory_lock`, selects
  at most one daily partition whose upper bound is at or before the cutoff
  (`today - retention_days`), rolls that day into `api_call_rollup`, then
  `ALTER TABLE api_call_log DETACH PARTITION … CONCURRENTLY` and
  `DROP TABLE`. Dry-run only logs.
- Failures are logged and never stop the next tick. Contending roles skip the
  tick when the lock is held.

See also [observability](observability.md) for call-log telemetry and
[ops monitoring](ops-monitoring.md) for health counters that read the log.

## work_items (JOOXF-370)

Pending dispatch order uses the `score` column as a **time-invariant sort
key**. Because the historical priority formula was linear in age, ordering by
`base - wAge * epoch(created_at) / 60` matches ordering by the minutes-aware
score at any shared "now". The absolute value is not a minutes-aware score.

Scores are written once at insert (and on reopen), and recomputed when an input
changes: star/select (`RescorePerson`), plan weight, depth/direct flags, or
admin score weights. A stored weights fingerprint (including a digest of plan
weights) triggers one set-based `UPDATE ... FROM (SELECT ... LIMIT n)` pass in
bounded batches, selecting only rows whose computed score differs. A bounded,
expiry-indexed pass removes direct-action bonuses after `selected_until` passes;
enqueue and rescore use the same expiry rule. There is no per-tick aging rewrite
and `score_updated_at` is unused.

`fillfactor=70` and tightened per-table autovacuum settings favour HOT updates
on state/lease transitions. The pending score index still includes `score` and
`not_before`, so state transitions that also touch indexed columns remain
non-HOT; measure with the synthetic steady-state test.

Existing heap and index bloat is cleared with the lead-only runbook
[rebuild-work-items.md](runbooks/rebuild-work-items.md). Never rewrite
`work_items` inside a migration.

JOOXF-386 adds `work_items_failed_updated_idx` on `updated_at` for failed
rows. Reclaim uses it to find lease-expired failures old enough for self-heal;
pending and active rows do not occupy this partial index. The integration
EXPLAIN test uses 400,000 work items with four failed rows and verifies that
self-heal uses this index without a `work_items` Seq Scan. On the same fixture,
the Reclaim lease selector, pump counts, claim and key-assignment queries, and
pending and expired-selection rescore queries also had no `work_items` Seq
Scan. Confirm the production `pg_stat_user_tables` rate after deployment;
worktree plans do not establish production behavior.

## river_job (JOOXF-370)

A partial expression index on `(args->>'kind')` for active states supports
health and metrics lookups. Completed and discarded retention default to 24h
and 168h (`queue.completed_retention`, `queue.discarded_retention`) and feed
River's cleaner. Autovacuum for `river_job` uses the same tight scale factor
as `work_items`.

## people upserts (JOOXF-370)

People writes sort each batch by `nsid`. `EnsurePeople` inserts missing rows
with `ON CONFLICT DO NOTHING`; `RefreshChangedPeople` locks and updates only
changed rows with `FOR NO KEY UPDATE` in NSID order, compatible with foreign-key
key-share locks. It advances `last_seen_at` at most once per hour per person.
