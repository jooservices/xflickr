-- name: UpsertPerson :batchexec
INSERT INTO people (nsid, username, realname, profile, icon, photos_count, last_seen_at, facet_state)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (nsid) DO UPDATE SET
    username = EXCLUDED.username,
    realname = COALESCE(NULLIF(EXCLUDED.realname, ''), people.realname),
    profile = CASE WHEN EXCLUDED.profile <> '{}'::jsonb THEN EXCLUDED.profile ELSE people.profile END,
    icon = COALESCE(NULLIF(EXCLUDED.icon, ''), people.icon),
    photos_count = CASE WHEN EXCLUDED.photos_count > 0 THEN EXCLUDED.photos_count ELSE people.photos_count END,
    last_seen_at = COALESCE(EXCLUDED.last_seen_at, people.last_seen_at),
    facet_state = CASE WHEN EXCLUDED.facet_state <> '{}'::jsonb THEN EXCLUDED.facet_state ELSE people.facet_state END
WHERE (people.username, people.realname, people.profile, people.icon, people.photos_count, people.last_seen_at, people.facet_state)
    IS DISTINCT FROM
    (EXCLUDED.username,
     COALESCE(NULLIF(EXCLUDED.realname, ''), people.realname),
     CASE WHEN EXCLUDED.profile <> '{}'::jsonb THEN EXCLUDED.profile ELSE people.profile END,
     COALESCE(NULLIF(EXCLUDED.icon, ''), people.icon),
     CASE WHEN EXCLUDED.photos_count > 0 THEN EXCLUDED.photos_count ELSE people.photos_count END,
     COALESCE(EXCLUDED.last_seen_at, people.last_seen_at),
     CASE WHEN EXCLUDED.facet_state <> '{}'::jsonb THEN EXCLUDED.facet_state ELSE people.facet_state END);

-- name: UpsertPhoto :batchexec
INSERT INTO photos (
    id, owner_nsid, secret, server, title, description, tags, taken_at,
    uploaded_at, last_update, visibility, license, media, geo, sizes,
    sizes_status, sizes_checked_at, raw, content_hash, tombstoned_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
    $16, $17, $18, $19, $20
)
ON CONFLICT (id) DO UPDATE SET
    owner_nsid = EXCLUDED.owner_nsid,
    secret = EXCLUDED.secret,
    server = EXCLUDED.server,
    title = EXCLUDED.title,
    description = EXCLUDED.description,
    tags = EXCLUDED.tags,
    taken_at = EXCLUDED.taken_at,
    uploaded_at = EXCLUDED.uploaded_at,
    last_update = EXCLUDED.last_update,
    visibility = EXCLUDED.visibility,
    license = EXCLUDED.license,
    media = EXCLUDED.media,
    geo = EXCLUDED.geo,
    raw = EXCLUDED.raw,
    content_hash = EXCLUDED.content_hash,
    tombstoned_at = EXCLUDED.tombstoned_at
WHERE photos.last_update IS DISTINCT FROM EXCLUDED.last_update
   OR photos.content_hash IS DISTINCT FROM EXCLUDED.content_hash;

-- name: SetSizes :exec
UPDATE photos
SET sizes = $2, sizes_status = $3, sizes_checked_at = $4
WHERE id = $1
  AND (sizes_status <> 'confirmed' OR $3 IN ('confirmed', 'unavailable'))
  AND (sizes, sizes_status, sizes_checked_at) IS DISTINCT FROM ($2, $3, $4);

-- name: MarkTombstone :exec
UPDATE photos SET tombstoned_at = $2
WHERE id = $1 AND tombstoned_at IS NULL;
