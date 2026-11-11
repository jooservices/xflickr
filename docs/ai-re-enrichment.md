# AI enrichment freshness

The catalog application checks freshness for one photo, enrichment kind, model,
and target version. It reads by the existing unique key `(photo_id, kind,
model)`. A result is current only when that key exists and its stored model
version exactly matches the requested version. Model and version comparisons
are case-sensitive.

The predicate returns `needsEnrichment`; it does not distinguish a photo with
no result from one with an older version and does not count stale records. The
read is bounded to one key and does not call Flickr or Ollama.

An outdated result remains stored and available to existing search until a
later successful enrichment replaces it. This slice provides only an internal
application API and PostgreSQL read path. It has no worker, automatic queueing,
status endpoint, REST contract, or user interface, so model changes do not yet
trigger re-enrichment automatically.
