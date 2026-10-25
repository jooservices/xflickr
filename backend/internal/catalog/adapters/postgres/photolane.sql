-- Photos still waiting for getSizes, best interest first. Every interest row
-- ranks as (live selection, star, depth) and a person ranks by their best row.
-- Within a person the newest upload comes first. $1 is how many top-ranked
-- persons to probe and $2 how many photos to return. The caller doubles $1
-- until $2 photos are found or no ranking stream was cut by its LIMIT, so a
-- ranking that starts with finished persons still reaches the first person with
-- work. "more" tells the caller a wider probe could find more persons; with no
-- photos the single row has a NULL id. Each stream below is served by an
-- index and a LIMIT; the photo probe uses photos_sizes_pending_idx.
WITH rows AS (
    (SELECT person_nsid, 0 AS tier, CASE WHEN starred THEN 0 ELSE 1 END AS sub, depth
     FROM crawl_interests
     WHERE selected_until IS NOT NULL AND selected_until > now()
     ORDER BY CASE WHEN starred THEN 0 ELSE 1 END, depth
     LIMIT $1::int)
    UNION ALL
    (SELECT person_nsid, 1, 0, depth
     FROM crawl_interests
     WHERE starred AND NOT (selected_until IS NOT NULL AND selected_until > now())
     ORDER BY depth
     LIMIT $1::int)
    UNION ALL
    (SELECT person_nsid, 2, 0, depth
     FROM crawl_interests
     WHERE NOT starred AND NOT (selected_until IS NOT NULL AND selected_until > now())
     ORDER BY depth
     LIMIT $1::int)
), best AS (
    SELECT DISTINCT ON (person_nsid) person_nsid, tier, sub, depth
    FROM rows
    ORDER BY person_nsid, tier, sub, depth
), ranked AS (
    SELECT person_nsid, tier, sub, depth
    FROM best
    ORDER BY tier, sub, depth, person_nsid
    LIMIT $1::int
)
SELECT c.more, p.id, p.owner_nsid
FROM (
    SELECT coalesce(max(n), 0) >= $1::int AS more
    FROM (SELECT count(*) AS n FROM rows GROUP BY tier) AS streams
) AS c
LEFT JOIN LATERAL (
    SELECT p.id, p.owner_nsid, row_number() OVER () AS rn
    FROM ranked AS r
    CROSS JOIN LATERAL (
        SELECT id, owner_nsid, uploaded_at
        FROM photos
        WHERE owner_nsid = r.person_nsid
          AND sizes_status <> 'confirmed'
          AND tombstoned_at IS NULL
        ORDER BY uploaded_at DESC NULLS LAST, id DESC
        LIMIT $2::int
    ) AS p
    ORDER BY r.tier, r.sub, r.depth, r.person_nsid, p.uploaded_at DESC NULLS LAST, p.id DESC
    LIMIT $2::int
) AS p ON true
ORDER BY p.rn
