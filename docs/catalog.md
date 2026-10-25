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
