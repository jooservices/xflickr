-- The photo lane drains size confirmation work before optional facets. Selected
-- or starred contacts are the only owners eligible for background photo work. $1 is
-- the person probe size, $2 is the outer job count, $3 is the enabled facet list,
-- and $4 bounds raw photo reads per owner before result/facet filters.
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
), best AS (
    SELECT DISTINCT ON (person_nsid) person_nsid, tier, sub, depth
    FROM rows
    ORDER BY person_nsid, tier, sub, depth
), ranked AS (
    SELECT person_nsid, tier, sub, depth
    FROM best
    ORDER BY tier, sub, depth, person_nsid
    LIMIT $1::int
), owners AS (
    SELECT ranked.*, owner_account.id AS account_id,
           (cursor.value::jsonb ->> 'uploaded_at')::timestamptz AS cursor_uploaded_at,
           (cursor.value::jsonb ->> 'photo_id')::bigint AS cursor_photo_id
    FROM ranked
    LEFT JOIN LATERAL (
        SELECT account.id
        FROM flickr_accounts AS account
        JOIN api_keys AS owner_key
          ON owner_key.id = account.key_id
         AND owner_key.user_id = account.user_id
         AND owner_key.status = 'valid'
        JOIN crawl_interests AS owner_interest
          ON owner_interest.user_id = account.user_id
         AND owner_interest.person_nsid = ranked.person_nsid
         AND owner_interest.depth = 0
        WHERE account.nsid = ranked.person_nsid
          AND account.status = 'ok'
        ORDER BY account.id
        LIMIT 1
    ) AS owner_account ON true
    LEFT JOIN schedule_meta AS cursor
      ON cursor.key = 'photo_lane_cursor:' || ranked.person_nsid
), photo_batches AS (
    SELECT o.person_nsid, o.tier, o.sub, o.depth, o.account_id,
           p.id, p.owner_nsid,
           p.uploaded_at, p.sizes_status, p.sizes_checked_at, p.last_update,
           p.visibility, p.server
    FROM owners AS o
    CROSS JOIN LATERAL (
        WITH after_nonnull AS (
            SELECT p.id, p.owner_nsid, p.uploaded_at, p.sizes_status,
                   p.sizes_checked_at, p.last_update, p.visibility, p.server
            FROM photos AS p
            WHERE p.owner_nsid = o.person_nsid
              AND p.tombstoned_at IS NULL
              AND o.cursor_photo_id IS NOT NULL
              AND o.cursor_uploaded_at IS NOT NULL
              AND p.uploaded_at IS NOT NULL
              AND (p.uploaded_at, p.id) < (o.cursor_uploaded_at, o.cursor_photo_id)
            ORDER BY p.uploaded_at DESC NULLS LAST, p.id DESC
            LIMIT $4::int
        ), after_null AS (
            SELECT p.id, p.owner_nsid, p.uploaded_at, p.sizes_status,
                   p.sizes_checked_at, p.last_update, p.visibility, p.server
            FROM photos AS p
            WHERE p.owner_nsid = o.person_nsid
              AND p.tombstoned_at IS NULL
              AND o.cursor_photo_id IS NOT NULL
              AND p.uploaded_at IS NULL
              AND (o.cursor_uploaded_at IS NOT NULL OR p.id < o.cursor_photo_id)
            ORDER BY p.uploaded_at DESC NULLS LAST, p.id DESC
            LIMIT GREATEST(0, $4::int - (SELECT count(*)::int FROM after_nonnull))
        ), after_page AS (
            SELECT * FROM after_nonnull
            UNION ALL
            SELECT * FROM after_null
        )
        SELECT * FROM after_page
        UNION ALL
        (SELECT p.id, p.owner_nsid, p.uploaded_at, p.sizes_status,
                p.sizes_checked_at, p.last_update, p.visibility, p.server
         FROM photos AS p
         WHERE p.owner_nsid = o.person_nsid
           AND p.tombstoned_at IS NULL
           AND (o.cursor_photo_id IS NULL OR NOT EXISTS (SELECT 1 FROM after_page))
         ORDER BY p.uploaded_at DESC NULLS LAST, p.id DESC
         LIMIT $4::int)
    ) AS p
), pending_photos AS (
    SELECT p.*
    FROM photo_batches AS p
    WHERE p.server <> '65535'
      AND (p.sizes_status NOT IN ('confirmed', 'unavailable')
           OR (p.sizes_status = 'confirmed'
               AND p.sizes_checked_at < p.last_update))
      AND (p.account_id IS NULL OR NOT EXISTS (
          SELECT 1
          FROM photo_account_sizes AS result
          WHERE result.photo_id = p.id
            AND result.account_id = p.account_id
            AND result.photo_updated_at = p.last_update
            AND result.photo_visibility = p.visibility
      ))
), restricted_photos AS (
    SELECT p.*
    FROM photo_batches AS p
    WHERE (p.sizes_status = 'unavailable' OR p.server = '65535')
      AND (p.account_id IS NULL OR NOT EXISTS (
          SELECT 1
          FROM photo_account_sizes AS result
          WHERE result.photo_id = p.id
            AND result.account_id = p.account_id
            AND result.photo_updated_at = p.last_update
            AND result.photo_visibility = p.visibility
      ))
), optional_photos AS (
    SELECT p.*
    FROM photo_batches AS p
    WHERE cardinality($3::text[]) > 0
      AND __PHOTO_SIZES_CONFIRMED__
      AND EXISTS (
          SELECT 1
          FROM unnest($3::text[]) AS enabled(facet)
          WHERE NOT EXISTS (
              SELECT 1
              FROM photo_facet_status AS done
              WHERE done.photo_id = p.id
                AND done.facet = enabled.facet
                AND done.photo_updated_at >= p.last_update
          )
      )
      AND EXISTS (
          SELECT 1
          FROM crawl_interests AS i
          WHERE i.person_nsid = p.owner_nsid
            AND (i.starred OR (i.selected_until IS NOT NULL AND i.selected_until > now()))
      )
), candidate_photos AS (
    SELECT * FROM pending_photos
    UNION
    SELECT * FROM restricted_photos
    UNION
    SELECT * FROM optional_photos
), photo_states AS (
    SELECT p.*,
           __PHOTO_SIZES_CONFIRMED__ AS sizes_confirmed,
           coalesce(account_sizes.resolved, false) AS owner_sizes_resolved
    FROM candidate_photos AS p
    LEFT JOIN LATERAL (
        SELECT bool_or(result.sizes_status IN ('confirmed', 'unavailable')) AS resolved
        FROM photo_account_sizes AS result
        JOIN flickr_accounts AS owner_account
          ON owner_account.id = result.account_id
         AND owner_account.nsid = p.owner_nsid
         AND owner_account.status = 'ok'
        JOIN api_keys AS owner_key
          ON owner_key.id = owner_account.key_id
         AND owner_key.user_id = owner_account.user_id
         AND owner_key.status = 'valid'
        JOIN crawl_interests AS owner_interest
          ON owner_interest.user_id = owner_account.user_id
         AND owner_interest.person_nsid = p.owner_nsid
         AND owner_interest.depth = 0
        WHERE result.photo_id = p.id
          AND result.photo_updated_at = p.last_update
          AND result.photo_visibility = p.visibility
    ) AS account_sizes ON true
), candidates AS (
    SELECT p.id, p.owner_nsid, p.tier, p.sub, p.depth, p.uploaded_at,
           0 AS facet_priority, 0 AS facet_order, 'sizes'::text AS facet
    FROM photo_states AS p
    WHERE (p.sizes_status <> 'confirmed'
           OR p.sizes_checked_at < p.last_update
           OR p.server = '65535')
      AND NOT p.owner_sizes_resolved

    UNION ALL

    SELECT p.id, p.owner_nsid, p.tier, p.sub, p.depth, p.uploaded_at,
           1 AS facet_priority, f.facet_order::int, f.facet
    FROM photo_states AS p
    CROSS JOIN UNNEST($3::text[]) WITH ORDINALITY AS f(facet, facet_order)
    WHERE p.sizes_confirmed
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
), cursor_updates AS (
    SELECT DISTINCT ON (batch.person_nsid)
           batch.person_nsid, batch.id, batch.uploaded_at
    FROM photo_batches AS batch
    WHERE NOT (SELECT more FROM counts)
      AND NOT EXISTS (
          SELECT 1 FROM candidates WHERE candidates.owner_nsid = batch.person_nsid
      )
    ORDER BY batch.person_nsid, batch.uploaded_at ASC NULLS FIRST, batch.id ASC
), cursor_writes AS (
    INSERT INTO schedule_meta (key, value)
    SELECT 'photo_lane_cursor:' || person_nsid,
           jsonb_build_object('uploaded_at', uploaded_at, 'photo_id', id)::text
    FROM cursor_updates
    ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value
    RETURNING key
)
SELECT counts.more, jobs.id, jobs.owner_nsid, jobs.facet
FROM counts
CROSS JOIN (SELECT count(*) FROM cursor_writes) AS cursor_write_count
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
