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

## Starred pending work correction

Earlier crawl inserts used `crawl_interests.starred` in the score without
persisting it to `work_items.starred`. Before starting the updated scheduler on
a database that ran that build, apply this batch in its own transaction and
repeat until it updates zero rows. Each batch locks at most 500 pending rows,
refreshes their static scores, and only changes `starred` from false to true;
explicit starred values from other enqueue paths are preserved.

```sql
BEGIN;
SET LOCAL lock_timeout = '1s';
SET LOCAL statement_timeout = '15s';

WITH batch AS (
    SELECT w.id,
           work_items_static_score(
               COALESCE(p.weight, 1),
               work_items_active_direct_action(w.direct_action, w.user_id, w.subject_key),
               ci.starred,
               w.depth,
               w.created_at
           ) AS score
    FROM work_items AS w
    JOIN crawl_interests AS ci
      ON ci.user_id = w.user_id AND ci.person_nsid = w.subject_key
    LEFT JOIN users AS u ON u.id = w.user_id
    LEFT JOIN plans AS p ON p.id = u.plan_id
    WHERE w.state = 'pending' AND w.starred = FALSE AND ci.starred = TRUE
    ORDER BY w.id
    LIMIT 500
    FOR UPDATE OF w SKIP LOCKED
)
UPDATE work_items AS w
SET starred = TRUE, score = batch.score, updated_at = now()
FROM batch
WHERE w.id = batch.id;

COMMIT;
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
