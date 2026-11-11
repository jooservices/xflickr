# Dropbox metadata check

The first Dropbox adapter slice exposes `dropbox.Client.CheckContent` for a
known file path. It sends one `files/get_metadata` request and returns true
only when Dropbox reports a file whose `content_hash` matches the supplied
hash. A path-not-found response and a different hash return false; malformed
or unexpected metadata returns an error.

Paths start with `/` and are interpreted relative to the App folder associated
with the injected token. Team namespaces and team-selection headers are not
used. The caller supplies a 32-byte hexadecimal Dropbox content hash. This is
Dropbox's block-based content hash, not the SHA-256 digest in `sync/app.Asset`.

The client accepts an injected HTTP client and access token. It bounds response
bodies to 1 MiB and maps authentication and rate-limit responses to the sync
application sentinels. Errors do not include the token or response body.

This helper checks one known path only. It does not search the account, compute
Dropbox content hashes, upload files, or implement `StorageProvider`. It is not
connected to the sync engine; JOOXF-147 remains a dependency for the full
provider workflow.
