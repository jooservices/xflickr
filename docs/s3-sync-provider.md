# S3 sync provider existence check

This slice adds an unconnected `ObjectChecker` for S3-compatible storage. The
caller supplies an already configured MinIO client. The checker calls
`StatObject` for the supplied bucket and key, then compares the returned ETag
with the expected ETag. The MinIO client may perform additional HTTP requests
for bucket-region discovery or SDK-configured retries; the checker does not
limit those requests.

Bucket and key values are validated for blank input but passed to MinIO
unchanged. One outer pair of double quotes is removed from each ETag before an
exact, case-sensitive comparison. An S3 ETag identifies an object version; it
is not treated as an MD5 or SHA-256 digest, including for multipart uploads.

An HTTP 404 from the object HEAD request means that the object is reported as
absent. S3-compatible services may not reveal through HEAD whether the bucket or
the object is missing. Authentication and throttling errors are mapped to the
sync application's shared sentinels. Other provider errors are returned in a
generic form without the provider response body. The checker does not accept,
store, or log credentials and its constructor does not make a network request.

## Deferred work

- S3 target configuration, credential encryption, and endpoint, bucket, and
  prefix validation.
- Multipart upload, size and SHA-256 verification, album handling, deletion,
  and complete error mapping.
- A full `StorageProvider` implementation, `providertest` support, and wiring
  into the sync engine.
- User-facing settings, API routes, and UI for connecting an S3 destination.

This checker is only the first adapter slice; an S3 sync provider is not
available to users yet.
