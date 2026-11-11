# Google Drive provider slice

The current Drive adapter slice exposes `FolderClient.EnsureAlbum`. It trims
the supplied name, searches for a non-trashed folder with that name directly
under My Drive root, and returns the first non-empty ID. If no folder is found,
it creates one in the root and returns the ID from Drive.

Calls are idempotent when made sequentially with the same name. Concurrent
calls can create duplicate folders. Shared drives and nested paths are not
supported. The caller supplies an already configured and authenticated Drive
service; this slice does not perform OAuth or store credentials. Errors map
authentication and rate-limit responses to the sync application sentinels and
do not include Drive response bodies.

OAuth consent and encrypted token storage, resumable upload, upload
verification, `Exists`, `Delete`, complete provider contract tests, engine
integration, and user interface work are deferred to later slices.
