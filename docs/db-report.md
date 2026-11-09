# Database report

`docker/scripts/db-report.sh` prints a read-only snapshot of the xflickr
database so that a change to queries, indexes, workers or server settings can
be measured before and after. It was written for the 2026-11-09 audit
(JOOXF-369 to JOOXF-376) and is the source of the numbers quoted in those
tickets.

```sh
docker/scripts/db-report.sh --env-file JOOservices/projects/xflickr/.env.develop
docker/scripts/db-report.sh --env-file <file> --deep    # adds exact counts, slower
```

The env file is read line by line (`DB_HOST`, `DB_PORT`, `DB_USER`,
`DB_PASSWORD`, `DB_NAME`) and never sourced, so odd quoting cannot run code and
no other variable is used. The session opens with
`default_transaction_read_only=on` and a 60 second statement timeout. The SQL
lives in `docker/scripts/db-report.sql` and a test proves that it has no write
statement. The report never prints secrets.

## Sections

1. Server: version, uptime, size and the tuning settings that matter.
2. Checkpoints: timed versus requested. A high requested share means
   `max_wal_size` is too small.
3. Disk reads and cache hit per database, temporary file volume and deadlocks.
4. Largest tables: bytes per row (bloat shows as a large number against the
   real row width), dead tuple share, sequential scans and rows read, HOT
   update share.
5. Partition families with children summed.
6. Large indexes that were never scanned (see `docs/database.md`, added by JOOXF-371, for
   the review procedure).
7. Active queries by wait type and anything running longer than 10 seconds. A
   long `IO:DataFileRead` query is a scan that the plan should avoid.
8. Queue and workers: River jobs by queue and state, run time and wait time
   before start per job kind, jobs running now.
9. Crawl throughput: Flickr calls per hour for the last 24 hours and quota
   waits. Calls well below the hourly limit with pending work means the queue
   or the database is the bottleneck, not the quota.
10. Pending work estimates (exact counts with `--deep`).

## How to read it

Take a report before and after each deployment of a database or worker change
and keep both in the ticket. Statistics are cumulative since the last reset, so
compare rates (for example calls per hour, requested checkpoints share) and
let a full day pass after a change before judging the cumulative counters.
