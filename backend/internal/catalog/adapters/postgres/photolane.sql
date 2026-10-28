-- The photo lane drains size confirmation work before optional facets. Selected
-- or starred contacts are the only owners eligible for optional facets. $1 is
-- the person probe size, $2 is the job count, and $3 is the enabled facet list.
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
), candidates AS (
    SELECT p.id, p.owner_nsid, r.tier, r.sub, r.depth, p.uploaded_at,
           0 AS facet_priority, 0 AS facet_order, 'sizes'::text AS facet
    FROM ranked AS r
    JOIN photos AS p ON p.owner_nsid = r.person_nsid
    WHERE (p.sizes_status NOT IN ('confirmed', 'unavailable')
           OR (p.sizes_status = 'confirmed' AND p.sizes_checked_at < p.last_update))
      AND p.tombstoned_at IS NULL

    UNION ALL

    SELECT p.id, p.owner_nsid, r.tier, r.sub, r.depth, p.uploaded_at,
           1 AS facet_priority, f.facet_order::int, f.facet
    FROM ranked AS r
    JOIN photos AS p ON p.owner_nsid = r.person_nsid
    CROSS JOIN UNNEST($3::text[]) WITH ORDINALITY AS f(facet, facet_order)
    WHERE p.sizes_status = 'confirmed'
      AND p.sizes_checked_at >= p.last_update
      AND p.tombstoned_at IS NULL
      AND EXISTS (
          SELECT 1
          FROM crawl_interests AS i
          WHERE i.person_nsid = p.owner_nsid
            AND (i.starred OR (i.selected_until IS NOT NULL AND i.selected_until > now()))
      )
      AND NOT EXISTS (
          SELECT 1
          FROM photo_facet_status AS done
          WHERE done.photo_id = p.id
            AND done.facet = f.facet
            AND done.photo_updated_at >= p.last_update
      )
), counts AS (
    SELECT coalesce(max(n), 0) >= $1::int AS more
    FROM (SELECT count(*) AS n FROM rows GROUP BY tier) AS streams
)
SELECT counts.more, jobs.id, jobs.owner_nsid, jobs.facet
FROM counts
LEFT JOIN LATERAL (
    SELECT candidates.*,
           row_number() OVER (
               ORDER BY facet_priority, tier, sub, depth, owner_nsid,
                        uploaded_at DESC NULLS LAST, id DESC, facet_order
           ) AS rn
    FROM candidates
    ORDER BY facet_priority, tier, sub, depth, owner_nsid,
             uploaded_at DESC NULLS LAST, id DESC, facet_order
    LIMIT $2::int
) AS jobs ON true
ORDER BY jobs.rn
