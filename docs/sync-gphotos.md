# Google Photos raw upload slice

This slice sends one asset's original bytes to the Google Photos Library API
uploads endpoint and returns the temporary upload token. The request uses
`POST /v1/uploads` with `application/octet-stream`,
`X-Goog-Upload-Content-Type`, and `X-Goog-Upload-Protocol: raw`, as described in
[Google's upload guide](https://developers.google.com/photos/library/guides/upload-media).

The token identifies uploaded bytes for a later `mediaItems.batchCreate` call.
It is not a remote media item ID and does not prove that a photo was created.
Authentication is supplied by the caller's configured HTTP client; future
OAuth work is expected to use the `photoslibrary.appendonly` scope.

This is an independent client only. It does not register a `StorageProvider`,
create albums or media items, persist item mappings, or implement sync
orchestration, OAuth, routes, or UI. JOOXF-149 remains incomplete while blocked
by JOOXF-147.

The client does not retry. A transport or response failure after Google has
received the bytes can leave an unused token. Callers must provide a fresh asset
reader for any later attempt and configure the HTTP client not to follow
redirects.
