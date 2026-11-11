# AI worker caption persistence

This slice provides an internal path for one caller-supplied photo image. The
AI application calls the existing `Captioner` once, validates the returned
caption, and stores it through a caption store port. It does not connect the
path to River or enable the `ai-worker` role.

The catalog adapter encodes the caption as JSON with `text`, `objects`,
`model`, and `version`. The catalog application service maps the payload and
model provenance to a `photo_enrichments` row with kind `caption`. The existing
catalog repository upserts on `(photo_id, kind, model)`, replacing the payload
and model version for the same model while keeping other models independent.

The AI adapter depends on the catalog application API. It does not import the
catalog PostgreSQL adapter or domain. Invalid photo IDs, empty images,
cancellation, captioner errors, invalid captions, and persistence errors return
to the caller. The store is not called when describing or validating the
caption fails.

This path stores caption metadata only. It does not create embeddings, publish
`enrichment.changed`, select photos, enforce quota, or represent completion of
all enrichment work. Worker wiring, retry and recovery remain deferred.
