# ADR 0008: Cross-module imports

- Status: Accepted
- Date: 2026-11-12
- Ticket: JOOXF-451

## Context

`docs/engineering-standards.md` said a module reaches another only through that
module's `app` package or through the outbox. The code did not follow it:
`crawl` imported `catalog`, `quota` and `settings` domain types, `media` imported
`sharing` domain types, and `crawl/domain` imported `catalog/domain`. The arch
file and the code disagreed, and `make arch-lint` failed with 26 notices.

Rewriting every shared type behind an `app` DTO would add a conversion layer for
plain value objects with no behaviour, which the KISS and YAGNI rules reject.
Letting one module import another module's adapters would reintroduce the
coupling that dependency inversion exists to prevent.

## Decision

A module may import another module's:

- `domain` package: plain types and value objects;
- `app` package: use-case interfaces and their types.

Rules that stay in force:

- A module never imports another module's `adapters`.
- A `domain` package imports no `app` or `adapters` package, of its own module
  or of another.
- A behaviour call across modules goes through the other module's `app`
  interface. A module may also react to another module's outbox event.
- Composition (wiring dependencies to implementations) stays in
  `internal/platform/roles` and the `wiring` component. No other package
  wires modules together.
- Shared `internal/platform/...` packages, other than `roles`, import no module
  package.

`.go-arch-lint.yml` declares the undeclared modules (`home`, `ops/detectors`,
`search`) and each cross-module edge that code needs, and nothing else.
`make arch-lint` runs in CI job `lint`. The depguard rules in `.golangci.yml`
keep forbidding infrastructure packages in `domain` and `app`. They do not
repeat the module rule, because `arch-lint` already enforces it.

The existing violations were fixed as follows:

- `platform/ops/adapters/http` imported `ops/app`, and `platform` may not import
  a module. It was moved to `internal/ops/adapters/http` (a pure move; the
  import paths of its callers changed).
- `quota/adapters/http` declared its own `CallStatsService` interface, identical
  to `app.CallStatsReader`. The handler now uses the app port directly. This is
  a type-only change with the same method set.

## Consequences

- A module can use another module's value types without a copy. Its use cases
  are called through the other module's `app` interface.
- The lint no longer hides the rule behind a per-file exception: a new edge
  becomes a reviewed line in `.go-arch-lint.yml`, and an adapter import between
  modules fails the build.
- Edges are declared when code needs them. This avoids a 14-module grid of
  unused declarations. Adding an edge the rule permits is a config change, not
  a rule change.
- Follow-up: `platform/ops/adapters/postgres` and `platform/ops` (monitor and
  status) still hold ops code under `platform/`. The arch file allows that
  because they import no module package. Moving them under `internal/ops/` is a
  separate ticket. It first decides how the two ops stores
  (`ops/adapters/postgres` and `platform/ops/adapters/postgres`) relate.

## Alternatives considered

- **App-only imports (the previous rule):** every shared type would need a DTO
  in the owning `app`. That is a conversion layer with no behaviour; rejected
  under KISS and YAGNI.
- **Allow adapters across modules:** would let one module depend on another's
  storage or HTTP code; rejected under dependency inversion.
- **`commonComponents` in go-arch-lint:** would also let `domain` import another
  module's `app`, which the rule forbids; rejected.
- **A depguard rule per module pair:** duplicates `arch-lint` and needs an entry
  for each pair; rejected under DRY.
