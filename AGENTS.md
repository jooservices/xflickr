# XFlickr contributor guide

Read `docs/ENVIRONMENT.md` before running commands. It defines the shared
services, worktree database rules, and runtime wrappers; follow it whenever
other guidance conflicts.

## Repository layout

- `backend/cmd/xflickr/`: executable entry point and role startup.
- `backend/internal/`: private backend packages. `platform/` holds cross-cutting
  infrastructure; feature modules own their domain and application logic.
- `backend/pkg/`: intentionally public Go packages, such as Flickr transport
  types.
- `backend/migrations/`: PostgreSQL schema migrations; `backend/sqlc.yaml`
  defines SQL code generation.
- `web/`: web application and frontend assets.
- `contracts/openapi/`: HTTP API contracts.
- `deploy/`, `docker/`: application deployment assets and helper scripts.
- `docs/`: product rules, feature documentation, and architecture decision
  records.

## Architecture and module boundaries

Use ports and adapters with dependencies flowing inward: **domain <- app <-
adapters**. The domain contains business rules and has no infrastructure
dependencies. The application layer coordinates use cases through domain and
port interfaces. Adapters implement those ports for HTTP, PostgreSQL, Redis,
search, filesystem storage, and external services.

Modules communicate through another module's application API or domain events.
Do not import another feature module's adapters or persistence implementation.
Keep shared platform code generic; business rules belong to their feature
module.

## Commands and runtimes

Run commands from this worktree. Go and Node are provided by the shared runtime
wrappers described in `docs/ENVIRONMENT.md`; do not assume host toolchains are
installed. Examples:

```sh
/home/joos/Workspaces/scripts/docker/runtime go go test ./...
/home/joos/Workspaces/scripts/docker/runtime node npm test
make lint
make check
```

Run repository make targets when available. Use `make dev-up` only to check
shared services; XFlickr does not start its own database, search, or cache
containers. Integration tests use the shared PostgreSQL and Redis services
configured in the generated worktree `.env`, after the environment check
described in `docs/ENVIRONMENT.md`.

## Tests

- Unit tests must provide at least 85% coverage for every touched package. For
  Go packages, measure statement coverage.
- Adapter integration tests use the shared services and worktree database. Never
  use testcontainers or start per-job or per-worktree service containers;
  `docs/ENVIRONMENT.md` is authoritative.
- Web tests use MSW for network boundaries and Playwright for browser flows.
- Keep unit tests independent of external services. Use fakes for network,
  captioning, and embedding ports.
- Never print `.env` contents. Never point tests or migrations at the live
  `xflickr` database.

## Documentation

Add one `docs/<topic>.md` file for each feature and add one line linking it from
`docs/README.md`. Update `docs/SPEC.md` when business rules change and add an
ADR for durable architecture decisions.

## Changes and commits

- Implement only the requested ticket scope and acceptance criteria.
- Commit in English using Conventional Commits: `type(scope): Subject
  (JOOXF-<ticket>)`.
- English is the only language for source, tests, comments, and documentation.
- Never commit `.env`, credentials, API keys, tokens, or other secrets. Flickr
  API key and secret are entered only in the UI and stored encrypted; do not add
  a CLI or environment-variable path for them.
