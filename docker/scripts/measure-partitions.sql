-- EXPLAIN (ANALYZE, BUFFERS) probes for HASH(id) vs HASH(owner_nsid).
-- Run after docker/scripts/synthetic-catalog.sh.

\timing on
\pset pager off

\echo === photos by owner (current HASH id) ===
EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
SELECT id, owner_nsid, uploaded_at
FROM photos
WHERE owner_nsid = 'owner-000042' AND tombstoned_at IS NULL
ORDER BY uploaded_at DESC NULLS LAST, id DESC
LIMIT 50;

\echo === photos by owner (HASH owner_nsid) ===
EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
SELECT id, owner_nsid, uploaded_at
FROM photos_by_owner
WHERE owner_nsid = 'owner-000042' AND tombstoned_at IS NULL
ORDER BY uploaded_at DESC NULLS LAST, id DESC
LIMIT 50;

\echo === photo by id (current HASH id) ===
EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
SELECT id, owner_nsid, last_update FROM photos WHERE id = 1500000;

\echo === photo by id (HASH owner_nsid, id alone) ===
EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
SELECT id, owner_nsid, last_update FROM photos_by_owner WHERE id = 1500000;

\echo === photo by id+owner (HASH owner_nsid) ===
EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
SELECT id, owner_nsid, last_update
FROM photos_by_owner
WHERE owner_nsid = 'owner-000000' AND id = 1500000;

\echo === favorites by person (current HASH id/photo) ===
EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
SELECT person_nsid, photo_id, fave_date
FROM favorites
WHERE person_nsid = 'owner-000042' AND removed_at IS NULL
ORDER BY fave_date DESC, photo_id DESC
LIMIT 50;

\echo === favorites by person (HASH person_nsid) ===
EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
SELECT person_nsid, photo_id, fave_date
FROM favorites_by_person
WHERE person_nsid = 'owner-000042' AND removed_at IS NULL
ORDER BY fave_date DESC, photo_id DESC
LIMIT 50;
