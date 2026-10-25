# Temporary media storage

Temporary media is stored through the `media/app.BlobStore` port. The current
workstation adapter stores files under `BLOB_LOCAL_DIR` and enforces the
`BLOB_MAX_BYTES` limit; set the limit to `0` to disable the cap. `Usage` reports
regular-file bytes and `Full` signals when the configured limit has been
reached.

The media role runs the asset cleaner immediately at startup and every five
minutes afterward. It selects expired asset rows, removes each blob, and then
deletes that row. Failed blob deletions leave their rows available for retry.
Invalid or empty blob keys are treated as already gone so they cannot block
newer expired rows. A failed cleanup pass is logged and retried at the next
interval. Local `PresignGet` returns a file URL with an informational expiry;
the cleaner enforces retention rather than the URL.

The S3 adapter uses minio-go and supports Garage-compatible endpoints. Set
`BLOB_DRIVER=s3` with `S3_ENDPOINT`, `S3_REGION`, `S3_BUCKET`, `S3_ACCESS_KEY`,
and `S3_SECRET_KEY` to enable it; `S3_USE_PATH_STYLE` selects path-style
requests. `BLOB_MAX_TTL` defaults to 24 hours. At startup the adapter ensures
the bucket exists and installs a lifecycle rule that expires objects after the
maximum TTL plus one day. The bucket cleaner still removes expired asset rows
and blobs every five minutes.

The S3 adapter's `Usage` sums object sizes and `Full` reports whether the
configured `BLOB_MAX_BYTES` cap has been reached. The optional S3 integration
test uses a pre-provisioned dedicated bucket when `S3_TEST_ENDPOINT`,
`S3_TEST_BUCKET`, `S3_TEST_ACCESS_KEY`, and `S3_TEST_SECRET_KEY` are set.
