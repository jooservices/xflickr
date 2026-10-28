# Cloud storage provider contract

`internal/sync/app.StorageProvider` is the application boundary for remote
storage adapters. The port is intentionally provider-neutral; real provider
implementations, account persistence, and sync orchestration belong to later
work.

## Shared types and errors

- `Capabilities` reports whether a destination supports user albums and remote
  deletion, plus an optional maximum asset size. A zero maximum means the
  provider publishes no size limit; a negative value is invalid.
- `Asset` carries a filename, content type, byte length, SHA-256 digest, and a
  stream. Callers must provide a fresh stream on every upload attempt. The
  adapter verifies that the bytes read match the declared size and digest.
- `RemoteAlbumID` and `RemoteItemID` keep provider identifiers distinct from
  local database IDs.
- `ErrRateLimited` identifies a retryable provider throttle, while
  `ErrAuthentication` identifies credentials that require repair. Adapters can
  wrap these sentinels so callers can use `errors.Is`.
- `ErrUnsupported` represents an operation the destination cannot perform. A
  provider without album support can map `EnsureAlbum` to its default
  destination; a provider without delete support returns `ErrUnsupported` from
  `Delete`.

## Adapter contract tests

Every adapter test package should call `providertest.Run` with a factory that
creates a fresh provider for each case. The shared suite checks valid
capabilities, idempotent album setup, upload and existence verification,
deletion behavior according to the advertised capability, rate-limit retry,
and identifiable authentication failures. `Scenario.UploadErrors` lets an
adapter test deterministic upload errors without calling a remote service.

The in-memory adapter in `internal/sync/adapters/fake` is the reference
implementation for the contract. It validates asset size and SHA-256, stores
the uploaded bytes in memory, and supports failure injection. Contract tests
must use local data only; provider credentials and real remote accounts are
outside this boundary.
