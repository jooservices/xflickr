# Temporary media storage

Temporary media is stored through the `media/app.BlobStore` port. The current
workstation adapter stores files under `BLOB_LOCAL_DIR` and enforces the
`BLOB_MAX_BYTES` limit; set the limit to `0` to disable the cap. `Usage` reports
regular-file bytes and `Full` signals when the configured limit has been
reached.

The media role runs the asset cleaner immediately at startup and every five
minutes afterward. It selects expired asset rows, removes each blob, and then
deletes that row. Failed blob deletions leave their rows available for retry.

The workstation environment uses local storage and does not run Garage. A
remote object-store adapter and its lifecycle rules belong to an environment
that provides that service.
