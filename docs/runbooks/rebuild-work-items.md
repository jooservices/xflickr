# Rebuild bloated work_items and river_job

Lead-only runbook for the live `xflickr` database after JOOXF-370 merges.
Developers must not run this against live data from a worktree.

## Preconditions

- Maintenance window agreed; expect AccessExclusiveLock for `VACUUM FULL`.
- Confirm `pg_repack` availability:

```sql
SELECT * FROM pg_available_extensions WHERE name = 'pg_repack';
```

- Capture before sizes:

```sql
SELECT relname,
       pg_size_pretty(pg_relation_size(oid)) AS heap,
       pg_size_pretty(pg_total_relation_size(oid)) AS total,
       n_live_tup, n_dead_tup
FROM pg_stat_user_tables
WHERE relname IN ('work_items', 'river_job');
```

## work_items

1. Reindex each index concurrently (non-blocking for readers/writers aside
   from brief share locks at start/end):

```sql
REINDEX INDEX CONCURRENTLY work_items_dedupe_key;
REINDEX INDEX CONCURRENTLY work_items_pending_score_idx;
REINDEX INDEX CONCURRENTLY work_items_ready_lease_idx;
-- include every remaining work_items index from pg_indexes
```

2. Table rewrite (choose one):

- If `pg_repack` is installed: `pg_repack -d xflickr -t work_items --no-order`
  (online; still monitor load).
- Otherwise in a short window: `VACUUM FULL work_items;`

   On a synthetic 500k-row table built with
   `docker/scripts/synthetic-work-items.sh`, expect `VACUUM FULL` on the order
   of minutes proportional to heap size (record the measured duration in the
   change ticket before touching live). The lock blocks all access to the
   table for the duration.

## river_job

Same pattern: `REINDEX INDEX CONCURRENTLY` for River indexes that are bloated,
then `pg_repack -t river_job` or `VACUUM FULL river_job` in the window.

## After

Re-run the size query, confirm dead tuples near zero, and watch
`n_tup_hot_upd / n_tup_upd` during steady crawl (target ≥ 70 percent for the
state-transition workload after JOOXF-370).
