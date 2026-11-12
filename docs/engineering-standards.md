# Engineering standards

These rules apply to every change in XFlickr, by people and by AI developers.
`AGENTS.md`, `docs/ENVIRONMENT.md` and `docs/rest-api.md` complement them. A
reviewer rejects a change that breaks them even when the tests pass.

## Principles

- SOLID, KISS, DRY. One place for a rule: never validate or compute the same thing
  in two layers. Small functions and types with one reason to change.
- Use a design pattern only where it removes a real problem. No speculative
  abstraction, no unused extension points, no dead code.
- Idiomatic Go of the version in `backend/go.mod`: composition over inheritance,
  small interfaces defined by the consumer, explicit errors, no package-level
  mutable state, no `init` side effects.
- Every operational setting that an administrator may want to change belongs in the
  settings registry (admin editable, audited), not in a constant or an environment
  variable. Environment variables are for infrastructure endpoints and secrets.
- Flickr quota is precious: no call that is not needed, no retry of a request known
  to be deleted or unreachable, no unbounded scan of the large tables.

## Backend structure

- A feature module lives in `internal/<module>/` with `domain/` (rules and types, no
  infrastructure imports), `app/` (use cases that depend on ports, not on adapters)
  and `adapters/<technology>/` (HTTP, PostgreSQL, Redis, Flickr, filesystem, fakes).
  Dependencies point inward: domain <- app <- adapters. A module reaches another
  module only through that module's `app` package or through an outbox event.
  `make arch-lint` and the `depguard` rules enforce it; never add an exception.
- Shared infrastructure lives in `internal/platform/<name>/` and stays generic;
  business rules belong to their module. Public packages for outside use live in
  `pkg/`.
- Ports (interfaces) sit next to the code that needs them, are small, and have a
  compile-time check in the adapter: `var _ app.Port = (*Adapter)(nil)`.
- Constructors are `New...` functions that validate their arguments and return an
  error; they take dependencies explicitly. Wiring happens in one composition place
  per role (`internal/platform/roles`), not inside packages.
- **New SQL uses sqlc** (owner decision 2026-11-12). Write the statement in the module's
  `queries/*.sql`, run `make gen`, and call the generated method. Hand-written SQL in an
  adapter is allowed only as a recorded exception with a one-line reason next to it:
  runtime-built dynamic SQL, or a pgx batch pipeline that sqlc cannot express (the
  two-statement upsert batches are an example). Existing hand-written SQL moves to sqlc
  under epic JOOXF-439; touch it only to move it or when the ticket needs it. Review
  rejects new hand-written SQL without a reason.
- SQL goes through pgx with parameters, never string concatenation. Schema changes
  are goose migrations named `YYYYMMDDHHMMSS_description.sql` with `+goose Up` and
  `+goose Down`; sqlc queries live with their module. Keep queries bounded: the
  call log and catalog tables hold millions of rows. `make gen` runs the pinned
  sqlc from `backend/go.mod` against `backend/sqlc.yaml`; commit the regenerated
  models with the migration or query change that caused them. Never edit sqlc
  output by hand — use overrides in `backend/sqlc.yaml` when a type needs a
  mapping.
- HTTP follows `docs/rest-api.md`; the contract in `contracts/openapi` is the
  source of truth and generated code is never edited by hand. Generate with the
  toolchain of `backend/go.mod` (see `docs/ENVIRONMENT.md`); commit generated
  files when the contract or its generator toolchain changes.

## Code style

- `gofmt`, and every linter in `.golangci.yml` must pass: errcheck, govet,
  staticcheck, gocritic, revive, gosec, cyclop (complexity at most 15), wrapcheck,
  errorlint, depguard, misspell. Do not add `nolint` without a reason in the same
  line and the lead's approval.
- Names: short and exact; exported identifiers have a doc comment that starts with
  the name; every package has a `// Package x ...` comment; no stutter
  (`mail.Sender`, not `mail.MailSender`).
- Errors: wrap with context and the package prefix, `fmt.Errorf("media: pack
  download %d: %w", id, err)`; sentinel errors are `var ErrX = errors.New("pkg:
  ...")` and are tested with `errors.Is` or `errors.As`; never compare error
  strings; never swallow an error; never put secrets in an error.
- Context: `ctx context.Context` is the first parameter, is passed down, and is
  honoured for cancellation and deadlines; do not store it in a struct.
- Logging: `log/slog` with the context variants (`WarnContext`, `ErrorContext`),
  stable snake_case field names, never a secret or a token (the redaction layer is
  a backstop, not a licence).
- Concurrency: every goroutine has an owner, a stop condition and a test; use the
  race detector.
- Web: TypeScript strict, ESLint and Prettier clean, API access only through the
  generated client, network mocked with MSW in unit tests, screens built exactly
  from the approved spec in `docs/ui/views` (all states, EN and VI copy, the
  `data-testid` values), accessibility rules of `docs/ui/a11y.md`.
- Python or shell helpers: standard library, small functions, tested like code.
- English only for code, comments, documentation, commits and Jira.

## Tests

- Write the failing test first. Every acceptance criterion has at least one
  automated test that fails without the change.
- Unit tests: table-driven with `t.Run`, descriptive names without underscores
  (`TestSizesPlaceholderIsClassifiedOnceWithoutFlickr`), `t.Helper()` in helpers,
  `t.Cleanup`, `t.Context()`, fakes written for the port. They need no database,
  Redis or network. Coverage: at least 85 percent per file you touch, measured by
  statements; no test that only exists to raise the number (assert behaviour, not
  that a function was called).
- Integration tests: gated by `XFLICKR_INTEGRATION_TEST=1`, use only the worktree
  database created by `docker/scripts/worktree_env.py`, never the live database,
  and clean up after themselves. Run the integration tests of every package you
  touch and `internal/platform/roles`; the full `make test-integration` passes on a
  fresh database before a pull request.
- Time and randomness are injected; never compare a Go time with a stored timestamp
  without truncating to the stored precision; no sleeps to fix ordering.
- E2E (Playwright): journeys are built from the shared step and scenario library,
  one scenario per user-visible flow, zero console errors, every screen covered.
- A flaky test is a defect: fix the cause, never retry or skip it.

## Documentation and delivery

- One `docs/<topic>.md` per feature, linked with one line from `docs/README.md`;
  update `docs/SPEC.md` when a business rule changes and add an ADR for a lasting
  architecture decision. Prose wraps at about 80 columns.
- Commits are small and logical, Conventional Commits in English:
  `type(scope): Subject (JOOXF-n)`. Pull requests target `develop`, use the
  template sections (What's changed, Why changed, Deployment), are never drafts,
  never mention a model name, and contain the test evidence.
- Before the pull request: `make lint`, tests of the touched packages, `make
  arch-lint` (no new notice), the Sonar and Gitleaks scans, and a rebase on
  `origin/develop`.

## What the reviewer checks

Structure and boundaries; one responsibility per type and function; no duplicated
rule; naming; error wrapping and handling; logging and secrets; context use;
settings in the registry; SQL bounds and migrations; quota and Flickr call impact;
tests that assert behaviour (not tautologies), cover every acceptance criterion,
and run reliably; generated and unrelated files absent from the diff; docs and
ADR updated; security (input validation, authorization, secrets); accessibility
and copy for screens.
