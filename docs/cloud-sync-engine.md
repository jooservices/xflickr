# Cloud sync engine

## Scope

This first slice uploads an already fetched temporary blob. It does not fetch
CDN content, select photos, create jobs, persist sync state, or connect a real
provider. The caller exclusively owns the blob during `Upload` and must retain
any valid remote ID returned, including with an error.

## Saved target listing foundation

The sync target application service can return one keyset page of metadata
owned by a user. Pages use the target ID cursor and are limited to 100 rows;
the PostgreSQL query uses the `(user_id, id)` index and never selects the
sealed token. This internal foundation does not add an HTTP route, provider
catalogue, or UI behavior. Those remain part of a later API-backed screen
slice.

## Upload and cleanup order

The engine validates context, blob key, and album ID. A supplied remote ID is
checked before capability checks or blob access. If it exists, the engine skips
opening and uploading and removes the blob if present; a missing blob counts as
already cleaned. If the ID is absent, the engine checks capabilities, opens the
blob once, validates metadata, uploads it, then checks the new ID with `Exists`.
Only a confirmed item and successfully closed reader allow blob deletion.

## Verification and retries

`Exists` establishes existence only; it does not verify remote checksum or
size. The in-memory fake checks both against uploaded bytes. A real provider
needs remote verification support before it is connected to this engine.

Upload or existence errors and a false existence result leave the blob in
place. Provider errors remain discoverable through `errors.Is`, including rate
limits. Close or delete errors return the known remote ID with an error. A retry
with that ID checks existence before another upload. Automatic backoff,
persistence, crash recovery, and concurrent duplicate protection are later work.
