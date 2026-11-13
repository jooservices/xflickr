# XFlickr environment (read this first; it overrides any ticket text that disagrees)

XFlickr is developed and run on the Dev Workspaces VM (`dev-workspaces`, 192.168.10.100). Go and Node run natively on the host, and the VM hosts the shared services below for local development and tests. Local commands do not start their own database, cache or search service. GitHub Actions runs on separate hosted runners and provisions workflow service containers for integration tests. Every local shared service listens on loopback only.

## Shared services (on the VM)

| Need | Use | Host address |
|---|---|---|
| PostgreSQL 18 + pgvector | databases `xflickr` (the single live database), `xflickr_test`, `xflickr_wt<id>` per worktree | `127.0.0.1:15435` |
| Redis 8 | live index 7 (prefix `xflickr:`), test index 8 (prefix `xflickr_test:`), worktrees use the test index with prefix `xflickr_wt<id>:` | `127.0.0.1:16382` |
| Search | shared Elasticsearch 9.x with security on, not OpenSearch; behind the `SearchIndex` port, pgvector is the fallback | `127.0.0.1:19200` |
| Logs/metrics | shared OpenObserve | `127.0.0.1:15080` |
| Error reporting | shared Sentry, project `xflickr`; the DSN goes in `SENTRY_DSN` | `127.0.0.1:19000` |
| Mail | Mailpit email sink (SMTP, no authentication); development and tests never send real mail | SMTP `127.0.0.1:11025`, UI `127.0.0.1:18025` |

Credentials live in the private `Workspaces/.env.docker`. Never print it, never commit it, and never `source` it in a shell: some values contain characters the shell interprets. Tools read it with a plain parser. `docker/scripts/worktree_env.py` does that for worktrees (see rule 10).

## Rules that replace ticket wording

1. Local development and tests use the VM's shared services. Do not use
   testcontainers or per-job or per-worktree service containers on the VM.
   Local integration tests use the shared PostgreSQL service with a freshly
   recreated worktree database, `xflickr_wt<id>`. `make test-integration`
   recreates only the current worktree database and reapplies migrations
   before running the suite. Preserve any fixture data you need before running
   it. GitHub Actions runners have no shared VM services, so each integration
   shard uses a fresh `pgvector/pgvector:pg18` PostgreSQL service container.
   Integration tests do not start containers themselves. A chaos or load test
   that needs isolation may use a one-off container if its ticket says so.
2. No dev Docker Compose for databases or search. `make dev-up` runs `docker/scripts/worktree_env.py --check`: it confirms the worktree `.env` selects its own database and that PostgreSQL, Redis and search answer. The only container XFlickr may own is a future application container (one image, roles comma separated).
3. Temporary download files and thumbnails use a directory on the VM disk behind the `BlobStore` port (local filesystem adapter first); the S3 adapter is optional later. Presigned ZIP links use `DOWNLOAD_LINK_TTL` (15m by default), which cannot exceed `BLOB_MAX_TTL`.
4. Uptime checks use OpenObserve alerts; external monitoring on the ESXi monitor server is a deferred ticket.
5. Branches: `develop` is the only long-lived branch in use. Each ticket is built in its own worktree and merged into `develop` (squash) when review, CI and every PR comment are resolved. `master` exists but is stale and unused until the first release.
6. The Flickr API key and secret are entered only in the application UI (setup wizard and Keys screen), stored encrypted. There is no CLI or `.env` path for them and none may be added.
7. Ollama (captions, embeddings) runs natively on the host when available; tests use the fake Captioner and Embedder. AI features are for the owner only in release 1.
8. Deployment to other servers is out of scope for release 1.
9. Go, Node, Python and golangci-lint are pinned in `.tool-versions`. CI reads
   those pins for its setup actions; `make check-tool-versions` warns when the
   local Go, Node or Python runtime differs. `backend/go.mod` and the root
   `Dockerfile` must keep the same Go version; `make check-go-version` verifies
   them. `docker/scripts/run.sh` runs Go and Node directly, so the Make targets
   work on the VM and on GitHub Actions. If `WORKSPACES_RUNTIME` is set it is
   used instead.
10. Work is done in a git worktree per ticket with its own database. Create it from the Workspaces root with `python3 scripts/git/main.py create --repo JOOservices/projects/xflickr --company JOOservices --ticket JOOXF-<n> --description "<slug>" --base develop`, then run `python3 docker/scripts/worktree_env.py <id>` inside the worktree, where `<id>` is the ticket key in lowercase without the hyphen (`JOOXF-294` becomes `jooxf294`). That creates `xflickr_wt<id>`, writes a private `.env` with a fresh master key (both `DB_NAME`/`DB_USER` and the `DB_DATABASE`/`DB_USERNAME` aliases, plus the worktree path so a copied `.env` is refused), and refuses the live and shared test databases. Running it again keeps the master key.
11. The develop runtime on the VM is a native process built from `backend/cmd/xflickr` with its own git-ignored `.env.develop` (it holds the encryption master key: never delete or replace it). It uses the live database `xflickr`. Only the develop runtime touches the live database. It is started by hand for now; `make develop-up` and `docs/develop-environment.md` still describe the older Docker flow of the WSL workstation and do not apply on the VM until JOOXF-315 (native develop runtime) replaces them.
12. The mandatory Gitleaks scan before push reads `.gitleaksignore`, which lists the 24 reviewed false positives of the history by fingerprint (test constants, RFC 5849 example tokens, a generated OpenAPI blob). A new finding is fixed or rejected, never added there without a lead review of the value.

## Code generation

`make gen` (`docker/scripts/gen.sh`) regenerates every checked-in generated
artifact from its source of truth:

| Step | Source | Tool pin | Output |
|---|---|---|---|
| OpenAPI bundle | `contracts/openapi/` | Redocly via `web` npm | `.tmp/openapi.bundle.yaml` |
| Go HTTP server | bundled OpenAPI | `go tool oapi-codegen` in `backend/go.mod` | `backend/internal/platform/api/gen/` |
| PostgreSQL models and queries | `backend/migrations` + module `queries/` | `go tool sqlc` (`github.com/sqlc-dev/sqlc` in `backend/go.mod`, currently v1.31.1) | `backend/internal/{platform,catalog}/adapters/postgres/gen/` |
| TypeScript client and Zod | bundled OpenAPI | Orval in `web/package.json` | `web/src/shared/api/gen`, `web/src/shared/api/zod` |
| Other `go generate` | `//go:generate` directives | module toolchain | package-local generated files |

sqlc is a build tool only: it is declared under `tool` in `backend/go.mod` and
is not a runtime dependency. Configure both sqlc modules in
`backend/sqlc.yaml`. Prefer sqlc overrides there for types the generator cannot
map (for example PostgreSQL extensions); never hand-edit generated files.

CI runs `make gen` then `make gen-stale`. The stale check fails when
regeneration changes any tracked or untracked file, so a migration or query
change that is not followed by committed sqlc output cannot merge. Running
`make gen` twice on a clean tree must produce no diff.

## Migration versions

`make precheck` fails when two files in `backend/migrations` share a version or
a `.sql` file is not named `YYYYMMDDHHMMSS_name.sql`; CI runs it too. Run
`make next-migration NAME=add_thing` to print a free filename: it runs
`git fetch origin` and picks a version above the working tree, `HEAD`, and every
local and `origin/*` branch, so no version is reused.

## Password hashing

The API uses Argon2id for account passwords. The hash cost is configurable with
these environment variables (defaults are also listed in `.env.example`):

| Variable | Default | Meaning |
|---|---:|---|
| `ARGON2ID_MEMORY` | `65536` | Memory in KiB (64 MiB). |
| `ARGON2ID_ITERATIONS` | `3` | Number of Argon2id passes. |
| `ARGON2ID_PARALLELISM` | `2` | Number of lanes/threads. |
| `ARGON2ID_SALT_LENGTH` | `16` | Random salt length in bytes. |
| `ARGON2ID_KEY_LENGTH` | `32` | Derived hash length in bytes. |

Keep these values consistent across API instances. Increasing them raises the
CPU or memory cost of registration and login.

## Web test report in CI

When a `web-test` shard fails, its vitest JUnit report is uploaded as a
`vitest-report-*` run artifact (kept 7 days). Open the failed run and download
the matching shard from the Artifacts section of the summary page.
