# Catalog people and photos

The catalog stores Flickr people and photo metadata in PostgreSQL. People are
upserted before their photos because `photos.owner_nsid` references
`people.nsid`. Photo IDs route across 16 hash partitions, with indexes for
owner, last update, and size retrieval status.

The PostgreSQL adapter accepts upsert batches of up to 500 records and skips
unchanged records. Photo equality uses Flickr's `last_update` value and a
SHA-256 hash of the source JSON. The adapter stores the zstd compressed source
in a `photos.raw` JSONB envelope with an explicit encoding and base64 data.

Size updates carry `none`, `provisional`, or `confirmed` status. A provisional
response cannot replace confirmed sizes. Tombstone updates preserve the first
recorded timestamp when repeated.

## Catalog read API

The authenticated read API exposes `GET /people`, `/people/{nsid}`,
`/people/{nsid}/photos`, `/favorites`, `/albums`, `/albums/{id}/photos`, and
`/photos/{id}`. The OpenAPI contract in `contracts/openapi/paths/catalog.yaml`
is the source for the generated server and web clients.

Every collection uses an opaque keyset cursor and a page size from 1 to 100
(default 50). People are listed when the user observed them or has at least one
visible photo from them. Favorites and albums are limited to the user's
connected Flickr accounts. Photo rows in every response use the shared sharing
policy and exclude tombstones; hidden records are omitted from lists and return
the same 404 as missing records for detail reads. Album and person photo counts
report only rows visible to the signed-in user.

Photo responses contain tags as `labels`, stored Flickr size entries and their
`sizes_status`, plus `ai_status` (`ready` when a caption enrichment exists,
otherwise `none`). Caption text, Flickr API credentials, the source payload,
photo secrets, and content hashes are not returned.
