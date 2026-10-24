# XFlickr environment (read this first; it overrides any ticket text that disagrees)

XFlickr is developed and run on one WSL workstation (`Workstation`, 192.168.2.10) that hosts a shared Docker stack. Nothing in this project starts its own database or search container for development or tests.

## Shared services (Docker network `workspaces-shared`)

| Need | Use | From a container | From the host |
|---|---|---|---|
| PostgreSQL 18 + pgvector | databases `xflickr` (the single live database), `xflickr_test`, `xflickr_wt<id>` per worktree | `postgres:5432` | `127.0.0.1:15435` |
| Redis 8 | live index 7 (prefix `xflickr:`), test index 8 (prefix `xflickr_test:`), worktrees use the test index with prefix `xflickr_wt<id>:` | `redis:6379` | `127.0.0.1:16382` |
| Search | shared Elasticsearch 9.x (port 19200), not OpenSearch; behind the `SearchIndex` port, pgvector is the fallback | `elasticsearch:9200` | `127.0.0.1:19200` |
| Logs/metrics | shared OpenObserve | `openobserve:5080` | `127.0.0.1:15080` |
| Mail | shared Mailpit (SMTP `mailpit:1025`) | | UI `127.0.0.1:18025` |

Connection values come from `scripts/docker/shared env-project xflickr --mode live|test|wt:<id> --output <file>` (run from the Workspaces root). The generated `.env` holds the shared root account: it is git-ignored and must never be printed, logged or committed. Run `scripts/docker/shared check-project xflickr --env-file <file> --mode ...` before any migrate, test or seed.

## Rules that replace ticket wording

1. No testcontainers and no per-job containers. Integration tests use `xflickr_test` or, in a ticket worktree, the worktree database `xflickr_wt<id>`; provision with `scripts/docker/shared provision-project xflickr --db <id> --postgres` (AI Flow does this per ticket).
2. No dev Docker Compose for databases, search, Garage or Gatus. `make dev-up` only checks that the shared services answer and that the project databases exist. The only container XFlickr owns is its own application container (one image, roles comma separated).
3. Temporary download files and thumbnails use a directory on the WSL disk behind the `BlobStore` port (local filesystem adapter first); the S3 adapter is optional later. Garage is not used.
4. Uptime checks use OpenObserve alerts; Gatus is not used.
5. Branches: `develop` is the only long-lived branch for now (there is no `master` yet). Each ticket is built in its own worktree and merged into `develop` when everything passes. `develop` runs on WSL against the single database `xflickr` with real crawled data that the owner inspects.
6. The Flickr API key and secret are entered only in the application UI (setup wizard and Keys screen), stored encrypted. There is no CLI or `.env` path for them and none may be added.
7. Ollama (captions, embeddings) runs natively on the WSL host, not in Docker. Tests use the fake Captioner/Embedder.
8. Deployment to the owner's other servers is out of scope.
9. Toolchains (Go, Node) are not installed on the WSL host; they run in the shared containers through `/home/joos/Workspaces/scripts/docker/runtime go|node ...` (run it from inside the repository or worktree). Every helper in `docker/scripts/` must call `docker/scripts/run.sh <tool> <args>`, which uses that wrapper when the environment variable `WORKSPACES_RUNTIME` points to it and the native tool otherwise (GitHub Actions). `make check` must therefore work in both places.
10. Work is done in a git worktree per ticket with its own database `xflickr_wt<id>` (the generated `.env` names it). Only a passing ticket is merged into `develop`.
