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

`work_items` and River indexes are owned by JOOXF-370 and are out of scope here.

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
