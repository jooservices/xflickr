# Search index bootstrap

The first search slice creates an Elasticsearch 9 photo index when explicitly
called. It does not run during application startup and does not add a public
HTTP route, a `SearchIndex` port, or indexing and query behavior.

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

## Deferred work

This bootstrap does not implement the `SearchIndex` port, upsert, delete,
query, reindex, outbox synchronization, permission-change synchronization,
hybrid retrieval, fallback behavior, REST endpoints, or UI. Those slices must
add and test their own behavior before search is a complete feature.
