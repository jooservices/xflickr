BEGIN;

CREATE TEMP TABLE flickr_accounts (
    user_id BIGINT NOT NULL,
    nsid TEXT NOT NULL,
    status TEXT NOT NULL
);
CREATE INDEX flickr_accounts_contacts_nsid_user_id_idx
    ON pg_temp.flickr_accounts (nsid, user_id) WHERE status='ok';

INSERT INTO pg_temp.flickr_accounts (user_id, nsid, status)
SELECT id,
       CASE WHEN id <= 2 THEN 'plan-target@N00' ELSE 'account-' || id::text END,
       'ok'
FROM generate_series(1, 400000) AS id;
ANALYZE pg_temp.flickr_accounts;

CREATE FUNCTION pg_temp.contacts_users_plan() RETURNS jsonb
LANGUAGE plpgsql
AS $$
DECLARE
    plan jsonb;
BEGIN
    EXECUTE $explain$
        EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
        SELECT COALESCE(array_agg(user_id ORDER BY user_id), '{}'::bigint[])
        FROM flickr_accounts
        WHERE nsid='plan-target@N00' AND status='ok'
    $explain$ INTO plan;

    IF plan::text LIKE '%Seq Scan%' THEN
        RAISE EXCEPTION 'contacts user lookup uses a sequential scan: %', plan;
    END IF;

    RETURN plan;
END;
$$;

SELECT pg_temp.contacts_users_plan();
ROLLBACK;
