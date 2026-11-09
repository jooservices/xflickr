# Production runtime on the ESXi services

Since 2026-11-09 the main git clone of the Dev Workspace VM (branch `develop`)
is the production runtime of XFlickr: the code stays on that VM, the data
services are the production services on ESXi. Worktrees stay on the VM with
their own PostgreSQL and Redis and never reach production.

| Service | Production | Worktrees and tests |
|---|---|---|
| PostgreSQL | 192.168.10.12, database `xflickr`, role `xflickr_app` | VM-local cluster, databases `xflickr_wt*` |
| Redis | 192.168.10.14, database 2, ACL user `xflickr`, prefix `xflickr:` | VM-local Redis, test indexes |
| Sentry | 192.168.10.17, organization `jooservices`, project `xflickr` | disabled (`SENTRY_DSN` empty) |

The old local database (`xflickr` on the VM) stays untouched as staging.

## Accounts (names only, values live in `.env.homelab` and `.env.develop`)

- PostgreSQL role `xflickr_app`: `LOGIN`, not a superuser, `CONNECTION LIMIT
  60`, owner of the `xflickr` database only; the database has `CONNECT`
  revoked from `PUBLIC`; extensions `citext`, `vector`, `plpgsql` were created
  by a superuser. The connection uses `DB_SSLMODE=require`.
- Redis ACL user `xflickr`: `~xflickr:* &* +@all -@dangerous +info`, same shape
  as the other consumers; persisted in `redis.conf`. The application selects
  it with `REDIS_USERNAME`.
- Credential keys in `.env.homelab`: `VM_POSTGRES_XFLICKR_{DB,USER,PASSWORD}`,
  `VM_REDIS_XFLICKR_{USER,PASSWORD,DB,PREFIX}`, `VM_SENTRY_XFLICKR_DSN`.

## Data move (done while the crawl is stopped, so there is no delta)

1. Back up what will change to the NAS (`scripts/backup/vm_backup.py`); only
   continue on `BACKUP SUCCESSFUL`.
2. Dump the local database in directory format:
   `pg_dump -Fd -j 2 -Z zstd:3 -d xflickr -f <dir>` (idle I/O class).
3. Restore into the NEW empty database, never into an existing one:
   `pg_restore -j 2 --no-owner --no-privileges -L <list without EXTENSION
   entries> --exit-on-error -h 192.168.10.12 -U xflickr_app -d xflickr <dir>`
   (extensions already exist; the role cannot create them). River jobs are
   ordinary tables and move with the database. Redis is not copied: the rolling
   quota ledger is rebuilt from `api_call_log` at first start (ADR 0007).
4. `ANALYZE`, then `xflickr migrate up` for the migrations added since the dump.
5. Verify: `docker/scripts/compare-db.sh --src-env <local env> --dst-env <prod
   env>` compares the exact row count of every table (exit 1 on any
   difference), plus the extension, partition, index and constraint counts.

## Switch and start

1. Keep `.env.develop` as `.env.develop.local-backup` (mode 600).
2. In `.env.develop` set `DB_HOST/DB_PORT/DB_NAME/DB_USER/DB_PASSWORD`,
   `DB_SSLMODE=require`, `REDIS_HOST/REDIS_PORT/REDIS_DB/REDIS_USERNAME/
   REDIS_PASSWORD/REDIS_PREFIX`, `SENTRY_DSN`, `SENTRY_ENVIRONMENT=production`.
   Keep `XFLICKR_MASTER_KEYS` and `XFLICKR_MASTER_KEY_ACTIVE` unchanged: the
   stored Flickr credentials are encrypted with them.
3. Start with `.tmp/dev/restart-xflickr.sh` (keeps the previous binary, runs
   `migrate up`), with few workers first, then raise gradually.
4. Watch the monitor server (Prometheus, load and I/O of .12 and .14), the slow
   log of .12 and `docker/scripts/db-report.sh`.

## Stop conditions and rollback

Stop the crawl when the load of .12 stays above 6 for 5 minutes, another
database on .12 is affected, errors rise, or the quota is exceeded. Rollback:
stop the server, restore `.env.develop.local-backup`, start again against the
local database (kept for at least 7 days).

## Deploying later changes

After a merge to `develop`: `git pull` in the main clone and
`.tmp/dev/restart-xflickr.sh`; `migrate up` runs against production only when
the merge added migrations. Worktrees and tests must never use the production
hosts (`docker/scripts/worktree_env.py` refuses them).
