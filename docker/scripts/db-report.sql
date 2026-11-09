-- Read-only database report (JOOXF-373). Run through docker/scripts/db-report.sh.
-- Variable: deep=1 adds exact counts (slower, reads work_items).
\pset footer off
\pset border 1
\echo
\echo '== 1. Server'
select version();
select (now() - pg_postmaster_start_time())::interval(0) as uptime, current_database() as database, pg_size_pretty(pg_database_size(current_database())) as size;
select name, setting || coalesce(unit, '') as value
from pg_settings
where name in ('shared_buffers','effective_cache_size','work_mem','maintenance_work_mem','max_wal_size','min_wal_size',
               'checkpoint_timeout','wal_compression','random_page_cost','effective_io_concurrency','autovacuum_vacuum_scale_factor',
               'autovacuum_max_workers','autovacuum_vacuum_cost_limit','track_io_timing','shared_preload_libraries','max_connections')
order by name;

\echo
\echo '== 2. Checkpoints (cluster, since stats reset)'
select num_timed, num_requested,
       round(100.0 * num_requested / nullif(num_timed + num_requested, 0), 1) as requested_pct,
       stats_reset
from pg_stat_checkpointer;

\echo
\echo '== 3. Disk reads and cache hit by database (top 6 by blocks read)'
select datname, pg_size_pretty(pg_database_size(datname)) as size, blks_read,
       round(100.0 * blks_hit / nullif(blks_hit + blks_read, 0), 2) as hit_pct,
       pg_size_pretty(temp_bytes) as temp_written, deadlocks
from pg_stat_database
where datname is not null and datname not like 'template%'
order by blks_read desc
limit 6;

\echo
\echo '== 4. Largest tables (bytes per row, dead tuples, scans, HOT updates)'
select c.relname as "table",
       pg_size_pretty(pg_total_relation_size(c.oid)) as total,
       pg_size_pretty(pg_indexes_size(c.oid)) as indexes,
       c.reltuples::bigint as est_rows,
       case when c.reltuples > 0 then (pg_relation_size(c.oid) / c.reltuples)::int end as bytes_per_row,
       round(100.0 * s.n_dead_tup / nullif(s.n_live_tup + s.n_dead_tup, 0), 1) as dead_pct,
       s.seq_scan, s.seq_tup_read, s.idx_scan,
       round(100.0 * s.n_tup_hot_upd / nullif(s.n_tup_upd, 0), 1) as hot_pct
from pg_class c
join pg_stat_user_tables s on s.relid = c.oid
where c.relkind in ('r','p') and not c.relispartition
order by pg_total_relation_size(c.oid) desc
limit 15;

\echo
\echo '== 5. Partition families (children summed)'
select p.relname as parent, count(*) as partitions,
       pg_size_pretty(sum(pg_total_relation_size(c.oid))) as total,
       pg_size_pretty(sum(pg_indexes_size(c.oid))) as indexes,
       sum(greatest(c.reltuples, 0))::bigint as est_rows,
       sum(s.seq_scan) as seq_scans, sum(s.idx_scan) as idx_scans
from pg_inherits i
join pg_class p on p.oid = i.inhparent
join pg_class c on c.oid = i.inhrelid
join pg_stat_user_tables s on s.relid = c.oid
group by p.relname
order by sum(pg_total_relation_size(c.oid)) desc;

\echo
\echo '== 6. Large indexes never scanned (candidates, see docs/database.md)'
select s.relname as "table", s.indexrelname as "index", pg_size_pretty(pg_relation_size(s.indexrelid)) as size
from pg_stat_user_indexes s
join pg_index i on i.indexrelid = s.indexrelid
where s.idx_scan = 0 and not i.indisunique and pg_relation_size(s.indexrelid) > 50 * 1024 * 1024
order by pg_relation_size(s.indexrelid) desc
limit 12;

\echo
\echo '== 7. Active queries now: by wait type, and anything running longer than 10 s'
select coalesce(wait_event_type, 'cpu') as waiting_on, count(*) as active
from pg_stat_activity
where datname = current_database() and state = 'active' and pid <> pg_backend_pid()
group by 1 order by 2 desc;
select pid, (now() - query_start)::interval(0) as running_for, left(regexp_replace(query, '\s+', ' ', 'g'), 110) as query
from pg_stat_activity
where datname = current_database() and state = 'active' and pid <> pg_backend_pid()
  and now() - query_start > interval '10 seconds'
order by query_start
limit 8;

\echo
\echo '== 8. Queue and workers (River): jobs by queue and state'
select queue, state, count(*) from river_job group by 1, 2 order by 1, 2;
\echo '   completed in the last hour: count, average and p95 run seconds, average wait seconds before start'
select args->>'kind' as kind, count(*) as jobs,
       round(avg(extract(epoch from finalized_at - attempted_at))::numeric, 1) as avg_run_s,
       round((percentile_cont(0.95) within group (order by extract(epoch from finalized_at - attempted_at)))::numeric, 1) as p95_run_s,
       round(avg(extract(epoch from attempted_at - scheduled_at))::numeric, 0) as avg_wait_s
from river_job
where state = 'completed' and finalized_at > now() - interval '1 hour'
group by 1 order by 2 desc limit 10;
select args->>'kind' as running_kind, count(*) as running_now from river_job where state = 'running' group by 1 order by 2 desc;

\echo
\echo '== 9. Crawl throughput: Flickr calls per hour (result rows), quota waits (last 24 h)'
select to_char(date_trunc('hour', called_at), 'MM-DD HH24"h"') as hour,
       count(*) filter (where outcome in ('success','failure')) as flickr_calls,
       count(*) filter (where outcome = 'quota_wait') as quota_waits
from api_call_log
where called_at > now() - interval '24 hours'
group by date_trunc('hour', called_at)
order by date_trunc('hour', called_at);

\echo
\echo '== 10. Pending work (estimates from statistics; add -v deep=1 for exact counts)'
select relname as "table", n_live_tup as est_live, n_dead_tup as dead from pg_stat_user_tables where relname in ('work_items','river_job','crawl_interests','outbox_events') order by relname;
\if :{?deep}
\echo '   exact work items by state'
select state, count(*) from work_items group by 1 order by 2 desc;
\endif
