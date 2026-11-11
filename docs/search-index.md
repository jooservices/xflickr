# Search index bootstrap and document writes

The search adapter creates an Elasticsearch 9 photo index when explicitly
called. The application also exposes a single-document `Upserter` port for
writing photos through the configured alias. Neither operation runs during
application startup or adds a public HTTP route.

## Names and creation

`OPENSEARCH_INDEX_PREFIX` is required and must match
`^[a-z0-9][a-z0-9_-]*_$`. The physical index name is `<prefix>photos-v1`; the
write alias is `<prefix>photos`. The prefix already ends in `_`, so no extra
separator is added. Elasticsearch accepts physical index names up to 255
bytes.

The adapter creates the index and alias with one `PUT` request. If the physical
index already exists, Elasticsearch returns an error and the adapter leaves it
alone. It does not repair or update an existing index or alias.

## Mapping

The mapping uses `dynamic: strict` and these fields:

| Field | Mapping |
|---|---|
| `title`, `description`, `tags`, `caption` | `text` with the built-in `standard` analyzer |
| `embedding` | 1,024-dimensional indexed `dense_vector`, cosine similarity, HNSW |
| `owner`, `kind`, `sizes_status`, `ai_status`, `visible_to`, `visibility` | `keyword` |
| `uploaded_at` | `date` |
| `publicly_shared` | `boolean` |

Elasticsearch `_id` is the document identifier; `_source` has no `id` field.
Visibility user IDs are decimal strings in `visible_to`, and shared records use
`publicly_shared`. A future query slice must verify that the existing search
visibility predicate matches this representation against indexed documents.

The creation request has this shape (`<prefix>` is replaced by the validated
configured prefix):

```json
{
  "mappings": {
    "dynamic": "strict",
    "properties": {
      "title": {"type": "text", "analyzer": "standard"},
      "description": {"type": "text", "analyzer": "standard"},
      "tags": {"type": "text", "analyzer": "standard"},
      "caption": {"type": "text", "analyzer": "standard"},
      "embedding": {
        "type": "dense_vector", "dims": 1024, "index": true,
        "similarity": "cosine", "index_options": {"type": "hnsw"}
      },
      "owner": {"type": "keyword"},
      "kind": {"type": "keyword"},
      "sizes_status": {"type": "keyword"},
      "ai_status": {"type": "keyword"},
      "uploaded_at": {"type": "date"},
      "visible_to": {"type": "keyword"},
      "visibility": {"type": "keyword"},
      "publicly_shared": {"type": "boolean"}
    }
  },
  "aliases": {
    "<prefix>photos": {"is_write_index": true}
  }
}
```

No shard, replica, or refresh interval overrides are sent; service defaults
apply. The adapter uses the configured `SEARCH_URL` and optional
`SEARCH_USERNAME` / `SEARCH_PASSWORD`, with the defaults documented in
`ENVIRONMENT.md`.

## Single-photo upsert

`search/app.Upserter` accepts one `Document`. A positive photo ID is sent as a
decimal Elasticsearch `_id` in `PUT <base URL>/<prefix>photos/_doc/<id>`; the
ID is not part of `_source`. The body contains the mapped photo fields:
`title`, `description`, `tags`, `caption`, `embedding`, `owner`, `kind`,
`sizes_status`, `ai_status`, `uploaded_at`, `visible_to`, `visibility`, and
`publicly_shared`. Empty embeddings and nil `uploaded_at` values are omitted;
nil `tags` and `visible_to` slices are JSON `null`.

An empty embedding or a 1,024-value embedding is accepted. Other dimensions are
rejected as an invalid document before the request. Non-finite embedding values
fail JSON encoding before the request. Repeating the upsert with the same ID
replaces the whole stored document. Any 2xx response succeeds; other statuses
return an error containing the status only. Request errors hide transport
details while preserving context cancellation for callers. The adapter does
not retry, force a refresh, or create a missing index.

## Deferred work

This slice does not implement the full `SearchIndex` port, batch upsert,
delete, query, reindex, outbox synchronization, permission-change
synchronization, hybrid retrieval, fallback behavior, REST endpoints, or UI.
The primitive upserter is not wired to an indexer; search synchronization and
visibility enforcement remain future work.
