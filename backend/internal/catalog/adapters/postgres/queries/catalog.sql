-- name: EnsurePeople :exec
INSERT INTO people (nsid, username, realname, profile, icon, photos_count, last_seen_at, facet_state)
SELECT nsid, username, realname, profile, icon, photos_count, last_seen_at, facet_state
FROM ROWS FROM (
    unnest(sqlc.arg(nsids)::text[]),
    unnest(sqlc.arg(usernames)::text[]),
    unnest(sqlc.arg(realnames)::text[]),
    unnest(sqlc.arg(profiles)::jsonb[]),
    unnest(sqlc.arg(icons)::text[]),
    unnest(sqlc.arg(photos_counts)::bigint[]),
    unnest(sqlc.arg(last_seen_ats)::timestamptz[]),
    unnest(sqlc.arg(facet_states)::jsonb[])
) AS incoming(nsid, username, realname, profile, icon, photos_count, last_seen_at, facet_state)
ORDER BY incoming.nsid
ON CONFLICT (nsid) DO NOTHING;

-- name: RefreshChangedPeople :exec
WITH incoming AS (
    SELECT nsid, username, realname, profile, icon, photos_count, last_seen_at, facet_state
    FROM ROWS FROM (
        unnest(sqlc.arg(nsids)::text[]),
        unnest(sqlc.arg(usernames)::text[]),
        unnest(sqlc.arg(realnames)::text[]),
        unnest(sqlc.arg(profiles)::jsonb[]),
        unnest(sqlc.arg(icons)::text[]),
        unnest(sqlc.arg(photos_counts)::bigint[]),
        unnest(sqlc.arg(last_seen_ats)::timestamptz[]),
        unnest(sqlc.arg(facet_states)::jsonb[])
    ) AS people_input(nsid, username, realname, profile, icon, photos_count, last_seen_at, facet_state)
), todo AS MATERIALIZED (
    SELECT
        p.nsid,
        CASE WHEN i.username = i.nsid AND p.username <> '' THEN p.username ELSE i.username END AS username,
        COALESCE(NULLIF(i.realname, ''), p.realname) AS realname,
        CASE WHEN i.profile <> '{}'::jsonb THEN i.profile ELSE p.profile END AS profile,
        COALESCE(NULLIF(i.icon, ''), p.icon) AS icon,
        CASE WHEN i.photos_count > 0 THEN i.photos_count ELSE p.photos_count END AS photos_count,
        CASE
            WHEN i.last_seen_at IS NULL THEN p.last_seen_at
            WHEN p.last_seen_at IS NULL THEN i.last_seen_at
            WHEN i.last_seen_at >= p.last_seen_at + interval '1 hour' THEN i.last_seen_at
            ELSE p.last_seen_at
        END AS last_seen_at,
        CASE WHEN i.facet_state <> '{}'::jsonb THEN i.facet_state ELSE p.facet_state END AS facet_state
    FROM people AS p
    JOIN incoming AS i USING (nsid)
    WHERE (p.username, p.realname, p.profile, p.icon, p.photos_count, p.last_seen_at, p.facet_state)
        IS DISTINCT FROM
        (
            CASE WHEN i.username = i.nsid AND p.username <> '' THEN p.username ELSE i.username END,
            COALESCE(NULLIF(i.realname, ''), p.realname),
            CASE WHEN i.profile <> '{}'::jsonb THEN i.profile ELSE p.profile END,
            COALESCE(NULLIF(i.icon, ''), p.icon),
            CASE WHEN i.photos_count > 0 THEN i.photos_count ELSE p.photos_count END,
            CASE
                WHEN i.last_seen_at IS NULL THEN p.last_seen_at
                WHEN p.last_seen_at IS NULL THEN i.last_seen_at
                WHEN i.last_seen_at >= p.last_seen_at + interval '1 hour' THEN i.last_seen_at
                ELSE p.last_seen_at
            END,
            CASE WHEN i.facet_state <> '{}'::jsonb THEN i.facet_state ELSE p.facet_state END
        )
    ORDER BY p.nsid
    FOR NO KEY UPDATE OF p
)
UPDATE people AS p
SET username = todo.username,
    realname = todo.realname,
    profile = todo.profile,
    icon = todo.icon,
    photos_count = todo.photos_count,
    last_seen_at = todo.last_seen_at,
    facet_state = todo.facet_state
FROM todo
WHERE p.nsid = todo.nsid;

-- name: UpsertPhoto :batchexec
INSERT INTO photos (
    id, owner_nsid, secret, server, title, description, tags, taken_at,
    uploaded_at, last_update, visibility, license, media, geo, sizes,
    sizes_status, sizes_checked_at, raw, content_hash, tombstoned_at, last_seen_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
    $16, $17, $18, $19, $20, $21
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
    tombstoned_at = EXCLUDED.tombstoned_at,
    last_seen_at = EXCLUDED.last_seen_at
WHERE photos.last_update IS DISTINCT FROM EXCLUDED.last_update
   OR photos.content_hash IS DISTINCT FROM EXCLUDED.content_hash
   OR photos.last_seen_at IS DISTINCT FROM EXCLUDED.last_seen_at;

-- name: SetSizes :exec
UPDATE photos
SET sizes = $2, sizes_status = $3, sizes_checked_at = $4
WHERE id = $1
  AND (sizes_status <> 'confirmed' OR $3 IN ('confirmed', 'unavailable'))
  AND (sizes, sizes_status, sizes_checked_at) IS DISTINCT FROM ($2, $3, $4);

-- name: MarkTombstone :exec
UPDATE photos SET tombstoned_at = $2
WHERE id = $1 AND tombstoned_at IS NULL;
