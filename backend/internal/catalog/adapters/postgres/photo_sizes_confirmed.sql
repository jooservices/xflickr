(
    (p.visibility = 'public'
     AND p.sizes_status = 'confirmed'
     AND p.sizes_checked_at >= p.last_update
     AND p.server <> '65535')
    OR EXISTS (
        SELECT 1
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
          AND result.sizes_status = 'confirmed'
          AND result.sizes_checked_at >= result.photo_updated_at
    )
)
